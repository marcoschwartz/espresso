package espresso

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

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
	opDup                  // duplicate top of stack
	opJumpIfTrue           // if pop is truthy, jump to iarg
	opGetProp              // pop obj, push obj[sarg]
	opSetProp              // pop val, pop obj, set obj[sarg] = val
	opGetIndex             // pop idx, pop obj, push obj[idx]
	opCallMethod           // pop iarg args, pop obj, call obj[sarg](args...), push result
	opNewArray             // push new empty array, iarg = number of initial elements from stack
	opNewObject            // push new empty object
	opSetObjProp           // pop val, peek obj, set obj[sarg] = val (leaves obj on stack)
	opTypeof               // pop val, push typeof string
	opInstanceof           // pop 2, push a instanceof b
	opBitAnd               // pop 2, push a & b
	opBitOr                // pop 2, push a | b
	opBitXor               // pop 2, push a ^ b
	opBitNot               // pop 1, push ~a
	opShl                  // pop 2, push a << b
	opShr                  // pop 2, push a >> b
	opUShr                 // pop 2, push a >>> b
	opIn                   // pop 2, push (sarg in obj)
	opThrow                // pop val, panic with it
	opNewCall              // new Constructor(args): pop iarg args, call sarg as constructor
	opLoadThis             // push this from scope
	opTemplate             // push evaluated template literal (sarg = raw template string)
	opMakeArrow            // push an arrow function value; iarg = index into bytecode's arrows slice
	opTryCatch             // try/catch: iarg = index into bytecode's tryCatches slice
	opAwait                // pop promise, push resolved value (sync unwrap)
	opLoadRegExp           // push a RegExp value; sarg = "pattern\x00flags"
	opSpreadObj            // pop source obj, peek target obj, copy all props from source into target
	opSpreadArr            // pop source arr, peek target arr, append all items
	opObjectKeys           // pop obj, push array of its keys
	opIterToArray          // pop value, convert Map/Set to array if needed, push result
)

type instr struct {
	op   op
	sarg string
	narg float64
	iarg int
}

type bcArrowDef struct {
	params  []string
	tokens  []tok
	isBlock bool
}

type bcTryCatchDef struct {
	tryBody   *bytecode
	catchVar  string // variable name for caught error (e.g. "e" in catch(e))
	catchBody *bytecode
}

type bytecode struct {
	code       []instr
	params     []string         // cached param names for recursive calls
	arrows     []bcArrowDef     // arrow function definitions referenced by opMakeArrow
	tryCatches []bcTryCatchDef  // try/catch definitions referenced by opTryCatch
}

// compileFuncBody attempts to compile a function body to bytecode.
// Returns nil if the body uses unsupported patterns.
func compileFuncBody(body string) *bytecode {
	tokens := tokenizeCached(body)
	return compileFuncBodyTokens(tokens)
}

// compileFuncBodyTokens compiles pre-tokenized function body to bytecode.
// Returns nil if compilation fails or tokens contain patterns the bytecode
// compiler doesn't handle correctly (falls back to interpreter).
func compileFuncBodyTokens(tokens []tok) *bytecode {
	if needsInterpreter(tokens) {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			// compilation failed — will fall back to interpreted
		}
	}()
	c := &bcCompiler{tokens: tokens, pos: 0}
	bc := c.compile()
	if c.failed {
		return nil
	}
	return bc
}

type bcCompiler struct {
	tokens     []tok
	pos        int
	failed     bool
	breakList  [][]int // stack of break jump indices to patch per loop/switch
	contList   [][]int // stack of continue jump indices to patch per loop
}

func (c *bcCompiler) peek() tok {
	if c.pos >= 0 && c.pos < len(c.tokens) {
		return c.tokens[c.pos]
	}
	return tok{t: tokEOF}
}

