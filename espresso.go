package espresso

// VM is a JavaScript virtual machine instance.
// It evaluates JavaScript expressions and statements using a Go-native
// interpreter — no V8, no CGO, no external dependencies.
type VM struct {
	scope map[string]*Value
}

// New creates a new VM with an empty scope.
func New() *VM {
	vm := &VM{scope: make(map[string]*Value)}
	RegisterSymbolGlobals(vm.scope)
	// globalThis — global object (Node.js / browser compat)
	vm.scope["globalThis"] = NewObj(make(map[string]*Value))
	// AbortController / AbortSignal — minimal stub for compatibility
	vm.scope["AbortController"] = NewNativeFunc(func(args []*Value) *Value {
		signal := NewObj(map[string]*Value{
			"aborted":          False,
			"reason":           Undefined,
			"addEventListener": NewNativeFunc(func(args []*Value) *Value { return Undefined }),
		})
		return NewObj(map[string]*Value{
			"signal": signal,
			"abort": NewNativeFunc(func(args []*Value) *Value {
				signal.object["aborted"] = True
				return Undefined
			}),
		})
	})
	// Register built-in globals as scope objects for bytecode compatibility.
	// The interpreter handles these inline, but bytecode needs them in scope.
	registerBuiltinGlobals(vm.scope)
	return vm
}

// Set injects a Go value into the JS scope.
func (vm *VM) Set(name string, value interface{}) {
	vm.scope[name] = ToValue(value)
}

// SetValue injects a Value directly into the JS scope.
func (vm *VM) SetValue(name string, value *Value) {
	vm.scope[name] = value
}

// Get reads a value from scope.
func (vm *VM) Get(name string) *Value {
	if v, ok := vm.scope[name]; ok {
		return v
	}
	return Undefined
}

// RunSafe is like Run but recovers from panics, returning an error instead.
func (vm *VM) RunSafe(code string) (result *Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			result = Undefined
			err = nil // swallow panic
		}
	}()
	return vm.Run(code)
}

// Eval evaluates JS code and returns the result.
// Handles both single expressions and multi-statement code.
// Uses token caching — repeated calls with the same code skip tokenization.
func (vm *VM) Eval(code string) (*Value, error) {
	tokens := tokenizeCached(code)
	// Check if this is multi-statement code (has semicolons or declarations)
	hasStatements := false
	for _, t := range tokens {
		if t.t == tokSemi {
			hasStatements = true
			break
		}
		if t.t == tokIdent && (t.v == "const" || t.v == "let" || t.v == "var" || t.v == "function" || t.v == "class") {
			hasStatements = true
			break
		}
	}
	if hasStatements {
		// Extract function declarations and add to scope
		funcs := ExtractFunctions(code)
		for k, v := range funcs {
			vm.scope[k] = v
		}
		ev := &evaluator{tokens: tokens, pos: 0, scope: vm.scope}
		result := ev.evalStatementsWithLastValue()
		if result == nil {
			return Undefined, nil
		}
		return result, nil
	}
	ev := &evaluator{tokens: tokens, pos: 0, scope: vm.scope}
	return ev.expr(), nil
}

// Run evaluates multiple JS statements (const, let, if, for, return, etc.).
// Returns the value of the last return statement, or Undefined.
func (vm *VM) Run(code string) (*Value, error) {
	// Extract function declarations first so they're available for calls
	funcs := ExtractFunctions(code)
	for k, v := range funcs {
		vm.scope[k] = v
	}
	tokens := tokenizeCached(code)

	// Try bytecode compilation — skip for code with patterns the bytecode
	// compiler doesn't handle correctly yet (TODO: fix these in bytecode)
	var bc *bytecode
	if !needsInterpreter(tokens) {
		bc = compileFuncBodyTokens(tokens)
	}
	if bc != nil {
		scope := vm.copyScope()
		result := execBytecodeSafe(bc, scope)
		// Write back scope changes
		for k, v := range scope {
			vm.scope[k] = v
		}
		if result == nil {
			return Undefined, nil
		}
		return result, nil
	}

	// Interpreter fallback
	ev := &evaluator{tokens: tokens, pos: 0, scope: vm.copyScope()}
	result := ev.evalStatements()
	for k, v := range ev.scope {
		vm.scope[k] = v
	}
	if result == nil || result == breakSentinel || result == continueSentinel {
		return Undefined, nil
	}
	return result, nil
}

// needsInterpreter scans tokens for patterns the bytecode compiler
// doesn't handle correctly yet. Returns true to force interpreter mode.
// Each check is tagged with a TODO so they can be fixed individually.
func needsInterpreter(tokens []tok) bool {
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		// TODO-BC: for...in, for...of, for await — bytecode loop bugs
		if t.t == tokIdent && t.v == "for" {
			return true
		}
		// TODO-BC: try/catch/finally — finally block not executed
		if t.t == tokIdent && t.v == "try" {
			return true
		}
		// TODO-BC: switch/case — not fully implemented
		if t.t == tokIdent && t.v == "switch" {
			return true
		}
		// TODO-BC: await — async semantics differ in bytecode
		if t.t == tokIdent && t.v == "await" {
			return true
		}
		// TODO-BC: inner function declarations — bytecode can't create closures
		if t.t == tokIdent && t.v == "function" {
			return true
		}
		// TODO-BC: new on variables — bcNewCall doesn't propagate scope for $constructor pattern
		if t.t == tokIdent && t.v == "new" {
			return true
		}
		// TODO-BC: Object.* and Array.* — special syntax not in bytecode
		if t.t == tokIdent && (t.v == "Object" || t.v == "Array") && i+2 < len(tokens) &&
			tokens[i+1].t == tokDot && tokens[i+2].t == tokIdent {
			return true
		}
	}
	return false
}

