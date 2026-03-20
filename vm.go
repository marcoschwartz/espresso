package espresso

// ─── Bytecode VM ─────────────────────────────────────────
// A stack-based bytecode compiler and VM for function bodies.
// Compiles simple function bodies (if/return, arithmetic, comparisons,
// function calls) to bytecode for fast repeated execution.
// Falls back to the interpreter for unsupported patterns.

type op uint8

const (
	opLoadVar    op = iota // push scope[sarg]
	opLoadNum              // push narg
	opLoadStr              // push sarg as string
	opStoreVar             // pop → scope[sarg]
	opAdd                  // pop 2, push a+b (string concat if either is string)
	opSub                  // pop 2, push a-b
	opMul                  // pop 2, push a*b
	opDiv                  // pop 2, push a/b
	opMod                  // pop 2, push a%b
	opLt                   // pop 2, push a<b
	opLtEq                 // pop 2, push a<=b
	opGt                   // pop 2, push a>b
	opGtEq                 // pop 2, push a>=b
	opEqEq                 // pop 2, push a==b (loose)
	opEqEqEq               // pop 2, push a===b (strict)
	opNeq                  // pop 2, push a!=b
	opNeqEq                // pop 2, push a!==b
	opNot                  // pop 1, push !a
	opNeg                  // pop 1, push -a
	opTrue                 // push true
	opFalse                // push false
	opNull                 // push null
	opUndefined            // push undefined
	opReturn               // return top of stack
	opJumpIfFalse          // if !pop, jump to iarg
	opJump                 // jump to iarg
	opCall                 // call function sarg with iarg args from stack
	opPop                  // discard top
)

type instr struct {
	op   op
	sarg string
	narg float64
	iarg int
}

type bytecode struct {
	code   []instr
	params []string // cached param names for recursive calls
}

// compileFuncBody attempts to compile a function body to bytecode.
// Returns nil if the body uses unsupported patterns.
func compileFuncBody(body string) *bytecode {
	tokens := tokenizeCached(body)
	c := &bcCompiler{tokens: tokens, pos: 0}
	bc := c.compile()
	if c.failed {
		return nil
	}
	return bc
}

type bcCompiler struct {
	tokens []tok
	pos    int
	failed bool
}

func (c *bcCompiler) peek() tok {
	if c.pos < len(c.tokens) {
		return c.tokens[c.pos]
	}
	return tok{t: tokEOF}
}

func (c *bcCompiler) advance() tok {
	t := c.peek()
	if c.pos < len(c.tokens) {
		c.pos++
	}
	return t
}

func (c *bcCompiler) expect(tt tokType) {
	if c.peek().t == tt {
		c.advance()
	}
}

func (c *bcCompiler) fail() {
	c.failed = true
}

func (c *bcCompiler) compile() *bytecode {
	bc := &bytecode{}
	for c.peek().t != tokEOF && !c.failed {
		c.compileStatement(bc)
		if c.peek().t == tokSemi {
			c.advance()
		}
	}
	return bc
}