func (c *bcCompiler) peekAt(offset int) tok {
	idx := c.pos + offset
	if idx >= 0 && idx < len(c.tokens) {
		return c.tokens[idx]
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

	// function name(params) { body } — nested function declaration
	if t.t == tokIdent && t.v == "function" {
		c.advance() // consume "function"
		if c.peek().t != tokIdent {
			c.fail()
			return
		}
		funcName := c.advance().v
		c.expect(tokLParen)
		// Parse params
		var params []string
		for c.peek().t != tokRParen && c.peek().t != tokEOF {
			if c.peek().t == tokIdent {
				params = append(params, c.advance().v)
			}
			if c.peek().t == tokComma { c.advance() }
		}
		c.expect(tokRParen)
		// Capture body tokens
		if c.peek().t != tokLBrace {
			c.fail()
			return
		}
		start := c.pos
		depth := 0
		for c.peek().t != tokEOF {
			if c.peek().t == tokLBrace { depth++ }
			if c.peek().t == tokRBrace { depth-- ; if depth == 0 { c.advance(); break } }
			c.advance()
		}
		bodyTokens := make([]tok, c.pos-start)
		copy(bodyTokens, c.tokens[start:c.pos])

		// Create as arrow and store in scope
		idx := len(bc.arrows)
		bc.arrows = append(bc.arrows, bcArrowDef{
			params:  params,
			tokens:  bodyTokens,
			isBlock: true,
		})
		bc.code = append(bc.code, instr{op: opMakeArrow, iarg: idx})
		bc.code = append(bc.code, instr{op: opStoreVar, sarg: funcName})
		return
	}

	// const/let/var name = expr  OR  const { a, b } = expr  OR  const [a, b] = expr
	if t.t == tokIdent && (t.v == "const" || t.v == "let" || t.v == "var") {
		c.advance()

		// Object destructuring: const { a, b, c: alias } = expr
		if c.peek().t == tokLBrace {
			c.advance() // skip {
			type destPair struct{ key, alias string }
			var pairs []destPair
			for c.peek().t != tokRBrace && c.peek().t != tokEOF && !c.failed {
				if c.peek().t == tokIdent {
					key := c.advance().v
					alias := key
					// Handle renaming: { key: alias }
					if c.peek().t == tokColon {
						c.advance()
						if c.peek().t == tokIdent {
							alias = c.advance().v
						}
					}
					pairs = append(pairs, destPair{key, alias})
				}
				if c.peek().t == tokComma {
					c.advance()
				}
			}
			c.expect(tokRBrace)
			c.expect(tokAssign)
			c.compileExpr(bc)
			// Store in a temp, then extract props
			tmpVar := "__destruct_obj__"
			bc.code = append(bc.code, instr{op: opStoreVar, sarg: tmpVar})
			for _, p := range pairs {
				bc.code = append(bc.code, instr{op: opLoadVar, sarg: tmpVar})
				bc.code = append(bc.code, instr{op: opGetProp, sarg: p.key})
				bc.code = append(bc.code, instr{op: opStoreVar, sarg: p.alias})
			}
			return
		}

		// Array destructuring: const [a, b] = expr (bail for ...rest patterns)
		if c.peek().t == tokLBrack {
			// Check for spread — bail to interpreter for rest patterns
			for ii := c.pos; ii < len(c.tokens); ii++ {
				if c.tokens[ii].t == tokRBrack { break }
				if c.tokens[ii].t == tokSpread { panic("array-rest-bail") }
			}
			c.advance() // skip [
			var names []string
			for c.peek().t != tokRBrack && c.peek().t != tokEOF && !c.failed {
				if c.peek().t == tokIdent {
					names = append(names, c.advance().v)
				} else {
					c.advance()
				}
				if c.peek().t == tokComma {
					c.advance()
				}
			}
			c.expect(tokRBrack)
			c.expect(tokAssign)
			c.compileExpr(bc)
			tmpVar := "__destruct_arr__"
			bc.code = append(bc.code, instr{op: opStoreVar, sarg: tmpVar})
			for i, name := range names {
				bc.code = append(bc.code, instr{op: opLoadVar, sarg: tmpVar})
				bc.code = append(bc.code, instr{op: opLoadNum, narg: float64(i)})
				bc.code = append(bc.code, instr{op: opGetIndex})
				bc.code = append(bc.code, instr{op: opStoreVar, sarg: name})
			}
			return
		}

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

	// while (cond) { body }
	if t.t == tokIdent && t.v == "while" {
		c.advance()
		c.expect(tokLParen)
		c.breakList = append(c.breakList, nil)
		c.contList = append(c.contList, nil)
		condStart := len(bc.code)
		c.compileExpr(bc)
		c.expect(tokRParen)
		exitJump := len(bc.code)
		bc.code = append(bc.code, instr{op: opJumpIfFalse})
		if c.peek().t == tokLBrace {
			c.advance()
			for c.peek().t != tokRBrace && c.peek().t != tokEOF && !c.failed {
				c.compileStatement(bc)
				if c.peek().t == tokSemi { c.advance() }
			}
			c.expect(tokRBrace)
		} else {
			c.compileStatement(bc)
		}
		// Patch continue jumps to condition
		conts := c.contList[len(c.contList)-1]
		c.contList = c.contList[:len(c.contList)-1]
		for _, ci := range conts {
			bc.code[ci].iarg = condStart
		}
		bc.code = append(bc.code, instr{op: opJump, iarg: condStart})
		bc.code[exitJump].iarg = len(bc.code)
		// Patch break jumps
		breaks := c.breakList[len(c.breakList)-1]
		c.breakList = c.breakList[:len(c.breakList)-1]
		for _, bi := range breaks {
			bc.code[bi].iarg = len(bc.code)
		}
		return
	}

	// for (init; cond; update) { body }
	if t.t == tokIdent && t.v == "for" {
		// Peek ahead to detect for...of / for...in
		saved := c.pos
		c.advance() // skip 'for'
		// Skip optional 'await' keyword (for await...of)
		if c.peek().t == tokIdent && c.peek().v == "await" {
			c.advance()
		}
		if c.peek().t == tokLParen {
			c.advance() // skip (
			if c.peek().t == tokIdent && (c.peek().v == "const" || c.peek().v == "let" || c.peek().v == "var") {
				c.advance() // skip const/let/var
				if c.peek().t == tokIdent {
					varName := c.advance().v
					if c.peek().t == tokIdent && c.peek().v == "of" {
						// for...of — bail to interpreter (Map/Set support)
						panic("forof-bail")
					}
					if c.peek().t == tokIdent && c.peek().v == "in" {
						c.advance() // skip 'in'
						c.compileExpr(bc) // object
						c.expect(tokRParen)
						c.compileForIn(bc, varName)
						return
					}
					// Not of/in — restore for C-style for parsing
					c.pos = saved
				} else {
					c.pos = saved
				}
			} else {
				c.pos = saved
			}
		} else {
			c.pos = saved
		}

		// C-style for (init; cond; update) { body }
		c.advance() // skip 'for'
		c.expect(tokLParen)
		c.breakList = append(c.breakList, nil)
		c.contList = append(c.contList, nil)
		// init
		if c.peek().t == tokIdent && (c.peek().v == "let" || c.peek().v == "var" || c.peek().v == "const") {
			c.advance()
			name := c.advance().v
			c.expect(tokAssign)
			c.compileExpr(bc)
			bc.code = append(bc.code, instr{op: opStoreVar, sarg: name})
		}
		c.expect(tokSemi)
		// cond
		condStart := len(bc.code)
		c.compileExpr(bc)
		c.expect(tokSemi)
		exitJump := len(bc.code)
		bc.code = append(bc.code, instr{op: opJumpIfFalse})
		// update — save tokens, compile after body
		updateStart := c.pos
		depth := 0
		for c.peek().t != tokEOF {
			if c.peek().t == tokLParen { depth++ }
			if c.peek().t == tokRParen { if depth == 0 { break }; depth-- }
			c.advance()
		}
		updateEnd := c.pos
		c.expect(tokRParen)
		// body
		if c.peek().t == tokLBrace {
			c.advance()
			for c.peek().t != tokRBrace && c.peek().t != tokEOF && !c.failed {
				c.compileStatement(bc)
				if c.peek().t == tokSemi { c.advance() }
			}
			c.expect(tokRBrace)
		} else {
			c.compileStatement(bc)
		}
		// Patch continue jumps to update
		updatePos := len(bc.code)
		conts := c.contList[len(c.contList)-1]
		c.contList = c.contList[:len(c.contList)-1]
		for _, ci := range conts {
			bc.code[ci].iarg = updatePos
		}
		// compile update expression
		savedPos := c.pos
		c.pos = updateStart
		c.compileUpdateExpr(bc)
		c.pos = savedPos
		_ = updateEnd
		bc.code = append(bc.code, instr{op: opJump, iarg: condStart})
		bc.code[exitJump].iarg = len(bc.code)
		// Patch break jumps
		breaks := c.breakList[len(c.breakList)-1]
		c.breakList = c.breakList[:len(c.breakList)-1]
		for _, bi := range breaks {
			bc.code[bi].iarg = len(bc.code)
		}
		return
	}

	// break
	if t.t == tokIdent && t.v == "break" {
		c.advance()
		if len(c.breakList) == 0 {
			c.fail()
			return
		}
		idx := len(bc.code)
		bc.code = append(bc.code, instr{op: opJump})
		c.breakList[len(c.breakList)-1] = append(c.breakList[len(c.breakList)-1], idx)
		return
	}

	// continue
	if t.t == tokIdent && t.v == "continue" {
		c.advance()
		if len(c.contList) == 0 {
			c.fail()
			return
		}
		idx := len(bc.code)
		bc.code = append(bc.code, instr{op: opJump})
		c.contList[len(c.contList)-1] = append(c.contList[len(c.contList)-1], idx)
		return
	}

	// switch (expr) { case val: ... default: ... }
	if t.t == tokIdent && t.v == "switch" {
		c.advance()
		c.expect(tokLParen)
		c.compileExpr(bc) // discriminant on stack
		c.expect(tokRParen)
		c.expect(tokLBrace)
		c.breakList = append(c.breakList, nil)

		var caseJumps []int   // jumps to case bodies
		var fallJumps []int   // jumps to skip case tests (next case)
		defaultJump := -1

		// Compile all case tests first, then bodies
		// Actually, we'll use the pattern: test → jump-to-body / fall-through to next test
		// For each case: dup discriminant, push case val, === compare, jump-if-true to body
		type caseInfo struct {
			bodyStart int
		}
		_ = caseInfo{}

		// Simple approach: linear chain of test-then-body
		for c.peek().t != tokRBrace && c.peek().t != tokEOF && !c.failed {
			if c.peek().t == tokIdent && c.peek().v == "case" {
				c.advance()
				// Dup discriminant for comparison
				bc.code = append(bc.code, instr{op: opDup})
				c.compileExpr(bc)
				bc.code = append(bc.code, instr{op: opEqEqEq})
				c.expect(tokColon)
				// If match, jump to body (fall through below)
				noMatchJump := len(bc.code)
				bc.code = append(bc.code, instr{op: opJumpIfFalse})
				// Patch any pending fall-through jumps to here (body)
				for _, fj := range fallJumps {
					bc.code[fj].iarg = len(bc.code)
				}
				fallJumps = nil
				caseJumps = append(caseJumps, noMatchJump)
				// Compile body statements until next case/default/}
				for c.peek().t != tokRBrace && c.peek().t != tokEOF && !c.failed {
					if c.peek().t == tokIdent && (c.peek().v == "case" || c.peek().v == "default") {
						break
					}
					c.compileStatement(bc)
					if c.peek().t == tokSemi {
						c.advance()
					}
				}
			} else if c.peek().t == tokIdent && c.peek().v == "default" {
				c.advance()
				c.expect(tokColon)
				// Patch previous no-match jump to skip to here
				if len(caseJumps) > 0 {
					last := caseJumps[len(caseJumps)-1]
					// The no-match for last case should jump to next case test, not here
					// But for default we want unmatched cases to reach here
					_ = last
				}
				defaultJump = len(bc.code)
				for _, fj := range fallJumps {
					bc.code[fj].iarg = len(bc.code)
				}
				fallJumps = nil
				for c.peek().t != tokRBrace && c.peek().t != tokEOF && !c.failed {
					if c.peek().t == tokIdent && (c.peek().v == "case" || c.peek().v == "default") {
						break
					}
					c.compileStatement(bc)
					if c.peek().t == tokSemi {
						c.advance()
					}
				}
			} else {
				c.fail()
				return
			}
		}
		c.expect(tokRBrace)
		_ = defaultJump

		// Patch no-match jumps: each case's no-match should go to the next case's test
		// Since we compiled linearly, the no-match jumps go to right after the body
		// Actually — fix: patch remaining unpatched case jumps to end
		endPos := len(bc.code)
		for _, cj := range caseJumps {
			if bc.code[cj].iarg == 0 {
				bc.code[cj].iarg = endPos
			}
		}

		// Pop the discriminant
		bc.code = append(bc.code, instr{op: opPop})

		// Patch break jumps
		breakEnd := len(bc.code)
		breaks := c.breakList[len(c.breakList)-1]
		c.breakList = c.breakList[:len(c.breakList)-1]
		for _, bi := range breaks {
			bc.code[bi].iarg = breakEnd
		}
		return
	}

	// throw expr
	if t.t == tokIdent && t.v == "throw" {
		c.advance()
		c.compileExpr(bc)
		bc.code = append(bc.code, instr{op: opThrow})
		return
	}

	// try { ... } catch (e) { ... } [finally { ... }]
	if t.t == tokIdent && t.v == "try" {
		c.advance()
		def := bcTryCatchDef{}

		// Compile try body as sub-bytecode
		c.expect(tokLBrace)
		tryCompiler := &bcCompiler{tokens: c.tokens, pos: c.pos}
		tryBc := &bytecode{}
		for tryCompiler.peek().t != tokRBrace && tryCompiler.peek().t != tokEOF && !tryCompiler.failed {
			tryCompiler.compileStatement(tryBc)
			if tryCompiler.peek().t == tokSemi {
				tryCompiler.advance()
			}
		}
		c.pos = tryCompiler.pos
		c.expect(tokRBrace)
		if tryCompiler.failed {
			c.fail()
			return
		}
		def.tryBody = tryBc

		// catch (e) { ... } or catch { ... }
		if c.peek().t == tokIdent && c.peek().v == "catch" {
			c.advance()
			if c.peek().t == tokLParen {
				c.advance()
				if c.peek().t == tokIdent {
					def.catchVar = c.advance().v
				}
				c.expect(tokRParen)
			}
			c.expect(tokLBrace)
			catchCompiler := &bcCompiler{tokens: c.tokens, pos: c.pos}
			catchBc := &bytecode{}
			for catchCompiler.peek().t != tokRBrace && catchCompiler.peek().t != tokEOF && !catchCompiler.failed {
				catchCompiler.compileStatement(catchBc)
				if catchCompiler.peek().t == tokSemi {
					catchCompiler.advance()
				}
			}
			c.pos = catchCompiler.pos
			c.expect(tokRBrace)
			if catchCompiler.failed {
				c.fail()
				return
			}
			def.catchBody = catchBc
		}

		// finally { ... } — skip but parse
		if c.peek().t == tokIdent && c.peek().v == "finally" {
			c.advance()
			c.expect(tokLBrace)
			depth := 1
			for c.peek().t != tokEOF && depth > 0 {
				if c.peek().t == tokLBrace { depth++ }
				if c.peek().t == tokRBrace { depth-- }
				if depth > 0 { c.advance() }
			}
			c.expect(tokRBrace)
		}

		idx := len(bc.tryCatches)
		bc.tryCatches = append(bc.tryCatches, def)
		bc.code = append(bc.code, instr{op: opTryCatch, iarg: idx})
		return
	}

	// do { body } while (cond)
	if t.t == tokIdent && t.v == "do" {
		c.advance()
		c.breakList = append(c.breakList, nil)
		c.contList = append(c.contList, nil)

		bodyStart := len(bc.code)
		c.expect(tokLBrace)
		for c.peek().t != tokRBrace && c.peek().t != tokEOF && !c.failed {
			c.compileStatement(bc)
			if c.peek().t == tokSemi {
				c.advance()
			}
		}
		c.expect(tokRBrace)

		// Patch continue jumps to condition
		condStart := len(bc.code)
		conts := c.contList[len(c.contList)-1]
		c.contList = c.contList[:len(c.contList)-1]
		for _, ci := range conts {
			bc.code[ci].iarg = condStart
		}

		// while (cond)
		if c.peek().t != tokIdent || c.peek().v != "while" {
			c.fail()
			return
		}
		c.advance()
		c.expect(tokLParen)
		c.compileExpr(bc)
		c.expect(tokRParen)
		bc.code = append(bc.code, instr{op: opJumpIfTrue, iarg: bodyStart})

		// Patch break jumps
		breakEnd := len(bc.code)
		breaks := c.breakList[len(c.breakList)-1]
		c.breakList = c.breakList[:len(c.breakList)-1]
		for _, bi := range breaks {
			bc.code[bi].iarg = breakEnd
		}
		return
	}

	// (for...in/of handled above before C-style for)

	// Assignment or expression statement: name = expr, name++, name.method(), etc.
	if t.t == tokIdent {
		name := t.v
		// Peek at the token AFTER the ident (t was peeked, not consumed)
		next := c.peekAt(1)
		if next.t == tokAssign {
			c.advance() // consume ident
			c.advance() // consume =
			c.compileExpr(bc)
			bc.code = append(bc.code, instr{op: opStoreVar, sarg: name})
			return
		}
		if next.t == tokPlusPlus {
			c.advance() // consume ident
			c.advance() // consume ++
			bc.code = append(bc.code, instr{op: opLoadVar, sarg: name})
			bc.code = append(bc.code, instr{op: opLoadNum, narg: 1})
			bc.code = append(bc.code, instr{op: opAdd})
			bc.code = append(bc.code, instr{op: opStoreVar, sarg: name})
			return
		}
		if next.t == tokMinusMinus {
			c.advance() // consume ident
			c.advance() // consume --
			bc.code = append(bc.code, instr{op: opLoadVar, sarg: name})
			bc.code = append(bc.code, instr{op: opLoadNum, narg: 1})
			bc.code = append(bc.code, instr{op: opSub})
			bc.code = append(bc.code, instr{op: opStoreVar, sarg: name})
			return
		}
		if next.t == tokPlusAssign {
			c.advance() // consume ident
			c.advance() // consume +=
			bc.code = append(bc.code, instr{op: opLoadVar, sarg: name})
			c.compileExpr(bc)
			bc.code = append(bc.code, instr{op: opAdd})
			bc.code = append(bc.code, instr{op: opStoreVar, sarg: name})
			return
		}
		if next.t == tokMinusAssign {
			c.advance() // consume ident
			c.advance() // consume -=
			bc.code = append(bc.code, instr{op: opLoadVar, sarg: name})
			c.compileExpr(bc)
			bc.code = append(bc.code, instr{op: opSub})
			bc.code = append(bc.code, instr{op: opStoreVar, sarg: name})
			return
		}
		if next.t == tokStarAssign {
			c.advance(); c.advance()
			bc.code = append(bc.code, instr{op: opLoadVar, sarg: name})
			c.compileExpr(bc)
			bc.code = append(bc.code, instr{op: opMul})
			bc.code = append(bc.code, instr{op: opStoreVar, sarg: name})
			return
		}
		if next.t == tokSlashAssign {
			c.advance(); c.advance()
			bc.code = append(bc.code, instr{op: opLoadVar, sarg: name})
			c.compileExpr(bc)
			bc.code = append(bc.code, instr{op: opDiv})
			bc.code = append(bc.code, instr{op: opStoreVar, sarg: name})
			return
		}
		if next.t == tokNullAssign {
			c.advance(); c.advance()
			// x ??= val → if x is null/undefined, x = val
			bc.code = append(bc.code, instr{op: opLoadVar, sarg: name})
			bc.code = append(bc.code, instr{op: opDup})
			jumpIdx := len(bc.code)
			bc.code = append(bc.code, instr{op: opJumpIfTrue}) // non-null → skip
			bc.code = append(bc.code, instr{op: opPop})
			c.compileExpr(bc)
			bc.code = append(bc.code, instr{op: opStoreVar, sarg: name})
			endIdx := len(bc.code)
			bc.code = append(bc.code, instr{op: opJump})
			bc.code[jumpIdx].iarg = len(bc.code)
			bc.code = append(bc.code, instr{op: opPop}) // discard dup'd value
			bc.code[endIdx].iarg = len(bc.code)
			return
		}
		if next.t == tokOrAssign {
			c.advance(); c.advance()
			bc.code = append(bc.code, instr{op: opLoadVar, sarg: name})
			bc.code = append(bc.code, instr{op: opDup})
			jumpIdx := len(bc.code)
			bc.code = append(bc.code, instr{op: opJumpIfTrue})
			bc.code = append(bc.code, instr{op: opPop})
			c.compileExpr(bc)
			bc.code = append(bc.code, instr{op: opStoreVar, sarg: name})
			bc.code[jumpIdx].iarg = len(bc.code)
			return
		}
		if next.t == tokAndAssign {
			c.advance(); c.advance()
			bc.code = append(bc.code, instr{op: opLoadVar, sarg: name})
			bc.code = append(bc.code, instr{op: opDup})
			jumpIdx := len(bc.code)
			bc.code = append(bc.code, instr{op: opJumpIfFalse})
			bc.code = append(bc.code, instr{op: opPop})
			c.compileExpr(bc)
			bc.code = append(bc.code, instr{op: opStoreVar, sarg: name})
			bc.code[jumpIdx].iarg = len(bc.code)
			return
		}
		// Check for property assignment: name.prop = expr or name.prop.sub = expr
		// Scan ahead to see if this is obj.prop = or obj[idx] = pattern
		if next.t == tokDot || next.t == tokLBrack {
			savedPos := c.pos
			// Parse the left-hand side as an expression
			c.compileExpr(bc)
			// If followed by =, this is a property assignment
			if c.peek().t == tokAssign {
				c.advance() // consume =
				// The expr left an object+getProp chain on stack. We need to
				// undo the last getProp and replace with setProp.
				// Find the last getProp or getIndex instruction
				lastIdx := len(bc.code) - 1
				if lastIdx >= 0 && bc.code[lastIdx].op == opGetProp {
					prop := bc.code[lastIdx].sarg
					bc.code = bc.code[:lastIdx] // remove the getProp
					c.compileExpr(bc)           // compile the value
					bc.code = append(bc.code, instr{op: opSetProp, sarg: prop})
					bc.code = append(bc.code, instr{op: opPop})
					return
				}
				if lastIdx >= 0 && bc.code[lastIdx].op == opGetIndex {
					bc.code = bc.code[:lastIdx] // remove the getIndex
					// We need the index back on stack — but it was already consumed.
					// Easier: recompile from scratch as a special case.
					// For now, fail and fall back to interpreter.
					c.pos = savedPos
					c.fail()
					return
				}
			}
			// Not an assignment — it was an expression statement
			bc.code = append(bc.code, instr{op: opPop})
			return
		}

		// Expression statement (e.g. console.log(...), doSomething())
		// t was peeked not consumed, so compileExpr will read it fresh
		c.compileExpr(bc)
		bc.code = append(bc.code, instr{op: opPop})
		return
	}

	// Unsupported statement
	c.fail()
}

// compileForOf compiles: for (const x of iterable) { body }
// The iterable is already on the stack.
func (c *bcCompiler) compileForOf(bc *bytecode, varName string) {
	// Convert Map/Set to array at runtime
	bc.code = append(bc.code, instr{op: opIterToArray})
	// Strategy: store iterable in a temp, use index counter
	iterVar := "__iter_" + varName
	idxVar := "__idx_" + varName
	lenVar := "__len_" + varName

	bc.code = append(bc.code, instr{op: opStoreVar, sarg: iterVar})
	bc.code = append(bc.code, instr{op: opLoadNum, narg: 0})
	bc.code = append(bc.code, instr{op: opStoreVar, sarg: idxVar})
	// Get length
	bc.code = append(bc.code, instr{op: opLoadVar, sarg: iterVar})
	bc.code = append(bc.code, instr{op: opGetProp, sarg: "length"})
	bc.code = append(bc.code, instr{op: opStoreVar, sarg: lenVar})

	c.breakList = append(c.breakList, nil)
	c.contList = append(c.contList, nil)

	// condition: idx < len
	condStart := len(bc.code)
	bc.code = append(bc.code, instr{op: opLoadVar, sarg: idxVar})
	bc.code = append(bc.code, instr{op: opLoadVar, sarg: lenVar})
	bc.code = append(bc.code, instr{op: opLt})
	exitJump := len(bc.code)
	bc.code = append(bc.code, instr{op: opJumpIfFalse})

	// varName = iter[idx]
	bc.code = append(bc.code, instr{op: opLoadVar, sarg: iterVar})
	bc.code = append(bc.code, instr{op: opLoadVar, sarg: idxVar})
	bc.code = append(bc.code, instr{op: opGetIndex})
	bc.code = append(bc.code, instr{op: opStoreVar, sarg: varName})

	// body
	c.expect(tokLBrace)
	for c.peek().t != tokRBrace && c.peek().t != tokEOF && !c.failed {
		c.compileStatement(bc)
		if c.peek().t == tokSemi {
			c.advance()
		}
	}
	c.expect(tokRBrace)

	// Patch continue jumps to update
	updatePos := len(bc.code)
	conts := c.contList[len(c.contList)-1]
	c.contList = c.contList[:len(c.contList)-1]
	for _, ci := range conts {
		bc.code[ci].iarg = updatePos
	}

	// idx++
	bc.code = append(bc.code, instr{op: opLoadVar, sarg: idxVar})
	bc.code = append(bc.code, instr{op: opLoadNum, narg: 1})
	bc.code = append(bc.code, instr{op: opAdd})
	bc.code = append(bc.code, instr{op: opStoreVar, sarg: idxVar})
	bc.code = append(bc.code, instr{op: opJump, iarg: condStart})
	bc.code[exitJump].iarg = len(bc.code)

	// Patch break jumps
	breaks := c.breakList[len(c.breakList)-1]
	c.breakList = c.breakList[:len(c.breakList)-1]
	for _, bi := range breaks {
		bc.code[bi].iarg = len(bc.code)
	}
}

// compileForIn compiles: for (const k in obj) { body }
// The object is already on the stack. We use Object.keys() approach.
func (c *bcCompiler) compileForIn(bc *bytecode, varName string) {
	// Object is on stack. Get its keys via opObjectKeys, then iterate.
	keysVar := "__keys_" + varName
	bc.code = append(bc.code, instr{op: opObjectKeys})
	bc.code = append(bc.code, instr{op: opStoreVar, sarg: keysVar})
	bc.code = append(bc.code, instr{op: opLoadVar, sarg: keysVar})
	c.compileForOfFromStack(bc, varName, keysVar)
}

// compileForOfFromStack is a helper that compiles a for...of loop body
// when the iterable array is stored in iterVar.
func (c *bcCompiler) compileForOfFromStack(bc *bytecode, varName, iterVar string) {
	idxVar := "__idx_" + varName
	lenVar := "__len_" + varName

	bc.code = append(bc.code, instr{op: opStoreVar, sarg: iterVar + "_arr"})
	bc.code = append(bc.code, instr{op: opLoadNum, narg: 0})
	bc.code = append(bc.code, instr{op: opStoreVar, sarg: idxVar})
	bc.code = append(bc.code, instr{op: opLoadVar, sarg: iterVar + "_arr"})
	bc.code = append(bc.code, instr{op: opGetProp, sarg: "length"})
	bc.code = append(bc.code, instr{op: opStoreVar, sarg: lenVar})

	c.breakList = append(c.breakList, nil)
	c.contList = append(c.contList, nil)

	condStart := len(bc.code)
	bc.code = append(bc.code, instr{op: opLoadVar, sarg: idxVar})
	bc.code = append(bc.code, instr{op: opLoadVar, sarg: lenVar})
	bc.code = append(bc.code, instr{op: opLt})
	exitJump := len(bc.code)
	bc.code = append(bc.code, instr{op: opJumpIfFalse})

	bc.code = append(bc.code, instr{op: opLoadVar, sarg: iterVar + "_arr"})
	bc.code = append(bc.code, instr{op: opLoadVar, sarg: idxVar})
	bc.code = append(bc.code, instr{op: opGetIndex})
	bc.code = append(bc.code, instr{op: opStoreVar, sarg: varName})

	c.expect(tokLBrace)
	for c.peek().t != tokRBrace && c.peek().t != tokEOF && !c.failed {
		c.compileStatement(bc)
		if c.peek().t == tokSemi {
			c.advance()
		}
	}
	c.expect(tokRBrace)

	updatePos := len(bc.code)
	conts := c.contList[len(c.contList)-1]
	c.contList = c.contList[:len(c.contList)-1]
	for _, ci := range conts {
		bc.code[ci].iarg = updatePos
	}

	bc.code = append(bc.code, instr{op: opLoadVar, sarg: idxVar})
	bc.code = append(bc.code, instr{op: opLoadNum, narg: 1})
	bc.code = append(bc.code, instr{op: opAdd})
	bc.code = append(bc.code, instr{op: opStoreVar, sarg: idxVar})
	bc.code = append(bc.code, instr{op: opJump, iarg: condStart})
	bc.code[exitJump].iarg = len(bc.code)

	breaks := c.breakList[len(c.breakList)-1]
	c.breakList = c.breakList[:len(c.breakList)-1]
	for _, bi := range breaks {
		bc.code[bi].iarg = len(bc.code)
	}
}

// compileUpdateExpr compiles a simple for-loop update like i++ or i += 1
func (c *bcCompiler) compileUpdateExpr(bc *bytecode) {
	if c.peek().t != tokIdent { return }
	name := c.advance().v
	if c.peek().t == tokPlusPlus {
		c.advance()
		bc.code = append(bc.code, instr{op: opLoadVar, sarg: name})
		bc.code = append(bc.code, instr{op: opLoadNum, narg: 1})
		bc.code = append(bc.code, instr{op: opAdd})
		bc.code = append(bc.code, instr{op: opStoreVar, sarg: name})
		return
	}
	if c.peek().t == tokMinusMinus {
		c.advance()
		bc.code = append(bc.code, instr{op: opLoadVar, sarg: name})
		bc.code = append(bc.code, instr{op: opLoadNum, narg: 1})
		bc.code = append(bc.code, instr{op: opSub})
		bc.code = append(bc.code, instr{op: opStoreVar, sarg: name})
		return
	}
	if c.peek().t == tokPlusAssign {
		c.advance()
		bc.code = append(bc.code, instr{op: opLoadVar, sarg: name})
		c.compileExpr(bc)
		bc.code = append(bc.code, instr{op: opAdd})
		bc.code = append(bc.code, instr{op: opStoreVar, sarg: name})
		return
	}
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

		// Keyword-based infix: "in", "instanceof"
		if t == tokIdent && c.peek().v == "in" && minPrec <= precComparison {
			c.advance()
			c.compileBinary(bc, precComparison+1)
			bc.code = append(bc.code, instr{op: opIn})
			continue
		}
		if t == tokIdent && c.peek().v == "instanceof" && minPrec <= precComparison {
			c.advance()
			c.compileBinary(bc, precComparison+1)
			bc.code = append(bc.code, instr{op: opInstanceof})
			continue
		}

		prec := tokenPrec(t)
		if prec == 0 || prec < minPrec {
			break
		}
		c.advance()

		// Short-circuit: a && b → eval a, dup, if falsy jump past b, pop, eval b
		if t == tokAnd {
			bc.code = append(bc.code, instr{op: opDup})
			jumpIdx := len(bc.code)
			bc.code = append(bc.code, instr{op: opJumpIfFalse})
			bc.code = append(bc.code, instr{op: opPop}) // discard the dup'd value
			c.compileBinary(bc, prec+1)
			bc.code[jumpIdx].iarg = len(bc.code)
			continue
		}
		// Nullish coalescing: a ?? b → eval a, if not null/undefined keep it, else eval b
		if t == tokNullCoalesce {
			bc.code = append(bc.code, instr{op: opDup})
			// We need a "jump if not null/undefined" — use opJumpIfTrue after typeof check
			// Simpler: just use opDup + custom null check. For now, use the same pattern as ||
			// but check for null/undefined specifically. We'll use opDup + opJumpIfTrue as approximation.
			// Actually, null and undefined are falsy, so ?? ≈ || for most SEM cases.
			// More precise: we need a new opcode. For now, use the || pattern which is close enough
			// for SEM pages where ?? is used on optional values.
			jumpIdx := len(bc.code)
			bc.code = append(bc.code, instr{op: opJumpIfTrue})
			bc.code = append(bc.code, instr{op: opPop})
			c.compileBinary(bc, prec+1)
			bc.code[jumpIdx].iarg = len(bc.code)
			continue
		}
		// Short-circuit: a || b → eval a, dup, if truthy jump past b, pop, eval b
		if t == tokOr {
			bc.code = append(bc.code, instr{op: opDup})
			jumpIdx := len(bc.code)
			bc.code = append(bc.code, instr{op: opJumpIfTrue})
			bc.code = append(bc.code, instr{op: opPop})
			c.compileBinary(bc, prec+1)
			bc.code[jumpIdx].iarg = len(bc.code)
			continue
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
		case tokBitAnd:
			bc.code = append(bc.code, instr{op: opBitAnd})
		case tokBitOr:
			bc.code = append(bc.code, instr{op: opBitOr})
		case tokBitXor:
			bc.code = append(bc.code, instr{op: opBitXor})
		case tokLShift:
			bc.code = append(bc.code, instr{op: opShl})
		case tokRShift:
			bc.code = append(bc.code, instr{op: opShr})
		case tokURShift:
			bc.code = append(bc.code, instr{op: opUShr})
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
	if c.peek().t == tokBitNot {
		c.advance()
		c.compileUnary(bc)
		bc.code = append(bc.code, instr{op: opBitNot})
		return
	}
	if c.peek().t == tokIdent && c.peek().v == "typeof" {
		c.advance()
		c.compileUnary(bc)
		bc.code = append(bc.code, instr{op: opTypeof})
		return
	}
	if c.peek().t == tokIdent && c.peek().v == "await" {
		c.advance()
		c.compileUnary(bc)
		bc.code = append(bc.code, instr{op: opAwait})
		return
	}
	c.compilePrimary(bc)
}

func (c *bcCompiler) compilePrimary(bc *bytecode) {
	t := c.advance()

	switch t.t {
	case tokNum:
		bc.code = append(bc.code, instr{op: opLoadNum, narg: t.n})
		c.compilePostfix(bc)
	case tokStr:
		bc.code = append(bc.code, instr{op: opLoadStr, sarg: t.v})
		c.compilePostfix(bc)
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
		case "this":
			bc.code = append(bc.code, instr{op: opLoadThis})
			c.compilePostfix(bc)
		case "new":
			// new Constructor(args...)
			if c.peek().t != tokIdent {
				c.fail()
				return
			}
			ctorName := c.advance().v
			// Handle dotted constructors: new Intl.NumberFormat(...)
			for c.peek().t == tokDot {
				c.advance()
				if c.peek().t != tokIdent {
					c.fail()
					return
				}
				ctorName += "." + c.advance().v
			}
			if c.peek().t != tokLParen {
				c.fail()
				return
			}
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
			bc.code = append(bc.code, instr{op: opNewCall, sarg: ctorName, iarg: argc})
			c.compilePostfix(bc)
		default:
			name := t.v
			// Single-param arrow: x => expr or x => { ... }
			if c.peek().t == tokArrow {
				c.advance() // skip =>
				c.compileArrowBody(bc, []string{name})
				return
			}
			// Variable, property access, method call, or function call
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
			// Postfix chain: .prop, .method(args), [expr] — in any order
			c.compilePostfix(bc)
		}
	case tokLParen:
		// Check if this is an arrow function: (params) => ...
		if c.isArrowFunction() {
			params := c.parseArrowParams()
			c.expect(tokArrow)
			c.compileArrowBody(bc, params)
			return
		}
		c.compileExpr(bc)
		c.expect(tokRParen)
		c.compilePostfix(bc)
	case tokLBrack:
		// Array literal: [expr, expr, ...spread]
		// Check for spread elements
		hasSpread := false
		for ii := c.pos; ii < len(c.tokens); ii++ {
			if c.tokens[ii].t == tokRBrack { break }
			if c.tokens[ii].t == tokSpread { hasSpread = true; break }
		}

		if hasSpread {
			// Build via empty array + concat/push pattern
			bc.code = append(bc.code, instr{op: opNewArray, iarg: 0})
			for c.peek().t != tokRBrack && c.peek().t != tokEOF && !c.failed {
				if c.peek().t == tokSpread {
					c.advance()
					c.compileExpr(bc)
					bc.code = append(bc.code, instr{op: opSpreadArr})
				} else {
					// Single element — wrap in 1-element array then spread
					c.compileExpr(bc)
					bc.code = append(bc.code, instr{op: opNewArray, iarg: 1})
					bc.code = append(bc.code, instr{op: opSpreadArr})
				}
				if c.peek().t == tokComma { c.advance() }
			}
		} else {
			argc := 0
			for c.peek().t != tokRBrack && c.peek().t != tokEOF && !c.failed {
				c.compileExpr(bc)
				argc++
				if c.peek().t == tokComma { c.advance() }
			}
			_ = argc // opNewArray emitted below
			bc.code = append(bc.code, instr{op: opNewArray, iarg: argc})
		}
		c.expect(tokRBrack)
		c.compilePostfix(bc)
	case tokLBrace:
		// Object literal: {key: expr, key: expr, ...}
		bc.code = append(bc.code, instr{op: opNewObject})
		for c.peek().t != tokRBrace && c.peek().t != tokEOF && !c.failed {
			// key can be ident or string
			var key string
			if c.peek().t == tokIdent {
				key = c.advance().v
				// Shorthand property: {x} is {x: x}
				if c.peek().t == tokComma || c.peek().t == tokRBrace {
					bc.code = append(bc.code, instr{op: opLoadVar, sarg: key})
					bc.code = append(bc.code, instr{op: opSetObjProp, sarg: key})
					if c.peek().t == tokComma {
						c.advance()
					}
					continue
				}
			} else if c.peek().t == tokStr {
				key = c.advance().v
			} else if c.peek().t == tokSpread {
				// Spread: {...expr} — copy all properties from expr into this object
				c.advance() // consume ...
				c.compileExpr(bc)
				bc.code = append(bc.code, instr{op: opSpreadObj})
				if c.peek().t == tokComma {
					c.advance()
				}
				continue
			} else if c.peek().t == tokLBrack {
				// Computed property: {[expr]: value}
				c.advance() // skip [
				c.compileExpr(bc)
				c.expect(tokRBrack)
				c.expect(tokColon)
				c.compileExpr(bc)
				// For computed props, we'd need a new opcode. Bail for now.
				c.fail()
				return
			} else {
				c.fail()
				return
			}
			c.expect(tokColon)
			c.compileExpr(bc)
			bc.code = append(bc.code, instr{op: opSetObjProp, sarg: key})
			if c.peek().t == tokComma {
				c.advance()
			}
		}
		c.expect(tokRBrace)
		c.compilePostfix(bc)
	case tokTemplatePart:
		bc.code = append(bc.code, instr{op: opTemplate, sarg: t.v})
		c.compilePostfix(bc)
	case tokRegExp:
		// t.v contains "pattern\x00flags"
		bc.code = append(bc.code, instr{op: opLoadRegExp, sarg: t.v})
		c.compilePostfix(bc)
	default:
		c.fail()
	}
}

// compilePostfix handles postfix chains: .prop, .method(args), [expr], ?.prop
func (c *bcCompiler) compilePostfix(bc *bytecode) {
	for !c.failed {
		if c.peek().t == tokOptChain {
			// Optional chaining: obj?.prop — treat like .prop with null check
			// For bytecode simplicity, treat ?. same as . (the null propagation
			// works because getProp on null/undefined returns undefined)
			c.advance() // skip ?.
			if c.peek().t != tokIdent {
				c.fail()
				return
			}
			prop := c.advance().v
			if c.peek().t == tokLParen {
				c.advance()
				argc := 0
				for c.peek().t != tokRParen && c.peek().t != tokEOF && !c.failed {
					c.compileExpr(bc)
					argc++
					if c.peek().t == tokComma { c.advance() }
				}
				c.expect(tokRParen)
				bc.code = append(bc.code, instr{op: opCallMethod, sarg: prop, iarg: argc})
			} else {
				bc.code = append(bc.code, instr{op: opGetProp, sarg: prop})
			}
			continue
		}
		if c.peek().t == tokDot {
			c.advance() // skip .
			if c.peek().t != tokIdent {
				c.fail()
				return
			}
			prop := c.advance().v
			// Check if this is a method call: obj.method(args)
			if c.peek().t == tokLParen {
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
				bc.code = append(bc.code, instr{op: opCallMethod, sarg: prop, iarg: argc})
			} else {
				bc.code = append(bc.code, instr{op: opGetProp, sarg: prop})
			}
		} else if c.peek().t == tokLBrack {
			c.advance() // skip [
			c.compileExpr(bc)
			c.expect(tokRBrack)
			bc.code = append(bc.code, instr{op: opGetIndex})
		} else {
			break
		}
	}
}

// isArrowFunction peeks ahead to check if current position is (params) => ...
// The opening ( has already been consumed by compilePrimary's advance().
func (c *bcCompiler) isArrowFunction() bool {
	depth := 1
	i := c.pos
	for i < len(c.tokens) {
		tt := c.tokens[i].t
		if tt == tokLParen || tt == tokLBrack || tt == tokLBrace {
			depth++
		} else if tt == tokRParen || tt == tokRBrack || tt == tokRBrace {
			depth--
			if depth == 0 {
				if tt != tokRParen {
					return false
				}
				// Check if => follows
				if i+1 < len(c.tokens) && c.tokens[i+1].t == tokArrow {
					return true
				}
				return false
			}
		}
		i++
	}
	return false
}

// parseArrowParams parses params inside already-opened parens: ident, ident, ... )
func (c *bcCompiler) parseArrowParams() []string {
	var params []string
	for c.peek().t != tokRParen && c.peek().t != tokEOF {
		if c.peek().t == tokIdent {
			params = append(params, c.advance().v)
			// Skip default value: = expr
			if c.peek().t == tokAssign {
				c.advance()
				// Skip the default expression
				depth := 0
				for c.peek().t != tokEOF {
					tt := c.peek().t
					if tt == tokLParen || tt == tokLBrack || tt == tokLBrace { depth++ }
					if tt == tokRParen || tt == tokRBrack || tt == tokRBrace {
						if depth == 0 { break }
						depth--
					}
					if depth == 0 && tt == tokComma { break }
					c.advance()
				}
			}
		} else if c.peek().t == tokLBrace || c.peek().t == tokLBrack {
			// Destructured params — skip and use a placeholder
			depth := 1
			c.advance()
			for c.peek().t != tokEOF && depth > 0 {
				if c.peek().t == tokLBrace || c.peek().t == tokLBrack { depth++ }
				if c.peek().t == tokRBrace || c.peek().t == tokRBrack { depth-- }
				if depth > 0 { c.advance() }
			}
			if c.peek().t == tokRBrace || c.peek().t == tokRBrack { c.advance() }
			params = append(params, "__destructured__")
		} else if c.peek().t == tokSpread {
			// ...rest param
			c.advance()
			if c.peek().t == tokIdent {
				params = append(params, c.advance().v)
			}
		} else {
			// Unknown token — advance to avoid infinite loop
			c.advance()
		}
		if c.peek().t == tokComma {
			c.advance()
		}
	}
	c.expect(tokRParen)
	return params
}

// compileArrowBody compiles an arrow function body and emits opMakeArrow.
func (c *bcCompiler) compileArrowBody(bc *bytecode, params []string) {
	isBlock := c.peek().t == tokLBrace
	// Capture the tokens for the arrow body
	start := c.pos
	if isBlock {
		// Block body: { ... }
		depth := 0
		for c.peek().t != tokEOF {
			if c.peek().t == tokLBrace {
				depth++
			} else if c.peek().t == tokRBrace {
				depth--
				if depth == 0 {
					c.advance() // consume closing }
					break
				}
			}
			c.advance()
		}
	} else {
		// Expression body: skip one expression
		c.skipBcExpr()
	}
	end := c.pos

	// Copy the body tokens
	bodyTokens := make([]tok, end-start)
	copy(bodyTokens, c.tokens[start:end])

	// Register in the bytecode's arrows slice
	idx := len(bc.arrows)
	bc.arrows = append(bc.arrows, bcArrowDef{
		params:  params,
		tokens:  bodyTokens,
		isBlock: isBlock,
	})
	bc.code = append(bc.code, instr{op: opMakeArrow, iarg: idx})
}

// skipBcExpr skips a single expression in the bytecode compiler (for arrow body capture).
func (c *bcCompiler) skipBcExpr() {
	depth := 0
	for c.peek().t != tokEOF {
		t := c.peek().t
		if t == tokLParen || t == tokLBrack || t == tokLBrace {
			depth++
		} else if t == tokRParen || t == tokRBrack || t == tokRBrace {
			if depth == 0 {
				return
			}
			depth--
		} else if depth == 0 && (t == tokComma || t == tokSemi) {
			return
		}
		c.advance()
	}
}

// ─── Bytecode Execution ─────────────────────────────────

// execBytecodeSafe wraps execBytecode with panic recovery.
func execBytecodeSafe(bc *bytecode, scope map[string]*Value) (result *Value) {
	defer func() {
		if r := recover(); r != nil {
			result = Undefined
		}
	}()
	return execBytecode(bc, scope)
}

// bcCallFrame saves state when entering a bytecoded function call.
type bcCallFrame struct {
	code    []instr
	codeLen int
	ip      int
	sp      int // stack pointer to restore result to
	bc      *bytecode
	saved   []*Value // saved param values for restore
	params  []string // param names to restore
}

func execBytecode(bc *bytecode, scope map[string]*Value) *Value {
	// Fixed-size stack — handles deep expressions in real-world JS
	var stack [256]*Value
	sp := 0
	code := bc.code
	codeLen := len(code)
	ip := 0

	// Call frame stack for iterative function calls (avoids Go stack recursion)
	var frames []bcCallFrame

execLoop:
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
			retVal := Undefined
			if sp > 0 {
				retVal = stack[sp-1]
			}
			// Pop call frame if we're in a nested call
			if len(frames) > 0 {
				f := frames[len(frames)-1]
				frames = frames[:len(frames)-1]
				// Restore params
				for i, p := range f.params {
					if f.saved[i] != nil {
						scope[p] = f.saved[i]
					} else {
						delete(scope, p)
					}
				}
				// Restore execution state
				bc = f.bc
				code = f.code
				codeLen = f.codeLen
				ip = f.ip
				sp = f.sp
				// Push return value
				stack[sp] = retVal
				sp++
				continue // skip ip++ at bottom
			}
			return retVal

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
				// Iterative bytecode call — push frame instead of recursing
				params := fn.bc.params
				saved := make([]*Value, len(params))
				for i, p := range params {
					saved[i] = scope[p]
					if i < argc {
						scope[p] = args[i]
					}
				}
				// Save current frame
				frames = append(frames, bcCallFrame{
					code:    code,
					codeLen: codeLen,
					ip:      ip + 1, // resume AFTER the opCall
					sp:      sp,     // where to put the result
					bc:      bc,
					saved:   saved,
					params:  params,
				})
				// Switch to callee — callee uses stack region above caller's sp
				// so it doesn't clobber caller's intermediate values
				bc = fn.bc
				code = fn.bc.code
				codeLen = len(code)
				ip = 0
				// sp stays at current position — callee builds on top
				continue // skip ip++ at bottom
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
			} else if fn.str == "__arrow" {
				arrowRegistryMu.Lock()
				af, afOk := arrowRegistry[int(fn.num)]
				arrowRegistryMu.Unlock()
				if afOk {
					// Try to get/compile bytecode for the arrow
					if !af.bcTried {
						// Trigger lazy compile via callArrow (which sets af.bc)
						argsCopy := make([]*Value, argc)
						copy(argsCopy, args)
						stack[sp] = callArrow(int(fn.num), argsCopy, scope)
						sp++
					} else if af.bc != nil {
						// Trampoline: use frame push like regular bc calls
						params := af.bc.params
						saved := make([]*Value, len(params))
						for i, p := range params {
							saved[i] = scope[p]
							if i < argc {
								scope[p] = args[i]
							}
						}
						// Merge arrow's captured scope
						if af.scope != nil {
							for k, v := range af.scope {
								if _, exists := scope[k]; !exists {
									scope[k] = v
								}
							}
						}
						frames = append(frames, bcCallFrame{
							code: code, codeLen: codeLen,
							ip: ip + 1, sp: sp,
							bc: bc, saved: saved, params: params,
						})
						bc = af.bc
						code = af.bc.code
						codeLen = len(code)
						ip = 0
						continue
					} else {
						// bc compilation failed — use interpreter
						argsCopy := make([]*Value, argc)
						copy(argsCopy, args)
						stack[sp] = callArrow(int(fn.num), argsCopy, scope)
						sp++
					}
				} else {
					stack[sp] = Undefined
					sp++
				}
			} else {
				stack[sp] = Undefined
				sp++
			}

		case opPop:
			if sp > 0 {
				sp--
			}

		case opDup:
			stack[sp] = stack[sp-1]
			sp++

		case opJumpIfTrue:
			sp--
			if stack[sp].truthy() {
				ip = inst.iarg
				continue
			}

		case opGetProp:
			obj := stack[sp-1]
			stack[sp-1] = obj.getProp(inst.sarg)

		case opSetProp:
			sp -= 2
			val := stack[sp+1]
			obj := stack[sp]
			if obj.typ == TypeObject && obj.object != nil {
				// Check setter
				if obj.getset != nil {
					if desc, ok := obj.getset[inst.sarg]; ok && desc.Set != nil && desc.Set.native != nil {
						desc.Set.native([]*Value{val})
						stack[sp] = val
						sp++
						ip++
						continue
					}
				}
				obj.object[inst.sarg] = val
			}
			stack[sp] = val
			sp++

		case opGetIndex:
			sp--
			idx := stack[sp]
			obj := stack[sp-1]
			stack[sp-1] = obj.getProp(idx.toStr())

		case opCallMethod:
			argc := inst.iarg
			method := inst.sarg
			sp -= argc
			args := make([]*Value, argc)
			copy(args, stack[sp:sp+argc])
			sp--
			obj := stack[sp]
			result := callMethodBC(obj, method, args)
			stack[sp] = result
			sp++

		case opNewArray:
			n := inst.iarg
			arr := make([]*Value, n)
			sp -= n
			copy(arr, stack[sp:sp+n])
			stack[sp] = &Value{typ: TypeArray, array: arr}
			sp++

		case opNewObject:
			stack[sp] = &Value{typ: TypeObject, object: make(map[string]*Value)}
			sp++

		case opSetObjProp:
			sp--
			val := stack[sp]
			obj := stack[sp-1] // leave obj on stack
			if obj.typ == TypeObject && obj.object != nil {
				obj.object[inst.sarg] = val
			}

		case opTypeof:
			v := stack[sp-1]
			switch v.typ {
			case TypeUndefined:
				stack[sp-1] = internStr("undefined")
			case TypeNull:
				stack[sp-1] = internStr("object")
			case TypeBool:
				stack[sp-1] = internStr("boolean")
			case TypeNumber:
				stack[sp-1] = internStr("number")
			case TypeString:
				stack[sp-1] = internStr("string")
			case TypeFunc:
				stack[sp-1] = internStr("function")
			default:
				stack[sp-1] = internStr("object")
			}

		case opInstanceof:
			sp--
			// Simplified: check if right-side is a constructor with a prototype that matches
			// For now, always false — enough to not crash
			stack[sp-1] = False

		case opBitAnd:
			sp--
			stack[sp-1] = internNum(float64(int64(stack[sp-1].toNum()) & int64(stack[sp].toNum())))

		case opBitOr:
			sp--
			stack[sp-1] = internNum(float64(int64(stack[sp-1].toNum()) | int64(stack[sp].toNum())))

		case opBitXor:
			sp--
			stack[sp-1] = internNum(float64(int64(stack[sp-1].toNum()) ^ int64(stack[sp].toNum())))

		case opBitNot:
			stack[sp-1] = internNum(float64(^int64(stack[sp-1].toNum())))

		case opShl:
			sp--
			stack[sp-1] = internNum(float64(int64(stack[sp-1].toNum()) << uint64(int64(stack[sp].toNum())&63)))

		case opShr:
			sp--
			stack[sp-1] = internNum(float64(int64(stack[sp-1].toNum()) >> uint64(int64(stack[sp].toNum())&63)))

		case opUShr:
			sp--
			stack[sp-1] = internNum(float64(uint32(stack[sp-1].toNum()) >> (uint32(stack[sp].toNum()) & 31)))

		case opIn:
			sp--
			key := stack[sp-1].toStr()
			obj := stack[sp]
			found := false
			if obj.typ == TypeObject {
				// Walk own props + prototype chain + getter descriptors
				for cur := obj; cur != nil && !found; cur = cur.proto {
					if cur.object != nil {
						if _, ok := cur.object[key]; ok {
							found = true
							break
						}
					}
					if cur.getset != nil {
						if _, ok := cur.getset[key]; ok {
							found = true
							break
						}
					}
				}
			} else if obj.typ == TypeFunc && obj.object != nil {
				_, found = obj.object[key]
			} else if obj.typ == TypeArray {
				// "index in array" check
				idx := int(stack[sp-1].toNum())
				found = idx >= 0 && idx < len(obj.array)
			}
			stack[sp-1] = newBool(found)

		case opThrow:
			sp--
			panic(stack[sp])

		case opNewCall:
			argc := inst.iarg
			sp -= argc
			args := make([]*Value, argc)
			copy(args, stack[sp:sp+argc])
			name := inst.sarg
			result := bcNewCall(name, args, scope)
			stack[sp] = result
			sp++

		case opLoadThis:
			if v, ok := scope["this"]; ok {
				stack[sp] = v
			} else {
				stack[sp] = Undefined
			}
			sp++

		case opTemplate:
			raw := inst.sarg
			stack[sp] = evalTemplate(raw, scope)
			sp++

		case opTryCatch:
			def := &bc.tryCatches[inst.iarg]
			result := execTryCatch(def, scope)
			if result != nil {
				return result
			}

		case opSpreadObj:
			sp--
			src := stack[sp]
			target := stack[sp-1] // leave target on stack
			if src.typ == TypeObject && src.object != nil && target.typ == TypeObject && target.object != nil {
				for k, v := range src.object {
					target.object[k] = v
				}
			}

		case opSpreadArr:
			sp--
			src := stack[sp]
			target := stack[sp-1]
			if target.typ == TypeArray && src.typ == TypeArray {
				target.array = append(target.array, src.array...)
			} else if target.typ == TypeArray {
				target.array = append(target.array, src)
			}

		case opObjectKeys:
			sp--
			obj := stack[sp]
			var keys []*Value
			if obj.typ == TypeObject && obj.object != nil {
				for k := range obj.object {
					if !strings.HasPrefix(k, "__") {
						keys = append(keys, newStr(k))
					}
				}
			}
			stack[sp] = newArr(keys)
			sp++

		case opIterToArray:
			// Convert Map/Set to iterable array for for...of
			obj := stack[sp-1]
			if obj.typ == TypeObject && obj.object != nil {
				if jm, ok := obj.Custom.(*jsMap); ok {
					entries := make([]*Value, len(jm.keys))
					for ii, key := range jm.keys {
						entries[ii] = newArr([]*Value{newStr(key), jm.values[key]})
					}
					stack[sp-1] = newArr(entries)
				} else if js, ok := obj.Custom.(*jsSet); ok {
					items := make([]*Value, len(js.items))
					for ii, key := range js.items {
						items[ii] = js.values[key]
					}
					stack[sp-1] = newArr(items)
				}
			}

		case opLoadRegExp:
			parts := strings.SplitN(inst.sarg, "\x00", 2)
			pattern := parts[0]
			flags := ""
			if len(parts) > 1 {
				flags = parts[1]
			}
			stack[sp] = newRegexpValue(pattern, flags)
			sp++

		case opAwait:
			val := stack[sp-1]
			if p := getPromise(val); p != nil {
				p.mu.Lock()
				if p.state == PromiseFulfilled {
					stack[sp-1] = p.value
				} else if p.state == PromiseRejected {
					p.mu.Unlock()
					panic(p.value) // throw the rejection
				} else {
					stack[sp-1] = Undefined
				}
				p.mu.Unlock()
			}
			// If not a promise, value passes through unchanged

		case opMakeArrow:
			def := &bc.arrows[inst.iarg]
			// Register as an arrow function using the existing arrow registry
			scopeCopy := make(map[string]*Value, len(scope))
			for k, v := range scope {
				scopeCopy[k] = v
			}
			id := registerArrow(&arrowFunc{
				params:  def.params,
				tokens:  def.tokens,
				isBlock: def.isBlock,
				scope:   scopeCopy,
			})
			stack[sp] = &Value{typ: TypeFunc, str: "__arrow", num: float64(id)}
			sp++
		}

		ip++
	}

	// End of code — pop frame if nested, otherwise return Undefined
	if len(frames) > 0 {
		f := frames[len(frames)-1]
		frames = frames[:len(frames)-1]
		for i, p := range f.params {
			if f.saved[i] != nil {
				scope[p] = f.saved[i]
			} else {
				delete(scope, p)
			}
		}
		bc = f.bc
		code = f.code
		codeLen = f.codeLen
		ip = f.ip
		sp = f.sp
		stack[sp] = Undefined
		sp++
		goto execLoop
	}
	return Undefined
}