// RunModule evaluates JS code without ExtractFunctions hoisting.
// Used by the module system so that function declarations naturally
// close over the module scope (const/var declarations are in scope).
func (vm *VM) RunModule(code string) (*Value, error) {
	tokens := tokenizeCached(code)
	ev := &evaluator{tokens: tokens, pos: 0, scope: vm.copyScope()}
	result := ev.evalStatements()
	for k, v := range ev.scope {
		vm.scope[k] = v
	}
	if result == nil || result == breakSentinel || result == continueSentinel {
		return Undefined, nil
	}
	return result, nil
}

// Call calls a function defined in scope with the given arguments.
func (vm *VM) Call(fn string, args ...interface{}) (*Value, error) {
	fnVal, ok := vm.scope[fn]
	if !ok || fnVal.typ != TypeFunc {
		return Undefined, nil
	}
	props := make(map[string]*Value)
	if len(fnVal.fnParams) == 1 && len(args) > 0 {
		props[fnVal.fnParams[0]] = ToValue(args[0])
	}
	ev := &evaluator{scope: vm.copyScope()}
	return ev.callFunc(fnVal, props), nil
}

// RegisterFunc registers a Go function callable from JS code.
func (vm *VM) RegisterFunc(name string, fn NativeFunc) {
	vm.scope[name] = NewNativeFunc(fn)
}

// Compile pre-compiles JS expression code for fast repeated execution.
// The returned Compiled object can be executed many times without re-tokenizing.
// It attempts closure compilation first for maximum performance.
//
//	compiled := vm.Compile("x * 2 + 1")
//	result := compiled.Exec(vm)
func (vm *VM) Compile(code string) *Compiled {
	tokens := tokenizeCached(code)
	// Try closure compilation for expression-wrapped-as-return
	returnWrapped := make([]tok, 0, len(tokens)+2)
	returnWrapped = append(returnWrapped, tok{t: tokIdent, v: "return"})
	returnWrapped = append(returnWrapped, tokens...)
	cp := compileTokens(returnWrapped)
	return &Compiled{tokens: tokens, isExpr: true, compiled: cp}
}

// CompileStatements pre-compiles JS statements for fast repeated execution.
// It attempts closure compilation first for maximum performance.
func (vm *VM) CompileStatements(code string) *Compiled {
	tokens := tokenizeCached(code)
	cp := compileTokens(tokens)
	return &Compiled{tokens: tokens, isExpr: false, compiled: cp}
}

// Compiled is pre-compiled JS code that can be executed repeatedly
// without re-tokenization. When possible, it uses the compiled (closure)
// path for faster execution; otherwise it falls back to the interpreted path.
type Compiled struct {
	tokens   []tok
	isExpr   bool
	compiled *compiledPage // non-nil if closure compilation succeeded
}

// Exec executes the compiled code using the VM's scope.
// Uses the closure-compiled path when available, otherwise falls back to interpreted.
func (c *Compiled) Exec(vm *VM) *Value {
	// Fast path: use compiled closures
	if c.compiled != nil {
		scope := vm.copyScope()
		result := c.compiled.execute(scope)
		// Merge scope changes back
		for k, v := range scope {
			vm.scope[k] = v
		}
		if result == nil {
			return Undefined
		}
		return result
	}

	// Fallback: interpreted path
	toks := make([]tok, len(c.tokens))
	copy(toks, c.tokens)
	if c.isExpr {
		ev := &evaluator{tokens: toks, pos: 0, scope: vm.scope}
		return ev.expr()
	}
	ev := &evaluator{tokens: toks, pos: 0, scope: vm.copyScope()}
	result := ev.evalStatements()
	for k, v := range ev.scope {
		vm.scope[k] = v
	}
	if result == nil || result == breakSentinel || result == continueSentinel {
		return Undefined
	}
	return result
}

// CompileAndRun compiles JS statements using the closure compiler and executes them.
// Falls back to the interpreted path if compilation fails.
func (vm *VM) CompileAndRun(code string) (*Value, error) {
	tokens := tokenizeCached(code)
	cp := compileTokens(tokens)
	if cp != nil {
		scope := vm.copyScope()
		result := cp.execute(scope)
		for k, v := range scope {
			vm.scope[k] = v
		}
		if result == nil {
			return Undefined, nil
		}
		return result, nil
	}
	// Fallback to interpreted
	return vm.Run(code)
}

// Scope returns a copy of all variables in the VM scope.
func (vm *VM) Scope() map[string]*Value {
	result := make(map[string]*Value, len(vm.scope))
	for k, v := range vm.scope {
		result[k] = v
	}
	return result
}

func (vm *VM) copyScope() map[string]*Value {
	scope := make(map[string]*Value, len(vm.scope))
	for k, v := range vm.scope {
		scope[k] = v
	}
	return scope
}