func (c *bcCompiler) compileStatement(bc *bytecode) {
	t := c.peek()

	// return expr
	if t.t == tokIdent && t.v == "return" {
		c.advance()
		c.compileExpr(bc)
		bc.code = append(bc.code, instr{op: opReturn})
		return
	}

	// if (cond) { body } [else { body }]
	if t.t == tokIdent && t.v == "if" {
		c.advance()
		c.expect(tokLParen)
		c.compileExpr(bc)
		c.expect(tokRParen)

		// Emit jump-if-false, patch later
		jumpIdx := len(bc.code)
		bc.code = append(bc.code, instr{op: opJumpIfFalse})

		// if body — could be { ... } or single statement
		if c.peek().t == tokLBrace {
			c.advance()
			for c.peek().t != tokRBrace && c.peek().t != tokEOF && !c.failed {
				c.compileStatement(bc)
				if c.peek().t == tokSemi {
					c.advance()
				}
			}
			c.expect(tokRBrace)
		} else {
			c.compileStatement(bc)
			if c.peek().t == tokSemi {
				c.advance()
			}
		}

		// Check for else
		if c.peek().t == tokIdent && c.peek().v == "else" {
			c.advance()
			elseJumpIdx := len(bc.code)
			bc.code = append(bc.code, instr{op: opJump})
			// Patch the if-false jump to here
			bc.code[jumpIdx].iarg = len(bc.code)

			if c.peek().t == tokLBrace {
				c.advance()
				for c.peek().t != tokRBrace && c.peek().t != tokEOF && !c.failed {
					c.compileStatement(bc)
					if c.peek().t == tokSemi {
						c.advance()
					}
				}
				c.expect(tokRBrace)
			} else {
				c.compileStatement(bc)
				if c.peek().t == tokSemi {
					c.advance()
				}
			}
			bc.code[elseJumpIdx].iarg = len(bc.code)
		} else {
			bc.code[jumpIdx].iarg = len(bc.code)
		}
		return
	}

	// const/let/var name = expr
	if t.t == tokIdent && (t.v == "const" || t.v == "let" || t.v == "var") {
		c.advance()
		if c.peek().t != tokIdent {
			c.fail()
			return
		}
		name := c.advance().v
		c.expect(tokAssign)
		c.compileExpr(bc)
		bc.code = append(bc.code, instr{op: opStoreVar, sarg: name})
		return
	}

	// Unsupported statement
	c.fail()
}

func (c *bcCompiler) compileExpr(bc *bytecode) {
	c.compileTernary(bc)
}

func (c *bcCompiler) compileTernary(bc *bytecode) {
	c.compileBinary(bc, 0)

	if c.peek().t == tokQuestion {
		c.advance()
		// Emit jump-if-false for the condition
		jumpFalse := len(bc.code)
		bc.code = append(bc.code, instr{op: opJumpIfFalse})

		c.compileExpr(bc) // consequent
		jumpEnd := len(bc.code)
		bc.code = append(bc.code, instr{op: opJump})

		c.expect(tokColon)
		bc.code[jumpFalse].iarg = len(bc.code)
		c.compileExpr(bc) // alternate
		bc.code[jumpEnd].iarg = len(bc.code)
	}
}

func (c *bcCompiler) compileBinary(bc *bytecode, minPrec int) {
	c.compileUnary(bc)

	for {
		t := c.peek().t
		prec := tokenPrec(t)
		if prec == 0 || prec < minPrec {
			break
		}
		c.advance()

		// Short-circuit operators need special handling in bytecode
		if t == tokAnd {
			// Duplicate top, jump-if-false to skip right side
			// Actually for bytecode: emit right side, then AND logic
			// Simple approach: fail and fall back to interpreter
			c.fail()
			return
		}
		if t == tokOr {
			c.fail()
			return
		}

		c.compileBinary(bc, prec+1)

		switch t {
		case tokPlus:
			bc.code = append(bc.code, instr{op: opAdd})
		case tokMinus:
			bc.code = append(bc.code, instr{op: opSub})
		case tokStar:
			bc.code = append(bc.code, instr{op: opMul})
		case tokSlash:
			bc.code = append(bc.code, instr{op: opDiv})
		case tokPercent:
			bc.code = append(bc.code, instr{op: opMod})
		case tokLt:
			bc.code = append(bc.code, instr{op: opLt})
		case tokLtEq:
			bc.code = append(bc.code, instr{op: opLtEq})
		case tokGt:
			bc.code = append(bc.code, instr{op: opGt})
		case tokGtEq:
			bc.code = append(bc.code, instr{op: opGtEq})
		case tokEqEq:
			bc.code = append(bc.code, instr{op: opEqEq})
		case tokEqEqEq:
			bc.code = append(bc.code, instr{op: opEqEqEq})
		case tokNotEq:
			bc.code = append(bc.code, instr{op: opNeq})
		case tokNotEqEq:
			bc.code = append(bc.code, instr{op: opNeqEq})
		default:
			c.fail()
			return
		}
	}
}