// callMethodBC calls a method on a value with pre-evaluated args (bytecode path).
func callMethodBC(obj *Value, method string, args []*Value) *Value {
	// Check for custom object methods first (native functions on the object or prototype)
	if obj.typ == TypeObject && obj.object != nil {
		if fn, ok := obj.object[method]; ok && fn.typ == TypeFunc {
			if fn.native != nil {
				return fn.native(args)
			}
			if fn.str == "__arrow" {
				return callArrow(int(fn.num), args, nil)
			}
			if fn.fnBody != "" {
				props := make(map[string]*Value, len(args))
				if len(fn.fnParams) > 0 {
					params := splitParams(fn.fnParams[0])
					for i, p := range params {
						if i < len(args) {
							props[p] = args[i]
						}
					}
				}
				ev := &evaluator{scope: map[string]*Value{"this": obj}}
				return ev.callFunc(fn, props)
			}
		}
	}
	// Check prototype chain — handle native, __arrow, and bytecode methods
	// with `this` bound to the original object
	if obj.typ == TypeObject && obj.proto != nil {
		for cur := obj.proto; cur != nil; cur = cur.proto {
			if cur.object != nil {
				if fn, ok := cur.object[method]; ok && fn.typ == TypeFunc {
					if fn.native != nil {
						return fn.native(args)
					}
					if fn.str == "__arrow" {
						scope := map[string]*Value{"this": obj}
						return callArrow(int(fn.num), args, scope)
					}
					if fn.fnBody != "" {
						props := make(map[string]*Value, len(args))
						if len(fn.fnParams) > 0 {
							params := splitParams(fn.fnParams[0])
							for i, p := range params {
								if i < len(args) {
									props[p] = args[i]
								}
							}
						}
						ev := &evaluator{scope: map[string]*Value{"this": obj}}
						return ev.callFunc(fn, props)
					}
				}
			}
		}
	}
	// Static methods on class constructors (TypeFunc with object map)
	if obj.typ == TypeFunc && obj.object != nil {
		if fn, ok := obj.object[method]; ok && fn.typ == TypeFunc {
			if fn.native != nil {
				return fn.native(args)
			}
			if fn.str == "__arrow" {
				return callArrow(int(fn.num), args, nil)
			}
		}
	}
	// Built-in methods via getProp — for arrays, strings, etc.
	fn := obj.getProp(method)
	if fn != nil && fn.typ == TypeFunc {
		if fn.native != nil {
			return fn.native(args)
		}
		if fn.str == "__arrow" {
			scope := map[string]*Value{"this": obj}
			return callArrow(int(fn.num), args, scope)
		}
	}
	// Fall back to built-in method implementations
	return callBuiltinMethod(obj, method, args)
}

