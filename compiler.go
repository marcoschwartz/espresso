package espresso

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// compileTokens compiles pre-tokenized JS code into a compiledPage.
// Returns nil if compilation fails (caller falls back to interpreted eval).
func compileTokens(tokens []tok) *compiledPage {
	c := &compiler{tokens: tokens, pos: 0}
	defer func() {
		if r := recover(); r != nil {
			// compilation failed — will fall back to interpreted
		}
	}()
	return c.compilePage()
}

type compiler struct {
	tokens []tok
	pos    int
}

func (c *compiler) peek() tok {
	if c.pos < len(c.tokens) {
		return c.tokens[c.pos]
	}
	return tok{t: tokEOF}
}

func (c *compiler) peekAt(offset int) tok {
	idx := c.pos + offset
	if idx < len(c.tokens) {
		return c.tokens[idx]
	}
	return tok{t: tokEOF}
}

func (c *compiler) advance() tok {
	t := c.peek()
	if c.pos < len(c.tokens) {
		c.pos++
	}
	return t
}

func (c *compiler) expect(t tokType) {
	c.advance()
}

func (c *compiler) compilePage() *compiledPage {
	page := &compiledPage{}

	for c.peek().t != tokEOF {
		t := c.peek()

		// const/let/var
		if t.t == tokIdent && (t.v == "const" || t.v == "let" || t.v == "var") {
			c.advance()

			// Array destructuring
			if c.peek().t == tokLBrack {
				c.advance()
				var names []string
				for c.peek().t != tokRBrack && c.peek().t != tokEOF {
					if c.peek().t == tokIdent {
						names = append(names, c.advance().v)
					} else {
						c.advance()
					}
				}
				c.expect(tokRBrack)
				c.expect(tokAssign)
				expr := c.compileExpr()
				page.Preamble = append(page.Preamble, compiledStmt{
					IsArrayDestructure: true,
					Names:              names,
					Expr:               expr,
				})
				if c.peek().t == tokSemi {
					c.advance()
				}
				continue
			}

			name := c.advance().v
			c.expect(tokAssign)
			expr := c.compileExpr()
			page.Preamble = append(page.Preamble, compiledStmt{Name: name, Expr: expr})
			if c.peek().t == tokSemi {
				c.advance()
			}
			continue
		}

		// return
		if t.t == tokIdent && t.v == "return" {
			c.advance()
			page.ReturnExpr = c.compileExpr()
			return page
		}

		// if statement
		if t.t == tokIdent && t.v == "if" {
			c.advance() // skip "if"
			c.expect(tokLParen)
			cond := c.compileExpr()
			c.expect(tokRParen)

			// Compile if body
			var ifBody *compiledPage
			if c.peek().t == tokLBrace {
				ifBody = c.compileBlock()
			}

			// Compile else body
			var elseBody *compiledPage
			if c.peek().t == tokIdent && c.peek().v == "else" {
				c.advance()
				if c.peek().t == tokIdent && c.peek().v == "if" {
					// else if — let the loop pick up "if" next
					continue
				}
				if c.peek().t == tokLBrace {
					elseBody = c.compileBlock()
				}
			}

			page.Preamble = append(page.Preamble, compiledStmt{
				IsIf:      true,
				Condition: cond,
				IfBody:    ifBody,
				ElseBody:  elseBody,
			})
			continue
		}

		// for loop
		if t.t == tokIdent && t.v == "for" {
			c.advance() // skip "for"
			c.expect(tokLParen)

			// Check for for...of
			if c.peek().t == tokIdent && (c.peek().v == "const" || c.peek().v == "let" || c.peek().v == "var") {
				c.advance() // skip const/let/var
				varName := c.advance().v
				if c.peek().t == tokIdent && c.peek().v == "of" {
					c.advance() // skip "of"
					iterExpr := c.compileExpr()
					c.expect(tokRParen)
					var body *compiledPage
					if c.peek().t == tokLBrace {
						body = c.compileBlock()
					}
					page.Preamble = append(page.Preamble, compiledStmt{
						IsForOf: true, IterVar: varName, IterExpr: iterExpr, LoopBody: body,
					})
					continue
				}
				// Regular for: already consumed const/let and name
				c.expect(tokAssign)
				initExpr := c.compileExpr()
				initStmt := &compiledStmt{Name: varName, Expr: initExpr}
				c.expect(tokSemi)
				condExpr := c.compileExpr()
				c.expect(tokSemi)
				// Update expression — handle i++ specially
				var updateExpr compiledExpr
				updateName := c.peek().v
				if c.peek().t == tokIdent {
					c.advance()
					if c.peek().t == tokPlusPlus {
						c.advance()
						n := updateName
						updateExpr = func(scope map[string]*Value) *Value {
							if v, ok := scope[n]; ok {
								scope[n] = newNum(v.toNum() + 1)
							}
							return Undefined
						}
					} else if c.peek().t == tokMinusMinus {
						c.advance()
						n := updateName
						updateExpr = func(scope map[string]*Value) *Value {
							if v, ok := scope[n]; ok {
								scope[n] = newNum(v.toNum() - 1)
							}
							return Undefined
						}
					} else if c.peek().t == tokPlusAssign {
						c.advance()
						val := c.compileExpr()
						n := updateName
						updateExpr = func(scope map[string]*Value) *Value {
							if v, ok := scope[n]; ok {
								scope[n] = newNum(v.toNum() + val(scope).toNum())
							}
							return Undefined
						}
					} else {
						// Unknown update — skip
						updateExpr = func(scope map[string]*Value) *Value { return Undefined }
					}
				}
				c.expect(tokRParen)
				var body *compiledPage
				if c.peek().t == tokLBrace {
					body = c.compileBlock()
				}
				page.Preamble = append(page.Preamble, compiledStmt{
					IsForLoop: true, InitStmt: initStmt, LoopCond: condExpr, LoopUpdate: updateExpr, LoopBody: body,
				})
			} else {
				// for (expr; ...; ...) — skip init
				if c.peek().t != tokSemi {
					c.compileExpr()
				}
				c.expect(tokSemi)
				_ = c.compileExpr() // condition (unused in simple skip)
				c.expect(tokSemi)
				if c.peek().t != tokRParen {
					c.compileExpr()
				}
				c.expect(tokRParen)
				if c.peek().t == tokLBrace {
					c.compileBlock()
				}
			}
			continue
		}

		// while loop
		if t.t == tokIdent && t.v == "while" {
			c.advance()
			c.expect(tokLParen)
			cond := c.compileExpr()
			c.expect(tokRParen)
			var body *compiledPage
			if c.peek().t == tokLBrace {
				body = c.compileBlock()
			}
			page.Preamble = append(page.Preamble, compiledStmt{
				IsWhile: true, LoopCond: cond, LoopBody: body,
			})
			continue
		}

		// try/catch
		if t.t == tokIdent && t.v == "try" {
			c.advance()
			var tryBody *compiledPage
			if c.peek().t == tokLBrace {
				tryBody = c.compileBlock()
			}
			if c.peek().t == tokIdent && c.peek().v == "catch" {
				c.advance()
				if c.peek().t == tokLParen {
					c.skipBalancedCompiler(tokLParen, tokRParen)
				}
				if c.peek().t == tokLBrace {
					c.skipBalancedCompiler(tokLBrace, tokRBrace)
				}
			}
			if c.peek().t == tokIdent && c.peek().v == "finally" {
				c.advance()
				if c.peek().t == tokLBrace {
					c.skipBalancedCompiler(tokLBrace, tokRBrace)
				}
			}
			page.Preamble = append(page.Preamble, compiledStmt{IsTryCatch: true, TryBody: tryBody})
			continue
		}

		// console.log/warn/error
		if t.t == tokIdent && t.v == "console" {
			c.advance()
			method := "log"
			if c.peek().t == tokDot {
				c.advance()
				method = c.advance().v
			}
			if c.peek().t == tokLParen {
				c.advance() // skip (
				var argFns []func(scope map[string]*Value) *Value
				for c.peek().t != tokRParen && c.peek().t != tokEOF {
					argFns = append(argFns, c.compileExpr())
					if c.peek().t == tokComma {
						c.advance()
					}
				}
				c.expect(tokRParen)
				m := method
				page.Preamble = append(page.Preamble, compiledStmt{
					ExprFn: func(scope map[string]*Value) *Value {
						var parts []string
						for _, fn := range argFns {
							parts = append(parts, fn(scope).toStr())
						}
						line := strings.Join(parts, " ")
						switch m {
						case "warn", "error":
							fmt.Fprintln(os.Stderr, line)
						default:
							fmt.Println(line)
						}
						return Undefined
					},
				})
			} else {
				page.Preamble = append(page.Preamble, compiledStmt{IsNoop: true})
			}
			if c.peek().t == tokSemi {
				c.advance()
			}
			continue
		}

		// Identifier — check for reassignment, ++, +=
		if t.t == tokIdent {
			name := t.v
			// name++
			if c.peekAt(1).t == tokPlusPlus {
				c.advance()
				c.advance()
				if c.peek().t == tokSemi {
					c.advance()
				}
				page.Preamble = append(page.Preamble, compiledStmt{IsIncrement: true, Name: name, IncrDelta: 1})
				continue
			}
			// name--
			if c.peekAt(1).t == tokMinusMinus {
				c.advance()
				c.advance()
				if c.peek().t == tokSemi {
					c.advance()
				}
				page.Preamble = append(page.Preamble, compiledStmt{IsIncrement: true, Name: name, IncrDelta: -1})
				continue
			}
			// name += expr
			if c.peekAt(1).t == tokPlusAssign {
				c.advance()
				c.advance()
				expr := c.compileExpr()
				if c.peek().t == tokSemi {
					c.advance()
				}
				page.Preamble = append(page.Preamble, compiledStmt{IsCompound: true, Name: name, CompoundOp: "+=", Expr: expr})
				continue
			}
			// name -= expr
			if c.peekAt(1).t == tokMinusAssign {
				c.advance()
				c.advance()
				expr := c.compileExpr()
				if c.peek().t == tokSemi {
					c.advance()
				}
				page.Preamble = append(page.Preamble, compiledStmt{IsCompound: true, Name: name, CompoundOp: "-=", Expr: expr})
				continue
			}
			// name = expr (reassignment, not declaration)
			if c.peekAt(1).t == tokAssign {
				c.advance()
				c.advance()
				expr := c.compileExpr()
				if c.peek().t == tokSemi {
					c.advance()
				}
				page.Preamble = append(page.Preamble, compiledStmt{IsReassign: true, Name: name, Expr: expr})
				continue
			}
			// Bare function call or expression
			c.compileExpr()
			if c.peek().t == tokSemi {
				c.advance()
			}
			continue
		}

		c.advance()
	}
	return nil
}