func (c *bcCompiler) compileUnary(bc *bytecode) {
	if c.peek().t == tokNot {
		c.advance()
		c.compileUnary(bc)
		bc.code = append(bc.code, instr{op: opNot})
		return
	}
	if c.peek().t == tokMinus {
		c.advance()
		c.compileUnary(bc)
		bc.code = append(bc.code, instr{op: opNeg})
		return
	}
	c.compilePrimary(bc)
}

func (c *bcCompiler) compilePrimary(bc *bytecode) {
	t := c.advance()

	switch t.t {
	case tokNum:
		bc.code = append(bc.code, instr{op: opLoadNum, narg: t.n})
	case tokStr:
		bc.code = append(bc.code, instr{op: opLoadStr, sarg: t.v})
	case tokIdent:
		switch t.v {
		case "true":
			bc.code = append(bc.code, instr{op: opTrue})
		case "false":
			bc.code = append(bc.code, instr{op: opFalse})
		case "null":
			bc.code = append(bc.code, instr{op: opNull})
		case "undefined":
			bc.code = append(bc.code, instr{op: opUndefined})
		default:
			// Variable or function call
			name := t.v
			if c.peek().t == tokLParen {
				// Function call: name(args...)
				c.advance() // skip (
				argc := 0
				for c.peek().t != tokRParen && c.peek().t != tokEOF && !c.failed {
					c.compileExpr(bc)
					argc++
					if c.peek().t == tokComma {
						c.advance()
					}
				}
				c.expect(tokRParen)
				bc.code = append(bc.code, instr{op: opCall, sarg: name, iarg: argc})
			} else {
				bc.code = append(bc.code, instr{op: opLoadVar, sarg: name})
			}
		}
	case tokLParen:
		c.compileExpr(bc)
		c.expect(tokRParen)
	default:
		c.fail()
	}
}

// ─── Bytecode Execution ─────────────────────────────────