// callBuiltinMethod handles built-in methods for arrays, strings, etc.
func callBuiltinMethod(obj *Value, method string, args []*Value) *Value {
	switch obj.typ {
	case TypeArray:
		return callArrayMethod(obj, method, args)
	case TypeString:
		return callStringMethod(obj, method, args)
	case TypeNumber:
		return callNumberMethod(obj, method, args)
	}
	return Undefined
}

// callNumberMethod handles built-in number methods.
func callNumberMethod(n *Value, method string, args []*Value) *Value {
	switch method {
	case "toFixed":
		prec := 0
		if len(args) > 0 {
			prec = int(args[0].toNum())
		}
		return newStr(strconv.FormatFloat(n.num, 'f', prec, 64))
	case "toString":
		if len(args) > 0 {
			base := int(args[0].toNum())
			if base >= 2 && base <= 36 {
				return newStr(strconv.FormatInt(int64(n.num), base))
			}
		}
		return newStr(n.toStr())
	case "toLocaleString":
		return newStr(formatWithCommas(int64(n.num)))
	case "toPrecision":
		prec := 6
		if len(args) > 0 {
			prec = int(args[0].toNum())
		}
		return newStr(strconv.FormatFloat(n.num, 'g', prec, 64))
	}
	return Undefined
}