// compileBlock compiles a { ... } block into a compiledPage.
func (c *compiler) compileBlock() *compiledPage {
	c.expect(tokLBrace)
	block := &compiledPage{}

	for c.peek().t != tokRBrace && c.peek().t != tokEOF {
		t := c.peek()

		// return
		if t.t == tokIdent && t.v == "return" {
			c.advance()
			block.ReturnExpr = c.compileExpr()
			if c.peek().t == tokSemi {
				c.advance()
			}
			for c.peek().t != tokRBrace && c.peek().t != tokEOF {
				c.advance()
			}
			break
		}

		// const/let/var
		if t.t == tokIdent && (t.v == "const" || t.v == "let" || t.v == "var") {
			c.advance()
			name := c.advance().v
			c.expect(tokAssign)
			expr := c.compileExpr()
			block.Preamble = append(block.Preamble, compiledStmt{Name: name, Expr: expr})
			if c.peek().t == tokSemi {
				c.advance()
			}
			continue
		}

		// if
		if t.t == tokIdent && t.v == "if" {
			c.advance()
			c.expect(tokLParen)
			cond := c.compileExpr()
			c.expect(tokRParen)
			var ifBody *compiledPage
			if c.peek().t == tokLBrace {
				ifBody = c.compileBlock()
			}
			var elseBody *compiledPage
			if c.peek().t == tokIdent && c.peek().v == "else" {
				c.advance()
				if c.peek().t == tokLBrace {
					elseBody = c.compileBlock()
				}
			}
			block.Preamble = append(block.Preamble, compiledStmt{IsIf: true, Condition: cond, IfBody: ifBody, ElseBody: elseBody})
			continue
		}

		// Identifier: ++, --, +=, -=, reassignment, or expression
		if t.t == tokIdent {
			name := t.v
			if c.peekAt(1).t == tokPlusPlus {
				c.advance()
				c.advance()
				if c.peek().t == tokSemi {
					c.advance()
				}
				block.Preamble = append(block.Preamble, compiledStmt{IsIncrement: true, Name: name, IncrDelta: 1})
				continue
			}
			if c.peekAt(1).t == tokMinusMinus {
				c.advance()
				c.advance()
				if c.peek().t == tokSemi {
					c.advance()
				}
				block.Preamble = append(block.Preamble, compiledStmt{IsIncrement: true, Name: name, IncrDelta: -1})
				continue
			}
			if c.peekAt(1).t == tokPlusAssign {
				c.advance()
				c.advance()
				expr := c.compileExpr()
				if c.peek().t == tokSemi {
					c.advance()
				}
				block.Preamble = append(block.Preamble, compiledStmt{IsCompound: true, Name: name, CompoundOp: "+=", Expr: expr})
				continue
			}
			if c.peekAt(1).t == tokMinusAssign {
				c.advance()
				c.advance()
				expr := c.compileExpr()
				if c.peek().t == tokSemi {
					c.advance()
				}
				block.Preamble = append(block.Preamble, compiledStmt{IsCompound: true, Name: name, CompoundOp: "-=", Expr: expr})
				continue
			}
			if c.peekAt(1).t == tokAssign {
				c.advance()
				c.advance()
				expr := c.compileExpr()
				if c.peek().t == tokSemi {
					c.advance()
				}
				block.Preamble = append(block.Preamble, compiledStmt{IsReassign: true, Name: name, Expr: expr})
				continue
			}
			c.compileExpr()
			if c.peek().t == tokSemi {
				c.advance()
			}
			continue
		}

		c.advance()
	}
	c.expect(tokRBrace)
	return block
}