func execBytecode(bc *bytecode, scope map[string]*Value) *Value {
	// Fixed-size stack on the Go stack — no heap allocation
	var stack [32]*Value
	sp := 0
	code := bc.code
	codeLen := len(code)
	ip := 0

	for ip < codeLen {
		inst := &code[ip]

		switch inst.op {
		case opLoadVar:
			if v, ok := scope[inst.sarg]; ok {
				stack[sp] = v
			} else {
				stack[sp] = Undefined
			}
			sp++

		case opLoadNum:
			stack[sp] = internNum(inst.narg)
			sp++

		case opLoadStr:
			stack[sp] = internStr(inst.sarg)
			sp++

		case opStoreVar:
			sp--
			scope[inst.sarg] = stack[sp]

		case opAdd:
			sp--
			a, b := stack[sp-1], stack[sp]
			if a.typ == TypeString || b.typ == TypeString {
				stack[sp-1] = newStr(a.toStr() + b.toStr())
			} else {
				stack[sp-1] = internNum(a.toNum() + b.toNum())
			}

		case opSub:
			sp--
			stack[sp-1] = internNum(stack[sp-1].toNum() - stack[sp].toNum())

		case opMul:
			sp--
			stack[sp-1] = internNum(stack[sp-1].toNum() * stack[sp].toNum())

		case opDiv:
			sp--
			bn := stack[sp].toNum()
			if bn != 0 {
				stack[sp-1] = internNum(stack[sp-1].toNum() / bn)
			} else {
				stack[sp-1] = internNum(0)
			}

		case opMod:
			sp--
			bn := stack[sp].toNum()
			if bn != 0 {
				stack[sp-1] = internNum(float64(int64(stack[sp-1].toNum()) % int64(bn)))
			} else {
				stack[sp-1] = internNum(0)
			}

		case opLt:
			sp--
			stack[sp-1] = newBool(stack[sp-1].toNum() < stack[sp].toNum())

		case opLtEq:
			sp--
			stack[sp-1] = newBool(stack[sp-1].toNum() <= stack[sp].toNum())

		case opGt:
			sp--
			stack[sp-1] = newBool(stack[sp-1].toNum() > stack[sp].toNum())

		case opGtEq:
			sp--
			stack[sp-1] = newBool(stack[sp-1].toNum() >= stack[sp].toNum())

		case opEqEq:
			sp--
			stack[sp-1] = newBool(looseEqual(stack[sp-1], stack[sp]))

		case opEqEqEq:
			sp--
			stack[sp-1] = newBool(strictEqual(stack[sp-1], stack[sp]))

		case opNeq:
			sp--
			stack[sp-1] = newBool(!looseEqual(stack[sp-1], stack[sp]))

		case opNeqEq:
			sp--
			stack[sp-1] = newBool(!strictEqual(stack[sp-1], stack[sp]))

		case opNot:
			stack[sp-1] = newBool(!stack[sp-1].truthy())

		case opNeg:
			stack[sp-1] = internNum(-stack[sp-1].toNum())

		case opTrue:
			stack[sp] = True
			sp++
		case opFalse:
			stack[sp] = False
			sp++
		case opNull:
			stack[sp] = Null
			sp++
		case opUndefined:
			stack[sp] = Undefined
			sp++

		case opReturn:
			if sp > 0 {
				return stack[sp-1]
			}
			return Undefined

		case opJumpIfFalse:
			sp--
			if !stack[sp].truthy() {
				ip = inst.iarg
				continue
			}

		case opJump:
			ip = inst.iarg
			continue

		case opCall:
			argc := inst.iarg
			sp -= argc
			args := stack[sp : sp+argc]

			fn, ok := scope[inst.sarg]
			if !ok || fn.typ != TypeFunc {
				stack[sp] = Undefined
				sp++
			} else if fn.native != nil {
				// Need to copy args for native functions (they may retain them)
				argsCopy := make([]*Value, argc)
				copy(argsCopy, args)
				stack[sp] = fn.native(argsCopy)
				sp++
			} else if fn.bc != nil {
				// Recursive bytecode call — reuse scope with save/restore
				params := fn.bc.params // cached param names
				saved := make([]*Value, len(params))
				for i, p := range params {
					saved[i] = scope[p] // save (may be nil)
					if i < argc {
						scope[p] = args[i]
					}
				}
				result := execBytecode(fn.bc, scope)
				// Restore
				for i, p := range params {
					if saved[i] != nil {
						scope[p] = saved[i]
					} else {
						delete(scope, p)
					}
				}
				stack[sp] = result
				sp++
			} else if fn.fnBody != "" {
				props := make(map[string]*Value, argc)
				if len(fn.fnParams) > 0 {
					params := splitParams(fn.fnParams[0])
					for i, p := range params {
						if i < argc {
							props[p] = args[i]
						}
					}
				}
				ev := &evaluator{scope: scope}
				stack[sp] = ev.callFunc(fn, props)
				sp++
			} else {
				stack[sp] = Undefined
				sp++
			}

		case opPop:
			if sp > 0 {
				sp--
			}
		}

		ip++
	}

	return Undefined
}

// splitParams splits a comma-separated param string into individual param names.
func splitParams(s string) []string {
	if s == "" {
		return nil
	}
	parts := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			p := trimSpace(s[start:i])
			if p != "" {
				parts = append(parts, p)
			}
			start = i + 1
		}
	}
	p := trimSpace(s[start:])
	if p != "" {
		parts = append(parts, p)
	}
	return parts
}

func trimSpace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
		i++
	}
	j := len(s)
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\n') {
		j--
	}
	return s[i:j]
}