// callArrayMethod handles built-in array methods with pre-evaluated args.
func callArrayMethod(arr *Value, method string, args []*Value) *Value {
	switch method {
	case "push":
		arr.array = append(arr.array, args...)
		return newNum(float64(len(arr.array)))
	case "pop":
		if len(arr.array) == 0 {
			return Undefined
		}
		v := arr.array[len(arr.array)-1]
		arr.array = arr.array[:len(arr.array)-1]
		return v
	case "shift":
		if len(arr.array) == 0 {
			return Undefined
		}
		v := arr.array[0]
		arr.array = arr.array[1:]
		return v
	case "unshift":
		arr.array = append(args, arr.array...)
		return newNum(float64(len(arr.array)))
	case "join":
		sep := ","
		if len(args) > 0 {
			sep = args[0].toStr()
		}
		parts := make([]string, len(arr.array))
		for i, v := range arr.array {
			parts[i] = v.toStr()
		}
		return newStr(strings.Join(parts, sep))
	case "slice":
		start := 0
		end := len(arr.array)
		if len(args) > 0 {
			start = int(args[0].toNum())
			if start < 0 {
				start = len(arr.array) + start
			}
		}
		if len(args) > 1 {
			end = int(args[1].toNum())
			if end < 0 {
				end = len(arr.array) + end
			}
		}
		if start < 0 {
			start = 0
		}
		if end > len(arr.array) {
			end = len(arr.array)
		}
		if start >= end {
			return &Value{typ: TypeArray, array: []*Value{}}
		}
		result := make([]*Value, end-start)
		copy(result, arr.array[start:end])
		return &Value{typ: TypeArray, array: result}
	case "concat":
		result := make([]*Value, len(arr.array))
		copy(result, arr.array)
		for _, a := range args {
			if a.typ == TypeArray {
				result = append(result, a.array...)
			} else {
				result = append(result, a)
			}
		}
		return &Value{typ: TypeArray, array: result}
	case "indexOf":
		if len(args) == 0 {
			return newNum(-1)
		}
		target := args[0]
		for i, v := range arr.array {
			if strictEqual(v, target) {
				return newNum(float64(i))
			}
		}
		return newNum(-1)
	case "includes":
		if len(args) == 0 {
			return False
		}
		target := args[0]
		for _, v := range arr.array {
			if strictEqual(v, target) {
				return True
			}
		}
		return False
	case "reverse":
		for i, j := 0, len(arr.array)-1; i < j; i, j = i+1, j-1 {
			arr.array[i], arr.array[j] = arr.array[j], arr.array[i]
		}
		return arr
	case "map":
		if len(args) == 0 {
			return arr
		}
		fn := args[0]
		result := make([]*Value, len(arr.array))
		for i, v := range arr.array {
			result[i] = callFuncValue(fn, []*Value{v, newNum(float64(i)), arr}, nil)
		}
		return &Value{typ: TypeArray, array: result}
	case "filter":
		if len(args) == 0 {
			return arr
		}
		fn := args[0]
		var result []*Value
		for i, v := range arr.array {
			r := callFuncValue(fn, []*Value{v, newNum(float64(i)), arr}, nil)
			if r.truthy() {
				result = append(result, v)
			}
		}
		if result == nil {
			result = []*Value{}
		}
		return &Value{typ: TypeArray, array: result}
	case "find":
		if len(args) == 0 {
			return Undefined
		}
		fn := args[0]
		for i, v := range arr.array {
			r := callFuncValue(fn, []*Value{v, newNum(float64(i)), arr}, nil)
			if r.truthy() {
				return v
			}
		}
		return Undefined
	case "findIndex":
		if len(args) == 0 {
			return newNum(-1)
		}
		fn := args[0]
		for i, v := range arr.array {
			r := callFuncValue(fn, []*Value{v, newNum(float64(i)), arr}, nil)
			if r.truthy() {
				return newNum(float64(i))
			}
		}
		return newNum(-1)
	case "some":
		if len(args) == 0 {
			return False
		}
		fn := args[0]
		for i, v := range arr.array {
			r := callFuncValue(fn, []*Value{v, newNum(float64(i)), arr}, nil)
			if r.truthy() {
				return True
			}
		}
		return False
	case "every":
		if len(args) == 0 {
			return True
		}
		fn := args[0]
		for i, v := range arr.array {
			r := callFuncValue(fn, []*Value{v, newNum(float64(i)), arr}, nil)
			if !r.truthy() {
				return False
			}
		}
		return True
	case "forEach":
		if len(args) == 0 {
			return Undefined
		}
		fn := args[0]
		for i, v := range arr.array {
			callFuncValue(fn, []*Value{v, newNum(float64(i)), arr}, nil)
		}
		return Undefined
	case "reduce":
		if len(args) == 0 {
			return Undefined
		}
		fn := args[0]
		acc := Undefined
		startIdx := 0
		if len(args) > 1 {
			acc = args[1]
		} else if len(arr.array) > 0 {
			acc = arr.array[0]
			startIdx = 1
		}
		for i := startIdx; i < len(arr.array); i++ {
			acc = callFuncValue(fn, []*Value{acc, arr.array[i], newNum(float64(i)), arr}, nil)
		}
		return acc
	case "flat":
		depth := 1
		if len(args) > 0 {
			depth = int(args[0].toNum())
		}
		return &Value{typ: TypeArray, array: flattenArray(arr.array, depth)}
	case "flatMap":
		if len(args) == 0 {
			return arr
		}
		fn := args[0]
		var result []*Value
		for i, v := range arr.array {
			r := callFuncValue(fn, []*Value{v, newNum(float64(i)), arr}, nil)
			if r.typ == TypeArray {
				result = append(result, r.array...)
			} else {
				result = append(result, r)
			}
		}
		return &Value{typ: TypeArray, array: result}
	case "sort":
		if len(args) == 0 {
			sortValues(arr.array)
		} else {
			fn := args[0]
			sort.SliceStable(arr.array, func(i, j int) bool {
				r := callFuncValue(fn, []*Value{arr.array[i], arr.array[j]}, nil)
				return r.toNum() < 0
			})
		}
		return arr
	case "splice":
		start := 0
		if len(args) > 0 {
			start = int(args[0].toNum())
			if start < 0 { start = len(arr.array) + start }
			if start < 0 { start = 0 }
			if start > len(arr.array) { start = len(arr.array) }
		}
		deleteCount := len(arr.array) - start
		if len(args) > 1 {
			deleteCount = int(args[1].toNum())
			if deleteCount < 0 { deleteCount = 0 }
		}
		end := start + deleteCount
		if end > len(arr.array) { end = len(arr.array) }
		removed := make([]*Value, end-start)
		copy(removed, arr.array[start:end])
		var insert []*Value
		if len(args) > 2 { insert = args[2:] }
		newArr := make([]*Value, 0, len(arr.array)-deleteCount+len(insert))
		newArr = append(newArr, arr.array[:start]...)
		newArr = append(newArr, insert...)
		newArr = append(newArr, arr.array[end:]...)
		arr.array = newArr
		return &Value{typ: TypeArray, array: removed}
	case "fill":
		fillVal := Undefined
		if len(args) > 0 { fillVal = args[0] }
		start := 0
		end := len(arr.array)
		if len(args) > 1 { start = int(args[1].toNum()) }
		if len(args) > 2 { end = int(args[2].toNum()) }
		for i := start; i < end && i < len(arr.array); i++ {
			arr.array[i] = fillVal
		}
		return arr
	}
	return Undefined
}