// ─── Expression Compiler ────────────────────────────────────────

func (c *compiler) compileExpr() compiledExpr {
	return c.compileTernary()
}

func (c *compiler) compileTernary() compiledExpr {
	left := c.compileNullishCoalesce()
	if c.peek().t == tokQuestion {
		c.advance()
		consequent := c.compileExpr()
		if c.peek().t == tokColon {
			c.advance()
			alternate := c.compileExpr()
			return func(scope map[string]*Value) *Value {
				if left(scope).truthy() {
					return consequent(scope)
				}
				return alternate(scope)
			}
		}
		// Missing colon (transpiler bug workaround)
		return func(scope map[string]*Value) *Value {
			if left(scope).truthy() {
				return consequent(scope)
			}
			return Null
		}
	}
	return left
}

func (c *compiler) compileNullishCoalesce() compiledExpr {
	left := c.compileLogicalOr()
	for c.peek().t == tokNullCoalesce {
		c.advance()
		right := c.compileLogicalOr()
		prev := left
		left = func(scope map[string]*Value) *Value {
			l := prev(scope)
			if l.typ == TypeNull || l.typ == TypeUndefined {
				return right(scope)
			}
			return l
		}
	}
	return left
}

func (c *compiler) compileLogicalOr() compiledExpr {
	left := c.compileLogicalAnd()
	for c.peek().t == tokOr {
		c.advance()
		right := c.compileLogicalAnd()
		prev := left
		left = func(scope map[string]*Value) *Value {
			l := prev(scope)
			if l.truthy() {
				return l
			}
			return right(scope)
		}
	}
	return left
}

func (c *compiler) compileLogicalAnd() compiledExpr {
	left := c.compileEquality()
	for c.peek().t == tokAnd {
		c.advance()
		right := c.compileEquality()
		prev := left
		left = func(scope map[string]*Value) *Value {
			l := prev(scope)
			if !l.truthy() {
				return l
			}
			return right(scope)
		}
	}
	return left
}

func (c *compiler) compileEquality() compiledExpr {
	left := c.compileComparison()
	for {
		t := c.peek().t
		if t == tokEqEqEq || t == tokEqEq {
			c.advance()
			right := c.compileComparison()
			prev := left
			left = func(scope map[string]*Value) *Value {
				return newBool(strictEqual(prev(scope), right(scope)))
			}
		} else if t == tokNotEqEq || t == tokNotEq {
			c.advance()
			right := c.compileComparison()
			prev := left
			left = func(scope map[string]*Value) *Value {
				return newBool(!strictEqual(prev(scope), right(scope)))
			}
		} else {
			break
		}
	}
	return left
}

func (c *compiler) compileComparison() compiledExpr {
	left := c.compileAdditive()
	for {
		t := c.peek().t
		switch t {
		case tokGt:
			c.advance()
			right := c.compileAdditive()
			prev := left
			left = func(scope map[string]*Value) *Value {
				return newBool(prev(scope).toNum() > right(scope).toNum())
			}
		case tokLt:
			c.advance()
			right := c.compileAdditive()
			prev := left
			left = func(scope map[string]*Value) *Value {
				return newBool(prev(scope).toNum() < right(scope).toNum())
			}
		case tokGtEq:
			c.advance()
			right := c.compileAdditive()
			prev := left
			left = func(scope map[string]*Value) *Value {
				return newBool(prev(scope).toNum() >= right(scope).toNum())
			}
		case tokLtEq:
			c.advance()
			right := c.compileAdditive()
			prev := left
			left = func(scope map[string]*Value) *Value {
				return newBool(prev(scope).toNum() <= right(scope).toNum())
			}
		default:
			return left
		}
	}
}

func (c *compiler) compileAdditive() compiledExpr {
	left := c.compileMultiplicative()
	for {
		t := c.peek().t
		if t == tokPlus {
			c.advance()
			right := c.compileMultiplicative()
			prev := left
			left = func(scope map[string]*Value) *Value {
				l, r := prev(scope), right(scope)
				if l.typ == TypeString || r.typ == TypeString {
					return newStr(l.toStr() + r.toStr())
				}
				return newNum(l.toNum() + r.toNum())
			}
		} else if t == tokMinus {
			c.advance()
			right := c.compileMultiplicative()
			prev := left
			left = func(scope map[string]*Value) *Value {
				return newNum(prev(scope).toNum() - right(scope).toNum())
			}
		} else {
			break
		}
	}
	return left
}

func (c *compiler) compileMultiplicative() compiledExpr {
	left := c.compileUnary()
	for {
		t := c.peek().t
		if t == tokStar {
			c.advance()
			right := c.compileUnary()
			prev := left
			left = func(scope map[string]*Value) *Value {
				return newNum(prev(scope).toNum() * right(scope).toNum())
			}
		} else if t == tokSlash {
			c.advance()
			right := c.compileUnary()
			prev := left
			left = func(scope map[string]*Value) *Value {
				r := right(scope).toNum()
				if r == 0 {
					return newNum(0)
				}
				return newNum(prev(scope).toNum() / r)
			}
		} else if t == tokPercent {
			c.advance()
			right := c.compileUnary()
			prev := left
			left = func(scope map[string]*Value) *Value {
				r := right(scope).toNum()
				if r == 0 {
					return newNum(0)
				}
				l := prev(scope).toNum()
				return newNum(float64(int64(l) % int64(r)))
			}
		} else {
			break
		}
	}
	return left
}