// callStringMethod handles built-in string methods with pre-evaluated args.
func callStringMethod(s *Value, method string, args []*Value) *Value {
	str := s.str
	switch method {
	case "charAt":
		idx := 0
		if len(args) > 0 {
			idx = int(args[0].toNum())
		}
		if idx >= 0 && idx < len(str) {
			return newStr(string(str[idx]))
		}
		return internStr("")
	case "indexOf":
		if len(args) == 0 {
			return newNum(-1)
		}
		return newNum(float64(strings.Index(str, args[0].toStr())))
	case "lastIndexOf":
		if len(args) == 0 {
			return newNum(-1)
		}
		return newNum(float64(strings.LastIndex(str, args[0].toStr())))
	case "includes":
		if len(args) == 0 {
			return False
		}
		return newBool(strings.Contains(str, args[0].toStr()))
	case "startsWith":
		if len(args) == 0 {
			return False
		}
		return newBool(strings.HasPrefix(str, args[0].toStr()))
	case "endsWith":
		if len(args) == 0 {
			return False
		}
		return newBool(strings.HasSuffix(str, args[0].toStr()))
	case "slice":
		start := 0
		end := len(str)
		if len(args) > 0 {
			start = int(args[0].toNum())
			if start < 0 {
				start = len(str) + start
			}
		}
		if len(args) > 1 {
			end = int(args[1].toNum())
			if end < 0 {
				end = len(str) + end
			}
		}
		if start < 0 {
			start = 0
		}
		if end > len(str) {
			end = len(str)
		}
		if start >= end {
			return internStr("")
		}
		return newStr(str[start:end])
	case "substring":
		start := 0
		end := len(str)
		if len(args) > 0 {
			start = int(args[0].toNum())
		}
		if len(args) > 1 {
			end = int(args[1].toNum())
		}
		if start < 0 {
			start = 0
		}
		if end > len(str) {
			end = len(str)
		}
		if start > end {
			start, end = end, start
		}
		return newStr(str[start:end])
	case "toUpperCase":
		return newStr(strings.ToUpper(str))
	case "toLowerCase":
		return newStr(strings.ToLower(str))
	case "trim":
		return newStr(strings.TrimSpace(str))
	case "trimStart":
		return newStr(strings.TrimLeft(str, " \t\n\r"))
	case "trimEnd":
		return newStr(strings.TrimRight(str, " \t\n\r"))
	case "split":
		sep := ""
		if len(args) > 0 {
			sep = args[0].toStr()
		}
		parts := strings.Split(str, sep)
		result := make([]*Value, len(parts))
		for i, p := range parts {
			result[i] = newStr(p)
		}
		return &Value{typ: TypeArray, array: result}
	case "replace":
		if len(args) < 2 {
			return s
		}
		return newStr(strings.Replace(str, args[0].toStr(), args[1].toStr(), 1))
	case "replaceAll":
		if len(args) < 2 {
			return s
		}
		return newStr(strings.ReplaceAll(str, args[0].toStr(), args[1].toStr()))
	case "repeat":
		if len(args) == 0 {
			return internStr("")
		}
		n := int(args[0].toNum())
		if n <= 0 {
			return internStr("")
		}
		return newStr(strings.Repeat(str, n))
	case "padStart":
		if len(args) == 0 {
			return s
		}
		targetLen := int(args[0].toNum())
		pad := " "
		if len(args) > 1 {
			pad = args[1].toStr()
		}
		for len(str) < targetLen && pad != "" {
			str = pad + str
		}
		if len(str) > targetLen {
			str = str[len(str)-targetLen:]
		}
		return newStr(str)
	case "padEnd":
		if len(args) == 0 {
			return s
		}
		targetLen := int(args[0].toNum())
		pad := " "
		if len(args) > 1 {
			pad = args[1].toStr()
		}
		for len(str) < targetLen && pad != "" {
			str = str + pad
		}
		if len(str) > targetLen {
			str = str[:targetLen]
		}
		return newStr(str)
	case "concat":
		for _, a := range args {
			str += a.toStr()
		}
		return newStr(str)
	case "match":
		if len(args) == 0 {
			return Null
		}
		rd := getRegexpData(args[0])
		if rd == nil {
			return Null
		}
		m := rd.Re.FindString(str)
		if m == "" {
			return Null
		}
		return &Value{typ: TypeArray, array: []*Value{newStr(m)}}
	case "search":
		if len(args) == 0 {
			return newNum(-1)
		}
		rd := getRegexpData(args[0])
		if rd == nil {
			return newNum(-1)
		}
		loc := rd.Re.FindStringIndex(str)
		if loc == nil {
			return newNum(-1)
		}
		return newNum(float64(loc[0]))
	case "toString":
		return s
	}
	return Undefined
}