func (c *compiler) compileUnary() compiledExpr {
	if c.peek().t == tokNot {
		c.advance()
		val := c.compileUnary()
		return func(scope map[string]*Value) *Value {
			return newBool(!val(scope).truthy())
		}
	}
	if c.peek().t == tokMinus {
		c.advance()
		val := c.compileUnary()
		return func(scope map[string]*Value) *Value {
			return newNum(-val(scope).toNum())
		}
	}
	if c.peek().t == tokIdent && c.peek().v == "typeof" {
		c.advance()
		val := c.compileUnary()
		return func(scope map[string]*Value) *Value {
			v := val(scope)
			switch v.typ {
			case TypeUndefined:
				return newStr("undefined")
			case TypeNull:
				return newStr("object")
			case TypeBool:
				return newStr("boolean")
			case TypeNumber:
				return newStr("number")
			case TypeString:
				return newStr("string")
			case TypeFunc:
				return newStr("function")
			default:
				return newStr("object")
			}
		}
	}
	if c.peek().t == tokIdent && c.peek().v == "new" {
		c.advance()
		ctor := c.advance().v
		if c.peek().t == tokLParen {
			c.advance()
			var ctorArgs []compiledExpr
			for c.peek().t != tokRParen && c.peek().t != tokEOF {
				ctorArgs = append(ctorArgs, c.compileExpr())
				if c.peek().t == tokComma {
					c.advance()
				}
			}
			c.expect(tokRParen)
			_ = ctorArgs
			if ctor == "Date" {
				return func(scope map[string]*Value) *Value {
					return newObj(map[string]*Value{
						"toLocaleTimeString": &Value{typ: TypeFunc, str: "__noop"},
						"toLocaleDateString": &Value{typ: TypeFunc, str: "__noop"},
						"getTime":            &Value{typ: TypeFunc, str: "__noop"},
					})
				}
			}
		}
		return func(scope map[string]*Value) *Value { return Undefined }
	}
	return c.compilePostfix()
}

func (c *compiler) compilePostfix() compiledExpr {
	val := c.compilePrimary()
	for {
		switch c.peek().t {
		case tokDot:
			c.advance()
			prop := c.advance().v
			if c.peek().t == tokLParen {
				// Method call
				val = c.compileMethodCall(val, prop)
			} else {
				prev := val
				val = func(scope map[string]*Value) *Value {
					return prev(scope).getProp(prop)
				}
			}
		case tokOptChain:
			c.advance()
			prop := c.advance().v
			prev := val
			if c.peek().t == tokLParen {
				mc := c.compileMethodCall(prev, prop)
				val = func(scope map[string]*Value) *Value {
					v := prev(scope)
					if v.typ == TypeUndefined || v.typ == TypeNull {
						return Undefined
					}
					return mc(scope)
				}
			} else {
				val = func(scope map[string]*Value) *Value {
					v := prev(scope)
					if v.typ == TypeUndefined || v.typ == TypeNull {
						return Undefined
					}
					return v.getProp(prop)
				}
			}
		case tokLBrack:
			c.advance()
			idx := c.compileExpr()
			c.expect(tokRBrack)
			prev := val
			val = func(scope map[string]*Value) *Value {
				return prev(scope).getProp(idx(scope).toStr())
			}
		case tokLParen:
			// Function call
			val = c.compileFuncCall(val)
		default:
			return val
		}
	}
}

func (c *compiler) compileFuncCall(fn compiledExpr) compiledExpr {
	c.advance() // skip (
	var args []compiledExpr
	for c.peek().t != tokRParen && c.peek().t != tokEOF {
		args = append(args, c.compileExpr())
		if c.peek().t == tokComma {
			c.advance()
		}
	}
	c.expect(tokRParen)

	return func(scope map[string]*Value) *Value {
		fnVal := fn(scope)
		if fnVal.typ == TypeFunc {
			// Evaluate args and call via the interpreted path
			argVals := make([]*Value, len(args))
			for i, a := range args {
				argVals[i] = a(scope)
			}
			// Check for arrow function
			if fnVal.str == "__arrow" {
				return callArrow(int(fnVal.num), argVals, scope)
			}
			// Check for built-in function body
			if fnVal.fnBody != "" {
				childScope := getScope(scope)
				for i, p := range fnVal.fnParams {
					if i < len(argVals) {
						childScope[p] = argVals[i]
					} else {
						childScope[p] = Undefined
					}
				}
				bodyTokens := tokenizeCached(fnVal.fnBody)
				ev := &evaluator{tokens: bodyTokens, pos: 0, scope: childScope}
				result := ev.evalStatements()
				putScope(childScope)
				if result == nil {
					return Undefined
				}
				return result
			}
		}
		return Undefined
	}
}

func (c *compiler) compileMethodCall(obj compiledExpr, method string) compiledExpr {
	c.advance() // skip (

	switch method {
	case "map":
		return c.compileMapCall(obj)
	case "filter":
		return c.compileFilterCall(obj)
	case "join":
		arg := c.compileExpr()
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			arr := obj(scope)
			if arr.typ != TypeArray {
				return newStr("")
			}
			sep := arg(scope).str
			var parts []string
			for _, item := range arr.array {
				parts = append(parts, item.toStr())
			}
			return newStr(strings.Join(parts, sep))
		}
	case "split":
		arg := c.compileExpr()
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			s := obj(scope)
			if s.typ != TypeString {
				return newArr(nil)
			}
			parts := strings.Split(s.str, arg(scope).str)
			arr := make([]*Value, len(parts))
			for i, p := range parts {
				arr[i] = newStr(p)
			}
			return newArr(arr)
		}
	case "isArray":
		arg := c.compileExpr()
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			return newBool(arg(scope).typ == TypeArray)
		}
	case "padStart":
		targetLen := c.compileExpr()
		var padStr compiledExpr
		if c.peek().t == tokComma {
			c.advance()
			padStr = c.compileExpr()
		}
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			s := obj(scope).toStr()
			tl := int(targetLen(scope).toNum())
			ps := " "
			if padStr != nil {
				ps = padStr(scope).toStr()
			}
			for len(s) < tl {
				s = ps + s
			}
			return newStr(s)
		}
	case "toFixed":
		digits := c.compileExpr()
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			return newStr(strconv.FormatFloat(obj(scope).toNum(), 'f', int(digits(scope).toNum()), 64))
		}
	case "toString":
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			return newStr(obj(scope).toStr())
		}
	case "find":
		return c.compileFindCall(obj)
	case "findIndex":
		return c.compileFindIndexCall(obj)
	case "some":
		return c.compileSomeCall(obj)
	case "every":
		return c.compileEveryCall(obj)
	case "reduce":
		return c.compileReduceCall(obj)
	case "concat":
		var args []compiledExpr
		for c.peek().t != tokRParen {
			args = append(args, c.compileExpr())
			if c.peek().t == tokComma {
				c.advance()
			}
		}
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			var result []*Value
			arr := obj(scope)
			if arr.typ == TypeArray {
				result = append(result, arr.array...)
			}
			for _, a := range args {
				v := a(scope)
				if v.typ == TypeArray {
					result = append(result, v.array...)
				} else {
					result = append(result, v)
				}
			}
			return newArr(result)
		}
	case "reverse":
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			arr := obj(scope)
			if arr.typ != TypeArray {
				return arr
			}
			n := len(arr.array)
			result := make([]*Value, n)
			for i, v := range arr.array {
				result[n-1-i] = v
			}
			return newArr(result)
		}
	case "flat":
		var depthExpr compiledExpr
		if c.peek().t != tokRParen {
			depthExpr = c.compileExpr()
		}
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			arr := obj(scope)
			if arr.typ != TypeArray {
				return arr
			}
			d := 1
			if depthExpr != nil {
				d = int(depthExpr(scope).toNum())
			}
			return newArr(flattenArray(arr.array, d))
		}
	case "flatMap":
		// Skip args
		for c.peek().t != tokRParen && c.peek().t != tokEOF {
			c.compileExpr()
			if c.peek().t == tokComma {
				c.advance()
			}
		}
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value { return newArr(nil) }
	case "sort":
		// Skip comparator if present
		for c.peek().t != tokRParen && c.peek().t != tokEOF {
			c.compileExpr()
			if c.peek().t == tokComma {
				c.advance()
			}
		}
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			arr := obj(scope)
			if arr.typ != TypeArray {
				return arr
			}
			result := make([]*Value, len(arr.array))
			copy(result, arr.array)
			sortValues(result)
			return newArr(result)
		}
	case "replace":
		search := c.compileExpr()
		c.expect(tokComma)
		repl := c.compileExpr()
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			v := obj(scope)
			if v.typ != TypeString {
				return v
			}
			return newStr(strings.Replace(v.str, search(scope).toStr(), repl(scope).toStr(), 1))
		}
	case "replaceAll":
		search := c.compileExpr()
		c.expect(tokComma)
		repl := c.compileExpr()
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			v := obj(scope)
			if v.typ != TypeString {
				return v
			}
			return newStr(strings.ReplaceAll(v.str, search(scope).toStr(), repl(scope).toStr()))
		}
	case "startsWith":
		arg := c.compileExpr()
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			return newBool(strings.HasPrefix(obj(scope).toStr(), arg(scope).toStr()))
		}
	case "endsWith":
		arg := c.compileExpr()
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			return newBool(strings.HasSuffix(obj(scope).toStr(), arg(scope).toStr()))
		}
	case "repeat":
		arg := c.compileExpr()
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			n := int(arg(scope).toNum())
			if n <= 0 {
				return newStr("")
			}
			return newStr(strings.Repeat(obj(scope).toStr(), n))
		}
	case "toLowerCase":
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value { return newStr(strings.ToLower(obj(scope).toStr())) }
	case "toUpperCase":
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value { return newStr(strings.ToUpper(obj(scope).toStr())) }
	case "charAt":
		arg := c.compileExpr()
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			s := obj(scope).toStr()
			idx := int(arg(scope).toNum())
			if idx >= 0 && idx < len(s) {
				return newStr(string(s[idx]))
			}
			return newStr("")
		}
	case "indexOf":
		arg := c.compileExpr()
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			v := obj(scope)
			search := arg(scope)
			if v.typ == TypeString {
				return newNum(float64(strings.Index(v.str, search.toStr())))
			}
			if v.typ == TypeArray {
				for i, item := range v.array {
					if strictEqual(item, search) {
						return newNum(float64(i))
					}
				}
			}
			return newNum(-1)
		}
	case "lastIndexOf":
		arg := c.compileExpr()
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			return newNum(float64(strings.LastIndex(obj(scope).toStr(), arg(scope).toStr())))
		}
	case "substring":
		startExpr := c.compileExpr()
		var endExpr compiledExpr
		if c.peek().t == tokComma {
			c.advance()
			endExpr = c.compileExpr()
		}
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			s := obj(scope).toStr()
			start := int(startExpr(scope).toNum())
			end := len(s)
			if endExpr != nil {
				end = int(endExpr(scope).toNum())
			}
			if start < 0 {
				start = 0
			}
			if end > len(s) {
				end = len(s)
			}
			if start > end {
				start, end = end, start
			}
			return newStr(s[start:end])
		}
	case "trim":
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value { return newStr(strings.TrimSpace(obj(scope).toStr())) }
	case "trimStart", "trimLeft":
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			return newStr(strings.TrimLeft(obj(scope).toStr(), " \t\n\r"))
		}
	case "trimEnd", "trimRight":
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			return newStr(strings.TrimRight(obj(scope).toStr(), " \t\n\r"))
		}
	case "includes":
		arg := c.compileExpr()
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			v := obj(scope)
			a := arg(scope)
			if v.typ == TypeString {
				return newBool(strings.Contains(v.str, a.toStr()))
			}
			if v.typ == TypeArray {
				for _, item := range v.array {
					if strictEqual(item, a) {
						return True
					}
				}
			}
			return False
		}
	case "slice":
		startExpr := c.compileExpr()
		var endExpr compiledExpr
		if c.peek().t == tokComma {
			c.advance()
			endExpr = c.compileExpr()
		}
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value {
			v := obj(scope)
			s := int(startExpr(scope).toNum())
			if v.typ == TypeArray {
				arr := v.array
				if s < 0 {
					s = len(arr) + s
				}
				if s < 0 {
					s = 0
				}
				e := len(arr)
				if endExpr != nil {
					e = int(endExpr(scope).toNum())
				}
				if e > len(arr) {
					e = len(arr)
				}
				if s >= e {
					return newArr(nil)
				}
				return newArr(arr[s:e])
			}
			if v.typ == TypeString {
				str := v.str
				if s < 0 {
					s = len(str) + s
				}
				if s < 0 {
					s = 0
				}
				e := len(str)
				if endExpr != nil {
					e = int(endExpr(scope).toNum())
				}
				if e > len(str) {
					e = len(str)
				}
				if s >= e {
					return newStr("")
				}
				return newStr(str[s:e])
			}
			return v
		}
	case "push", "pop", "shift":
		for c.peek().t != tokRParen && c.peek().t != tokEOF {
			c.compileExpr()
			if c.peek().t == tokComma {
				c.advance()
			}
		}
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value { return Undefined }
	default:
		// Unknown method — skip args
		for c.peek().t != tokRParen && c.peek().t != tokEOF {
			c.compileExpr()
			if c.peek().t == tokComma {
				c.advance()
			}
		}
		c.expect(tokRParen)
		return func(scope map[string]*Value) *Value { return Undefined }
	}
}

func (c *compiler) compileMapCall(arr compiledExpr) compiledExpr {
	params := c.parseCompilerArrowParams()
	c.expect(tokArrow)

	bodyStart := c.pos
	hasParen := c.peek().t == tokLParen
	if hasParen {
		c.advance()
	}

	// Compile as generic expression (no h() node path)
	c.pos = bodyStart
	if hasParen {
		// stay before (
	}
	return c.compileMapFallback(arr, params)
}

func (c *compiler) compileMapFallback(arr compiledExpr, params []string) compiledExpr {
	hasParen := c.peek().t == tokLParen
	if hasParen {
		c.advance()
	}
	bodyExpr := c.compileExpr()
	if hasParen {
		c.expect(tokRParen)
	}
	c.expect(tokRParen) // close .map()

	return func(scope map[string]*Value) *Value {
		arrVal := arr(scope)
		if arrVal.typ != TypeArray {
			return newArr(nil)
		}
		results := make([]*Value, 0, len(arrVal.array))
		for i, item := range arrVal.array {
			childScope := getScope(scope)
			if len(params) > 0 {
				childScope[params[0]] = item
			}
			if len(params) > 1 {
				childScope[params[1]] = newNum(float64(i))
			}
			results = append(results, bodyExpr(childScope))
			putScope(childScope)
		}
		return newArr(results)
	}
}