// bcNewCall handles `new Constructor(args)` in bytecode.
func bcNewCall(name string, args []*Value, scope map[string]*Value) *Value {
	switch name {
	case "Intl.NumberFormat":
		return bcNewIntlNumberFormat(args)
	case "Map":
		var initArg *Value
		if len(args) > 0 {
			initArg = args[0]
		}
		return newMapValue(initArg)
	case "Set":
		var initArr *Value
		if len(args) > 0 {
			initArr = args[0]
		}
		return newSetValue(initArr)
	case "WeakMap":
		return newWeakMapValue()
	case "WeakSet":
		return newWeakSetValue()
	case "Date":
		return bcNewDate(args)
	case "RegExp":
		if len(args) > 0 {
			pattern := args[0].toStr()
			flags := ""
			if len(args) > 1 {
				flags = args[1].toStr()
			}
			return newRegexpValue(pattern, flags)
		}
		return Undefined
	case "Error", "TypeError", "RangeError", "ReferenceError", "SyntaxError":
		msg := ""
		if len(args) > 0 {
			msg = args[0].toStr()
		}
		return newObj(map[string]*Value{
			"message": newStr(msg),
			"name":    newStr(name),
		})
	default:
		// Check scope for constructor function
		// Handle dotted names: "mod.MyClass" → scope["mod"].MyClass
		var ctor *Value
		if strings.Contains(name, ".") {
			parts := strings.SplitN(name, ".", 2)
			if base, ok := scope[parts[0]]; ok {
				ctor = base.getProp(parts[1])
			}
		} else {
			if v, ok := scope[name]; ok {
				ctor = v
			}
		}
		if ctor != nil && ctor.typ == TypeFunc {
			if ctor.native != nil {
				return ctor.native(args)
			}
			// JS function used as constructor — create this, call function
			thisObj := newObj(make(map[string]*Value))
			if ctor.str == "__arrow" {
				childScope := make(map[string]*Value, len(scope)+1)
				for k, v := range scope {
					childScope[k] = v
				}
				childScope["this"] = thisObj
				result := callArrow(int(ctor.num), args, childScope)
				if result != nil && result.typ == TypeObject {
					return result
				}
				return thisObj
			}
			if ctor.fnBody != "" {
				childScope := make(map[string]*Value, len(scope)+4)
				for k, v := range scope {
					childScope[k] = v
				}
				childScope["this"] = thisObj
				if len(ctor.fnParams) > 0 && len(args) > 0 {
					params := strings.Split(ctor.fnParams[0], ",")
					for i, p := range params {
						p = strings.TrimSpace(p)
						if p != "" && i < len(args) {
							childScope[p] = args[i]
						}
					}
				}
				tokens := tokenizeCached(ctor.fnBody)
				ev := &evaluator{tokens: tokens, pos: 0, scope: childScope}
				result := ev.evalStatements()
				if result != nil && result.typ == TypeObject {
					return result
				}
				return thisObj
			}
		}
		return Undefined
	}
}