func (c *compiler) compileFilterCall(arr compiledExpr) compiledExpr {
	params := c.parseCompilerArrowParams()
	c.expect(tokArrow)
	body := c.compileExpr()
	c.expect(tokRParen)

	return func(scope map[string]*Value) *Value {
		arrVal := arr(scope)
		if arrVal.typ != TypeArray {
			return newArr(nil)
		}
		var results []*Value
		for i, item := range arrVal.array {
			childScope := getScope(scope)
			if len(params) > 0 {
				childScope[params[0]] = item
			}
			if len(params) > 1 {
				childScope[params[1]] = newNum(float64(i))
			}
			if body(childScope).truthy() {
				results = append(results, item)
			}
			putScope(childScope)
		}
		return newArr(results)
	}
}

func (c *compiler) compileFindCall(arr compiledExpr) compiledExpr {
	params := c.parseCompilerArrowParams()
	c.expect(tokArrow)
	body := c.compileExpr()
	c.expect(tokRParen)

	return func(scope map[string]*Value) *Value {
		arrVal := arr(scope)
		if arrVal.typ != TypeArray {
			return Undefined
		}
		for i, item := range arrVal.array {
			childScope := getScope(scope)
			if len(params) > 0 {
				childScope[params[0]] = item
			}
			if len(params) > 1 {
				childScope[params[1]] = newNum(float64(i))
			}
			found := body(childScope).truthy()
			putScope(childScope)
			if found {
				return item
			}
		}
		return Undefined
	}
}

func (c *compiler) compileFindIndexCall(arr compiledExpr) compiledExpr {
	params := c.parseCompilerArrowParams()
	c.expect(tokArrow)
	body := c.compileExpr()
	c.expect(tokRParen)

	return func(scope map[string]*Value) *Value {
		arrVal := arr(scope)
		if arrVal.typ != TypeArray {
			return newNum(-1)
		}
		for i, item := range arrVal.array {
			childScope := getScope(scope)
			if len(params) > 0 {
				childScope[params[0]] = item
			}
			if len(params) > 1 {
				childScope[params[1]] = newNum(float64(i))
			}
			found := body(childScope).truthy()
			putScope(childScope)
			if found {
				return newNum(float64(i))
			}
		}
		return newNum(-1)
	}
}

func (c *compiler) compileSomeCall(arr compiledExpr) compiledExpr {
	params := c.parseCompilerArrowParams()
	c.expect(tokArrow)
	body := c.compileExpr()
	c.expect(tokRParen)

	return func(scope map[string]*Value) *Value {
		arrVal := arr(scope)
		if arrVal.typ != TypeArray {
			return False
		}
		for i, item := range arrVal.array {
			childScope := getScope(scope)
			if len(params) > 0 {
				childScope[params[0]] = item
			}
			if len(params) > 1 {
				childScope[params[1]] = newNum(float64(i))
			}
			match := body(childScope).truthy()
			putScope(childScope)
			if match {
				return True
			}
		}
		return False
	}
}

func (c *compiler) compileEveryCall(arr compiledExpr) compiledExpr {
	params := c.parseCompilerArrowParams()
	c.expect(tokArrow)
	body := c.compileExpr()
	c.expect(tokRParen)

	return func(scope map[string]*Value) *Value {
		arrVal := arr(scope)
		if arrVal.typ != TypeArray {
			return True
		}
		for i, item := range arrVal.array {
			childScope := getScope(scope)
			if len(params) > 0 {
				childScope[params[0]] = item
			}
			if len(params) > 1 {
				childScope[params[1]] = newNum(float64(i))
			}
			match := body(childScope).truthy()
			putScope(childScope)
			if !match {
				return False
			}
		}
		return True
	}
}

func (c *compiler) compileReduceCall(arr compiledExpr) compiledExpr {
	params := c.parseCompilerArrowParams()
	c.expect(tokArrow)
	body := c.compileExpr()

	var initExpr compiledExpr
	if c.peek().t == tokComma {
		c.advance()
		initExpr = c.compileExpr()
	}
	c.expect(tokRParen)

	return func(scope map[string]*Value) *Value {
		arrVal := arr(scope)
		if arrVal.typ != TypeArray || len(arrVal.array) == 0 {
			if initExpr != nil {
				return initExpr(scope)
			}
			return Undefined
		}
		accParam := "acc"
		itemParam := "item"
		if len(params) > 0 {
			accParam = params[0]
		}
		if len(params) > 1 {
			itemParam = params[1]
		}

		var acc *Value
		startIdx := 0
		if initExpr != nil {
			acc = initExpr(scope)
		} else {
			acc = arrVal.array[0]
			startIdx = 1
		}

		for i := startIdx; i < len(arrVal.array); i++ {
			childScope := getScope(scope)
			childScope[accParam] = acc
			childScope[itemParam] = arrVal.array[i]
			if len(params) > 2 {
				childScope[params[2]] = newNum(float64(i))
			}
			acc = body(childScope)
			putScope(childScope)
		}
		return acc
	}
}

func (c *compiler) parseCompilerArrowParams() []string {
	var params []string
	if c.peek().t == tokLParen {
		c.advance()
		for c.peek().t != tokRParen && c.peek().t != tokEOF {
			if c.peek().t == tokIdent {
				params = append(params, c.advance().v)
			}
			if c.peek().t == tokComma {
				c.advance()
			}
		}
		c.advance() // skip )
	} else if c.peek().t == tokIdent {
		params = append(params, c.advance().v)
	}
	return params
}

// ─── Primary Expression Compiler ────────────────────────────────

func (c *compiler) compilePrimary() compiledExpr {
	t := c.peek()

	switch t.t {
	case tokStr:
		c.advance()
		s := t.v
		return func(scope map[string]*Value) *Value { return newStr(s) }

	case tokTemplatePart:
		raw := c.advance().v
		return func(scope map[string]*Value) *Value {
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
					exprEv := &evaluator{tokens: exprTokens, pos: 0, scope: scope}
					sb.WriteString(exprEv.expr().toStr())
				} else {
					sb.WriteByte(raw[i])
					i++
				}
			}
			return newStr(sb.String())
		}

	case tokNum:
		c.advance()
		n := t.n
		return func(scope map[string]*Value) *Value { return newNum(n) }

	case tokLParen:
		// Check for arrow function
		if c.isCompilerArrowFunction() {
			return c.compileArrowFunction()
		}
		c.advance()
		val := c.compileExpr()
		c.expect(tokRParen)
		return val

	case tokLBrack:
		return c.compileArray()

	case tokLBrace:
		return c.compileObject()

	case tokIdent:
		switch t.v {
		case "true":
			c.advance()
			return func(scope map[string]*Value) *Value { return True }
		case "false":
			c.advance()
			return func(scope map[string]*Value) *Value { return False }
		case "null":
			c.advance()
			return func(scope map[string]*Value) *Value { return Null }
		case "undefined":
			c.advance()
			return func(scope map[string]*Value) *Value { return Undefined }
		case "Object":
			c.advance()
			if c.peek().t == tokDot {
				c.advance()
				method := c.advance().v
				if c.peek().t == tokLParen {
					c.advance()
					arg := c.compileExpr()
					var extraArgs []compiledExpr
					for c.peek().t == tokComma {
						c.advance()
						extraArgs = append(extraArgs, c.compileExpr())
					}
					c.expect(tokRParen)
					switch method {
					case "keys":
						return func(scope map[string]*Value) *Value {
							v := arg(scope)
							if v.typ == TypeObject && v.object != nil {
								keys := make([]*Value, 0, len(v.object))
								for k := range v.object {
									keys = append(keys, newStr(k))
								}
								return newArr(keys)
							}
							return newArr(nil)
						}
					case "values":
						return func(scope map[string]*Value) *Value {
							v := arg(scope)
							if v.typ == TypeObject && v.object != nil {
								vals := make([]*Value, 0, len(v.object))
								for _, val := range v.object {
									vals = append(vals, val)
								}
								return newArr(vals)
							}
							return newArr(nil)
						}
					case "entries":
						return func(scope map[string]*Value) *Value {
							v := arg(scope)
							if v.typ == TypeObject && v.object != nil {
								entries := make([]*Value, 0, len(v.object))
								for k, val := range v.object {
									entries = append(entries, newArr([]*Value{newStr(k), val}))
								}
								return newArr(entries)
							}
							return newArr(nil)
						}
					case "assign":
						return func(scope map[string]*Value) *Value {
							target := arg(scope)
							if target.typ != TypeObject || target.object == nil {
								target = &Value{typ: TypeObject, object: make(map[string]*Value)}
							}
							for _, ea := range extraArgs {
								src := ea(scope)
								if src.typ == TypeObject && src.object != nil {
									for k, v := range src.object {
										target.object[k] = v
									}
								}
							}
							return target
						}
					case "freeze":
						return func(scope map[string]*Value) *Value { return arg(scope) }
					}
				}
			}
			return func(scope map[string]*Value) *Value { return Undefined }
		case "Array":
			c.advance()
			if c.peek().t == tokDot {
				c.advance()
				method := c.advance().v
				if c.peek().t == tokLParen {
					c.advance()
					arg := c.compileExpr()
					c.expect(tokRParen)
					switch method {
					case "isArray":
						return func(scope map[string]*Value) *Value { return newBool(arg(scope).typ == TypeArray) }
					case "from":
						return func(scope map[string]*Value) *Value {
							v := arg(scope)
							if v.typ == TypeArray {
								return v
							}
							if v.typ == TypeString {
								arr := make([]*Value, len(v.str))
								for i, ch := range v.str {
									arr[i] = newStr(string(ch))
								}
								return newArr(arr)
							}
							return newArr(nil)
						}
					}
				}
			}
			return func(scope map[string]*Value) *Value { return Undefined }
		case "JSON":
			c.advance()
			if c.peek().t == tokDot {
				c.advance()
				method := c.advance().v
				if c.peek().t == tokLParen {
					c.advance()
					arg := c.compileExpr()
					c.expect(tokRParen)
					switch method {
					case "stringify":
						return func(scope map[string]*Value) *Value {
							b, _ := json.Marshal(valueToInterface(arg(scope)))
							return newStr(string(b))
						}
					case "parse":
						return func(scope map[string]*Value) *Value {
							return jsonToValue(json.RawMessage(arg(scope).toStr()))
						}
					}
				}
			}
			return func(scope map[string]*Value) *Value { return Undefined }
		case "Math":
			c.advance()
			if c.peek().t == tokDot {
				c.advance()
				method := c.advance().v
				if c.peek().t == tokLParen {
					c.advance()
					arg := c.compileExpr()
					var arg2 compiledExpr
					if c.peek().t == tokComma {
						c.advance()
						arg2 = c.compileExpr()
					}
					c.expect(tokRParen)
					return func(scope map[string]*Value) *Value {
						n := arg(scope).toNum()
						switch method {
						case "floor":
							return newNum(float64(int64(n)))
						case "ceil":
							if n == float64(int64(n)) {
								return newNum(n)
							}
							return newNum(float64(int64(n) + 1))
						case "round":
							return newNum(float64(int64(n + 0.5)))
						case "abs":
							if n < 0 {
								return newNum(-n)
							}
							return newNum(n)
						case "min":
							if arg2 != nil {
								b := arg2(scope).toNum()
								if b < n {
									return newNum(b)
								}
							}
							return newNum(n)
						case "max":
							if arg2 != nil {
								b := arg2(scope).toNum()
								if b > n {
									return newNum(b)
								}
							}
							return newNum(n)
						case "random":
							return newNum(0)
						}
						return newNum(n)
					}
				}
			}
			return func(scope map[string]*Value) *Value { return Undefined }
		case "String":
			c.advance()
			if c.peek().t == tokLParen {
				c.advance()
				arg := c.compileExpr()
				c.expect(tokRParen)
				return func(scope map[string]*Value) *Value { return newStr(arg(scope).toStr()) }
			}
			return func(scope map[string]*Value) *Value { return Undefined }
		case "Number":
			c.advance()
			if c.peek().t == tokLParen {
				c.advance()
				arg := c.compileExpr()
				c.expect(tokRParen)
				return func(scope map[string]*Value) *Value { return newNum(arg(scope).toNum()) }
			}
			return func(scope map[string]*Value) *Value { return Undefined }
		case "Boolean":
			c.advance()
			if c.peek().t == tokLParen {
				c.advance()
				arg := c.compileExpr()
				c.expect(tokRParen)
				return func(scope map[string]*Value) *Value { return newBool(arg(scope).truthy()) }
			}
			return func(scope map[string]*Value) *Value { return Undefined }
		case "parseInt":
			c.advance()
			if c.peek().t == tokLParen {
				c.advance()
				arg := c.compileExpr()
				if c.peek().t == tokComma {
					c.advance()
					c.compileExpr()
				} // skip radix
				c.expect(tokRParen)
				return func(scope map[string]*Value) *Value {
					n, err := strconv.ParseInt(strings.TrimSpace(arg(scope).toStr()), 10, 64)
					if err != nil {
						return newNum(0)
					}
					return newNum(float64(n))
				}
			}
			return func(scope map[string]*Value) *Value { return Undefined }
		case "parseFloat":
			c.advance()
			if c.peek().t == tokLParen {
				c.advance()
				arg := c.compileExpr()
				c.expect(tokRParen)
				return func(scope map[string]*Value) *Value {
					n, err := strconv.ParseFloat(strings.TrimSpace(arg(scope).toStr()), 64)
					if err != nil {
						return newNum(0)
					}
					return newNum(n)
				}
			}
			return func(scope map[string]*Value) *Value { return Undefined }
		case "console":
			c.advance()
			method := "log"
			if c.peek().t == tokDot {
				c.advance()
				method = c.advance().v
			}
			if c.peek().t == tokLParen {
				c.advance() // skip (
				var argFns []func(scope map[string]*Value) *Value
				for c.peek().t != tokRParen && c.peek().t != tokEOF {
					argFns = append(argFns, c.compileExpr())
					if c.peek().t == tokComma {
						c.advance()
					}
				}
				c.expect(tokRParen)
				m := method
				return func(scope map[string]*Value) *Value {
					var parts []string
					for _, fn := range argFns {
						parts = append(parts, fn(scope).toStr())
					}
					line := strings.Join(parts, " ")
					switch m {
					case "warn", "error":
						fmt.Fprintln(os.Stderr, line)
					default:
						fmt.Println(line)
					}
					return Undefined
				}
			}
			return func(scope map[string]*Value) *Value { return Undefined }
		default:
			c.advance()
			name := t.v
			// Check for single-param arrow: ident =>
			if c.peek().t == tokArrow {
				c.advance() // skip =>
				if c.peek().t == tokLBrace {
					c.skipCompilerBalanced(tokLBrace, tokRBrace)
				} else {
					c.compileExpr()
				}
				return func(scope map[string]*Value) *Value {
					return &Value{typ: TypeFunc, str: "__noop"}
				}
			}
			return func(scope map[string]*Value) *Value {
				if val, ok := scope[name]; ok {
					return val
				}
				return Undefined
			}
		}

	default:
		c.advance()
		return func(scope map[string]*Value) *Value { return Undefined }
	}
}