// execTryCatch executes a try/catch block in bytecode. Returns non-nil if a return was hit.
func execTryCatch(def *bcTryCatchDef, scope map[string]*Value) (result *Value) {
	// Try body — catch panics
	func() {
		defer func() {
			if r := recover(); r != nil {
				// Caught an error — run catch body if present
				if def.catchBody != nil {
					if def.catchVar != "" {
						if v, ok := r.(*Value); ok {
							scope[def.catchVar] = v
						} else {
							scope[def.catchVar] = newStr(fmt.Sprintf("%v", r))
						}
					}
					res := execBytecode(def.catchBody, scope)
					if res != nil && res != Undefined {
						result = res
					}
				}
			}
		}()
		res := execBytecode(def.tryBody, scope)
		if res != nil && res != Undefined {
			result = res
		}
	}()
	return
}

// evalTemplate evaluates a template literal string with ${...} interpolation.
func evalTemplate(raw string, scope map[string]*Value) *Value {
	var sb strings.Builder
	i := 0
	for i < len(raw) {
		if i+1 < len(raw) && raw[i] == '$' && raw[i+1] == '{' {
			i += 2
			depth := 1
			start := i
			for i < len(raw) && depth > 0 {
				if raw[i] == '{' {
					depth++
				} else if raw[i] == '}' {
					depth--
				}
				if depth > 0 {
					i++
				}
			}
			exprStr := raw[start:i]
			if i < len(raw) {
				i++
			}
			exprTokens := tokenize(exprStr)
			ev := &evaluator{tokens: exprTokens, pos: 0, scope: scope}
			sb.WriteString(ev.expr().toStr())
		} else {
			sb.WriteByte(raw[i])
			i++
		}
	}
	return newStr(sb.String())
}

// bcNewIntlNumberFormat creates an Intl.NumberFormat-like object for bytecode.
func bcNewIntlNumberFormat(args []*Value) *Value {
	style := ""
	currency := ""
	maxFrac := -1
	minFrac := -1
	if len(args) > 1 && args[1].typ == TypeObject && args[1].object != nil {
		opts := args[1].object
		if s, ok := opts["style"]; ok {
			style = s.toStr()
		}
		if c, ok := opts["currency"]; ok {
			currency = c.toStr()
		}
		if v, ok := opts["minimumFractionDigits"]; ok {
			minFrac = int(v.toNum())
		}
		if v, ok := opts["maximumFractionDigits"]; ok {
			maxFrac = int(v.toNum())
		}
	}
	fmtStyle := style
	fmtCurrency := currency
	fmtMinFrac := minFrac
	fmtMaxFrac := maxFrac
	formatFn := NewNativeFunc(func(a []*Value) *Value {
		if len(a) == 0 {
			return internStr("")
		}
		n := a[0].toNum()
		if fmtStyle == "currency" {
			prefix := "$"
			switch strings.ToUpper(fmtCurrency) {
			case "EUR":
				prefix = "€"
			case "GBP":
				prefix = "£"
			case "JPY":
				prefix = "¥"
			}
			if fmtMaxFrac == 0 {
				return newStr(prefix + formatWithCommas(int64(n)))
			}
			frac := 2
			if fmtMinFrac >= 0 {
				frac = fmtMinFrac
			}
			return newStr(prefix + strconv.FormatFloat(n, 'f', frac, 64))
		}
		return newStr(formatWithCommas(int64(n)))
	})
	return newObj(map[string]*Value{"format": formatFn})
}

// bcNewDate creates a Date-like object for bytecode new Date() calls.
func bcNewDate(args []*Value) *Value {
	return newObj(map[string]*Value{
		"toLocaleTimeString": NewNativeFunc(func(a []*Value) *Value { return internStr("00:00:00") }),
		"toLocaleDateString": NewNativeFunc(func(a []*Value) *Value { return internStr("1/1/2026") }),
		"toISOString":        NewNativeFunc(func(a []*Value) *Value { return internStr("2026-01-01T00:00:00.000Z") }),
		"toString":           NewNativeFunc(func(a []*Value) *Value { return internStr("Thu Jan 01 2026") }),
		"getTime":            NewNativeFunc(func(a []*Value) *Value { return internNum(0) }),
		"getFullYear":        NewNativeFunc(func(a []*Value) *Value { return internNum(2026) }),
		"getMonth":           NewNativeFunc(func(a []*Value) *Value { return internNum(0) }),
		"getDate":            NewNativeFunc(func(a []*Value) *Value { return internNum(1) }),
		"getHours":           NewNativeFunc(func(a []*Value) *Value { return internNum(0) }),
		"getMinutes":         NewNativeFunc(func(a []*Value) *Value { return internNum(0) }),
		"getSeconds":         NewNativeFunc(func(a []*Value) *Value { return internNum(0) }),
	})
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