func (c *compiler) compileArray() compiledExpr {
	c.expect(tokLBrack)
	var items []compiledExpr
	for c.peek().t != tokRBrack && c.peek().t != tokEOF {
		// Handle spread operator
		if c.peek().t == tokSpread {
			c.advance()
			spreadExpr := c.compileExpr()
			items = append(items, func(scope map[string]*Value) *Value {
				v := spreadExpr(scope)
				// Mark as spread for special handling
				return &Value{typ: TypeArray, array: []*Value{v}, str: "__spread__"}
			})
		} else {
			items = append(items, c.compileExpr())
		}
		if c.peek().t == tokComma {
			c.advance()
		}
	}
	c.expect(tokRBrack)

	if len(items) == 0 {
		return func(scope map[string]*Value) *Value { return newArr(nil) }
	}
	return func(scope map[string]*Value) *Value {
		var arr []*Value
		for _, item := range items {
			v := item(scope)
			if v.str == "__spread__" && v.typ == TypeArray && len(v.array) == 1 {
				inner := v.array[0]
				if inner.typ == TypeArray {
					arr = append(arr, inner.array...)
				} else {
					arr = append(arr, inner)
				}
			} else {
				arr = append(arr, v)
			}
		}
		return newArr(arr)
	}
}

func (c *compiler) compileObject() compiledExpr {
	c.expect(tokLBrace)
	type kv struct {
		key  string
		expr compiledExpr
	}
	var pairs []kv
	var spreadExprs []compiledExpr

	for c.peek().t != tokRBrace && c.peek().t != tokEOF {
		// Spread operator
		if c.peek().t == tokSpread {
			c.advance()
			spreadExprs = append(spreadExprs, c.compileExpr())
			if c.peek().t == tokComma {
				c.advance()
			}
			continue
		}

		var key string
		if c.peek().t == tokStr {
			key = c.advance().v
		} else if c.peek().t == tokIdent {
			key = c.advance().v
		} else if c.peek().t == tokLBrack {
			// Computed property [expr]: value
			c.advance()
			keyExpr := c.compileExpr()
			c.expect(tokRBrack)
			c.expect(tokColon)
			val := c.compileExpr()
			// Store as a dynamic pair
			ke := keyExpr
			ve := val
			spreadExprs = append(spreadExprs, func(scope map[string]*Value) *Value {
				return newObj(map[string]*Value{ke(scope).toStr(): ve(scope)})
			})
			if c.peek().t == tokComma {
				c.advance()
			}
			continue
		} else {
			c.advance()
			continue
		}

		if c.peek().t == tokComma || c.peek().t == tokRBrace {
			// Shorthand
			k := key
			pairs = append(pairs, kv{key: k, expr: func(scope map[string]*Value) *Value {
				if v, ok := scope[k]; ok {
					return v
				}
				return Undefined
			}})
			if c.peek().t == tokComma {
				c.advance()
			}
			continue
		}

		c.expect(tokColon)
		val := c.compileExpr()
		pairs = append(pairs, kv{key: key, expr: val})
		if c.peek().t == tokComma {
			c.advance()
		}
	}
	c.expect(tokRBrace)

	return func(scope map[string]*Value) *Value {
		obj := make(map[string]*Value, len(pairs))
		// Apply spread first
		for _, se := range spreadExprs {
			v := se(scope)
			if v.typ == TypeObject && v.object != nil {
				for k, val := range v.object {
					obj[k] = val
				}
			}
		}
		for _, p := range pairs {
			obj[p.key] = p.expr(scope)
		}
		return newObj(obj)
	}
}

func (c *compiler) isCompilerArrowFunction() bool {
	saved := c.pos
	defer func() { c.pos = saved }()
	c.pos++ // skip (
	depth := 1
	for c.pos < len(c.tokens) && depth > 0 {
		if c.tokens[c.pos].t == tokLParen {
			depth++
		} else if c.tokens[c.pos].t == tokRParen {
			depth--
		}
		c.pos++
	}
	return c.pos < len(c.tokens) && c.tokens[c.pos].t == tokArrow
}

func (c *compiler) compileArrowFunction() compiledExpr {
	c.advance() // skip (
	for c.peek().t != tokRParen && c.peek().t != tokEOF {
		c.advance()
	}
	c.expect(tokRParen)
	c.expect(tokArrow)
	// Skip body
	if c.peek().t == tokLBrace {
		c.skipCompilerBalanced(tokLBrace, tokRBrace)
	} else {
		c.compileExpr()
	}
	return func(scope map[string]*Value) *Value {
		return &Value{typ: TypeFunc, str: "__noop"}
	}
}

func (c *compiler) skipBalancedCompiler(open, close tokType) {
	depth := 1
	c.advance()
	for depth > 0 && c.pos < len(c.tokens) {
		t := c.advance()
		if t.t == open {
			depth++
		} else if t.t == close {
			depth--
		}
	}
}

func (c *compiler) skipCompilerBalanced(open, close tokType) {
	depth := 1
	c.advance()
	for depth > 0 && c.pos < len(c.tokens) {
		t := c.advance()
		if t.t == open {
			depth++
		} else if t.t == close {
			depth--
		}
	}
}
