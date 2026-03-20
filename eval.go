package espresso

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// ─── Arrow Function Registry ────────────────────────────────────
// Stores captured arrow functions so they can be called later.

type arrowFunc struct {
	params  []string
	tokens  []tok
	isBlock bool
	scope   map[string]*Value
}

var (
	arrowRegistry   = make(map[int]*arrowFunc)
	arrowNextID     int
	arrowRegistryMu sync.Mutex
)

func registerArrow(af *arrowFunc) int {
	arrowRegistryMu.Lock()
	arrowNextID++
	id := arrowNextID
	arrowRegistry[id] = af
	arrowRegistryMu.Unlock()
	return id
}

func callArrow(id int, args []*Value, callerScope map[string]*Value) *Value {
	arrowRegistryMu.Lock()
	af, ok := arrowRegistry[id]
	arrowRegistryMu.Unlock()
	if !ok {
		return Undefined
	}

	// Fast path: simple expression arrows (n => n * 2) — reuse caller scope
	// instead of allocating a new map. We save+restore the param values.
	if !af.isBlock && len(af.params) <= 2 && len(af.scope) == 0 {
		// Save old values, set params
		saved := make([]*Value, len(af.params))
		for i, name := range af.params {
			saved[i] = callerScope[name]
			if i < len(args) {
				callerScope[name] = args[i]
			} else {
				callerScope[name] = Undefined
			}
		}
		ev := &evaluator{tokens: af.tokens, pos: 0, scope: callerScope}
		result := ev.expr()
		// Restore
		for i, name := range af.params {
			if saved[i] != nil {
				callerScope[name] = saved[i]
			} else {
				delete(callerScope, name)
			}
		}
		return result
	}

	// General path: build child scope
	childScope := make(map[string]*Value, len(callerScope)+len(af.params))
	for k, v := range af.scope {
		childScope[k] = v
	}
	for k, v := range callerScope {
		childScope[k] = v
	}
	for i, name := range af.params {
		if i < len(args) {
			childScope[name] = args[i]
		} else {
			childScope[name] = Undefined
		}
	}

	ev := &evaluator{tokens: af.tokens, pos: 0, scope: childScope}

	if af.isBlock {
		result := ev.evalStatements()
		if result == nil {
			return Undefined
		}
		return result
	}
	return ev.expr()
}

// ─── Tokenizer ──────────────────────────────────────────────────

type tokType int

const (
	tokEOF tokType = iota
	tokIdent
	tokNum
	tokStr
	tokDot
	tokOptChain // ?.
	tokLParen
	tokRParen
	tokLBrack
	tokRBrack
	tokLBrace
	tokRBrace
	tokComma
	tokColon
	tokSemi
	tokPlus
	tokMinus
	tokStar
	tokSlash
	tokPercent
	tokEqEqEq
	tokNotEqEq
	tokEqEq
	tokNotEq
	tokGtEq
	tokLtEq
	tokGt
	tokLt
	tokAnd
	tokOr
	tokNot
	tokQuestion
	tokNullCoalesce // ??
	tokArrow        // =>
	tokAssign
	tokSpread       // ...
	tokTemplatePart // parts of template literals: `text${...}text`
	tokPlusPlus     // ++
	tokMinusMinus   // --
	tokPlusAssign   // +=
	tokMinusAssign  // -=
)

type tok struct {
	t   tokType
	v   string
	n   float64
}

func tokenize(src string) []tok {
	var tokens []tok
	i := 0
	for i < len(src) {
		// skip whitespace
		for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r') {
			i++
		}
		if i >= len(src) {
			break
		}
		ch := src[i]

		// line comment
		if ch == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		// block comment
		if ch == '/' && i+1 < len(src) && src[i+1] == '*' {
			i += 2
			for i+1 < len(src) {
				if src[i] == '*' && src[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			continue
		}

		// string
		if ch == '"' || ch == '\'' {
			i++
			var sb strings.Builder
			for i < len(src) && src[i] != ch {
				if src[i] == '\\' && i+1 < len(src) {
					i++
					switch src[i] {
					case 'n':
						sb.WriteByte('\n')
					case 't':
						sb.WriteByte('\t')
					case '\\':
						sb.WriteByte('\\')
					default:
						sb.WriteByte(src[i])
					}
				} else {
					sb.WriteByte(src[i])
				}
				i++
			}
			if i < len(src) {
				i++
			}
			tokens = append(tokens, tok{t: tokStr, v: sb.String()})
			continue
		}

		// template literal (simple, no interpolation in transpiled output)
		if ch == '`' {
			i++
			// Capture entire template literal content as single token
			var sb strings.Builder
			for i < len(src) && src[i] != '`' {
				if src[i] == '\\' && i+1 < len(src) { i++; sb.WriteByte(src[i]) } else { sb.WriteByte(src[i]) }
				i++
			}
			if i < len(src) { i++ }
			raw := sb.String()
			if strings.Contains(raw, "${") {
				tokens = append(tokens, tok{t: tokTemplatePart, v: raw})
			} else {
				tokens = append(tokens, tok{t: tokStr, v: raw})
			}
			continue
		}

		// number
		if ch >= '0' && ch <= '9' {
			start := i
			for i < len(src) && ((src[i] >= '0' && src[i] <= '9') || src[i] == '.') {
				i++
			}
			n, _ := strconv.ParseFloat(src[start:i], 64)
			tokens = append(tokens, tok{t: tokNum, v: src[start:i], n: n})
			continue
		}

		// identifier
		if isJSIdentStart(ch) {
			start := i
			for i < len(src) && isJSIdentChar(src[i]) {
				i++
			}
			tokens = append(tokens, tok{t: tokIdent, v: src[start:i]})
			continue
		}

		// multi-char operators
		if ch == '.' && i+2 < len(src) && src[i+1] == '.' && src[i+2] == '.' {
			tokens = append(tokens, tok{t: tokSpread})
			i += 3
			continue
		}
		if ch == '=' && i+2 < len(src) && src[i+1] == '=' && src[i+2] == '=' {
			tokens = append(tokens, tok{t: tokEqEqEq})
			i += 3
			continue
		}
		if ch == '!' && i+2 < len(src) && src[i+1] == '=' && src[i+2] == '=' {
			tokens = append(tokens, tok{t: tokNotEqEq})
			i += 3
			continue
		}
		if ch == '=' && i+1 < len(src) && src[i+1] == '>' {
			tokens = append(tokens, tok{t: tokArrow})
			i += 2
			continue
		}
		if ch == '=' && i+1 < len(src) && src[i+1] == '=' {
			tokens = append(tokens, tok{t: tokEqEq})
			i += 2
			continue
		}
		if ch == '!' && i+1 < len(src) && src[i+1] == '=' {
			tokens = append(tokens, tok{t: tokNotEq})
			i += 2
			continue
		}
		if ch == '&' && i+1 < len(src) && src[i+1] == '&' {
			tokens = append(tokens, tok{t: tokAnd})
			i += 2
			continue
		}
		if ch == '|' && i+1 < len(src) && src[i+1] == '|' {
			tokens = append(tokens, tok{t: tokOr})
			i += 2
			continue
		}
		if ch == '>' && i+1 < len(src) && src[i+1] == '=' {
			tokens = append(tokens, tok{t: tokGtEq})
			i += 2
			continue
		}
		if ch == '<' && i+1 < len(src) && src[i+1] == '=' {
			tokens = append(tokens, tok{t: tokLtEq})
			i += 2
			continue
		}
		if ch == '?' && i+1 < len(src) && src[i+1] == '.' {
			tokens = append(tokens, tok{t: tokOptChain})
			i += 2
			continue
		}

		// single-char
		switch ch {
		case '.':
			tokens = append(tokens, tok{t: tokDot})
		case '(':
			tokens = append(tokens, tok{t: tokLParen})
		case ')':
			tokens = append(tokens, tok{t: tokRParen})
		case '[':
			tokens = append(tokens, tok{t: tokLBrack})
		case ']':
			tokens = append(tokens, tok{t: tokRBrack})
		case '{':
			tokens = append(tokens, tok{t: tokLBrace})
		case '}':
			tokens = append(tokens, tok{t: tokRBrace})
		case ',':
			tokens = append(tokens, tok{t: tokComma})
		case ':':
			tokens = append(tokens, tok{t: tokColon})
		case ';':
			tokens = append(tokens, tok{t: tokSemi})
		case '+':
			if i+1 < len(src) && src[i+1] == '+' {
				tokens = append(tokens, tok{t: tokPlusPlus})
				i++
			} else if i+1 < len(src) && src[i+1] == '=' {
				tokens = append(tokens, tok{t: tokPlusAssign})
				i++
			} else {
				tokens = append(tokens, tok{t: tokPlus})
			}
		case '-':
			if i+1 < len(src) && src[i+1] == '-' {
				tokens = append(tokens, tok{t: tokMinusMinus})
				i++
			} else if i+1 < len(src) && src[i+1] == '=' {
				tokens = append(tokens, tok{t: tokMinusAssign})
				i++
			} else {
				tokens = append(tokens, tok{t: tokMinus})
			}
		case '*':
			tokens = append(tokens, tok{t: tokStar})
		case '/':
			tokens = append(tokens, tok{t: tokSlash})
		case '%':
			tokens = append(tokens, tok{t: tokPercent})
		case '>':
			tokens = append(tokens, tok{t: tokGt})
		case '<':
			tokens = append(tokens, tok{t: tokLt})
		case '!':
			tokens = append(tokens, tok{t: tokNot})
		case '?':
			if i+1 < len(src) && src[i+1] == '?' {
				tokens = append(tokens, tok{t: tokNullCoalesce})
				i++
			} else {
				tokens = append(tokens, tok{t: tokQuestion})
			}
		case '=':
			tokens = append(tokens, tok{t: tokAssign})
		}
		i++
	}
	tokens = append(tokens, tok{t: tokEOF})
	return tokens
}

func isJSIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' || ch == '$'
}
func isJSIdentChar(ch byte) bool {
	return isJSIdentStart(ch) || (ch >= '0' && ch <= '9')
}

// ─── Evaluator ──────────────────────────────────────────────────

type evaluator struct {
	tokens   []tok
	pos      int
	scope    map[string]*Value
	// 4-entry read cache for scope lookups — avoids map hashing.
	// Only active when useCache is true (set on loop body evaluators).
	cacheK   [4]string
	cacheV   [4]*Value
	cacheN   int
	useCache bool
}

func newEvaluator(src string, scope map[string]*Value) *evaluator {
	return &evaluator{
		tokens: tokenize(src),
		pos:    0,
		scope:  scope,
	}
}

func (e *evaluator) peek() tok {
	if e.pos < len(e.tokens) {
		return e.tokens[e.pos]
	}
	return tok{t: tokEOF}
}

// getVar looks up a variable, checking the inline cache first if active.
func (e *evaluator) getVar(name string) (*Value, bool) {
	if e.useCache {
		for i := 0; i < e.cacheN; i++ {
			if e.cacheK[i] == name {
				return e.cacheV[i], true
			}
		}
		v, ok := e.scope[name]
		if ok && e.cacheN < 4 {
			e.cacheK[e.cacheN] = name
			e.cacheV[e.cacheN] = v
			e.cacheN++
		}
		return v, ok
	}
	v, ok := e.scope[name]
	return v, ok
}

// setVar writes a variable and invalidates the cache entry if present.
func (e *evaluator) setVar(name string, val *Value) {
	e.scope[name] = val
	for i := 0; i < e.cacheN; i++ {
		if e.cacheK[i] == name {
			e.cacheV[i] = val
			return
		}
	}
}

// clearCache resets the variable cache (call when scope changes externally).
func (e *evaluator) clearCache() {
	e.cacheN = 0
}

func (e *evaluator) advance() tok {
	t := e.peek()
	if e.pos < len(e.tokens) {
		e.pos++
	}
	return t
}

func (e *evaluator) expect(t tokType) tok {
	tk := e.advance()
	if tk.t != t {
		// best-effort: return what we got
	}
	return tk
}

func (e *evaluator) childScope() *evaluator {
	child := make(map[string]*Value, len(e.scope))
	for k, v := range e.scope {
		child[k] = v
	}
	return &evaluator{scope: child}
}

// evalExpr evaluates the source and returns the result.
func evalExpr(src string, scope map[string]*Value) *Value {
	ev := newEvaluator(src, scope)
	return ev.expr()
}




// ─── Recursive Descent ─────────────────────────────────────────

func (e *evaluator) expr() *Value {
	return e.pratt(0)
}

// Operator precedence levels for the Pratt parser.
// Higher number = tighter binding.
const (
	precNone       = 0
	precNullish    = 1 // ??
	precOr         = 2 // ||
	precAnd        = 3 // &&
	precEquality   = 4 // === !== == !=
	precComparison = 5 // > < >= <=
	precAdditive   = 6 // + -
	precMultiply   = 7 // * / %
)

// tokenPrec returns the precedence of a binary operator token.
func tokenPrec(t tokType) int {
	switch t {
	case tokNullCoalesce:
		return precNullish
	case tokOr:
		return precOr
	case tokAnd:
		return precAnd
	case tokEqEqEq, tokNotEqEq, tokEqEq, tokNotEq:
		return precEquality
	case tokGt, tokLt, tokGtEq, tokLtEq:
		return precComparison
	case tokPlus, tokMinus:
		return precAdditive
	case tokStar, tokSlash, tokPercent:
		return precMultiply
	}
	return precNone
}

// pratt is a Pratt (operator-precedence) parser that replaces the recursive
// descent chain: ternary → nullish → or → and → eq → cmp → add → mul.
// One function, one loop, a precedence table.
func (e *evaluator) pratt(minPrec int) *Value {
	left := e.unary()

	for {
		t := e.peek().t

		// Ternary — lowest precedence, right-associative, short-circuits
		if t == tokQuestion && minPrec == 0 {
			e.advance()
			if left.truthy() {
				consequent := e.expr()
				if e.peek().t == tokColon {
					e.advance()
					e.skipExpr()
				}
				return consequent
			}
			e.skipExpr()
			if e.peek().t == tokColon {
				e.advance()
				return e.expr()
			}
			return Null
		}

		prec := tokenPrec(t)
		if prec == 0 || prec < minPrec {
			break
		}

		e.advance()

		// Short-circuit operators
		switch t {
		case tokOr:
			if left.truthy() {
				// Short-circuit: evaluate but discard right side
				e.pratt(prec + 1)
				// left stays truthy, but check for more ||
				continue
			}
			left = e.pratt(prec + 1)
			continue
		case tokAnd:
			if !left.truthy() {
				// Short-circuit: evaluate but discard right side
				e.pratt(prec + 1)
				continue
			}
			left = e.pratt(prec + 1)
			continue
		case tokNullCoalesce:
			right := e.pratt(prec + 1)
			if left.typ == TypeNull || left.typ == TypeUndefined {
				left = right
			}
			continue
		}

		// Non-short-circuit binary operators
		right := e.pratt(prec + 1)

		switch t {
		// Equality
		case tokEqEqEq:
			left = newBool(strictEqual(left, right))
		case tokNotEqEq:
			left = newBool(!strictEqual(left, right))
		case tokEqEq:
			left = newBool(looseEqual(left, right))
		case tokNotEq:
			left = newBool(!looseEqual(left, right))
		// Comparison
		case tokGt:
			left = newBool(left.toNum() > right.toNum())
		case tokLt:
			left = newBool(left.toNum() < right.toNum())
		case tokGtEq:
			left = newBool(left.toNum() >= right.toNum())
		case tokLtEq:
			left = newBool(left.toNum() <= right.toNum())
		// Additive
		case tokPlus:
			if left.typ == TypeString || right.typ == TypeString {
				left = newStr(left.toStr() + right.toStr())
			} else {
				left = newNum(left.toNum() + right.toNum())
			}
		case tokMinus:
			left = newNum(left.toNum() - right.toNum())
		// Multiplicative
		case tokStar:
			left = newNum(left.toNum() * right.toNum())
		case tokSlash:
			rn := right.toNum()
			if rn != 0 {
				left = newNum(left.toNum() / rn)
			} else {
				left = internNum(0)
			}
		case tokPercent:
			rn := right.toNum()
			if rn != 0 {
				left = newNum(float64(int64(left.toNum()) % int64(rn)))
			} else {
				left = internNum(0)
			}
		}
	}

	return left
}

// skipExpr skips a complete expression without evaluating it.
// It counts balanced parens/brackets/braces to handle nested expressions.
func (e *evaluator) skipExpr() {
	depth := 0
	for e.pos < len(e.tokens) {
		t := e.tokens[e.pos]
		switch t.t {
		case tokLParen, tokLBrack, tokLBrace:
			depth++
			e.pos++
		case tokRParen, tokRBrack, tokRBrace:
			if depth == 0 {
				return
			}
			depth--
			e.pos++
		case tokComma:
			if depth == 0 {
				return
			}
			e.pos++
		case tokColon:
			if depth == 0 {
				return
			}
			e.pos++
		case tokSemi:
			if depth == 0 {
				return
			}
			e.pos++
		case tokEOF:
			return
		default:
			e.pos++
		}
	}
}

func (e *evaluator) unary() *Value {
	if e.peek().t == tokNot {
		e.advance()
		val := e.unary()
		return newBool(!val.truthy())
	}
	// Prefix ++/--
	if e.peek().t == tokPlusPlus {
		e.advance()
		name := e.advance().v
		if v, ok := e.scope[name]; ok {
			nv := newNum(v.toNum() + 1)
			e.scope[name] = nv
			return nv
		}
		return internNum(1)
	}
	if e.peek().t == tokMinusMinus {
		e.advance()
		name := e.advance().v
		if v, ok := e.scope[name]; ok {
			nv := newNum(v.toNum() - 1)
			e.scope[name] = nv
			return nv
		}
		return internNum(-1)
	}
	if e.peek().t == tokMinus {
		e.advance()
		val := e.unary()
		return newNum(-val.toNum())
	}
	if e.peek().t == tokIdent && e.peek().v == "typeof" {
		e.advance()
		val := e.unary()
		switch val.typ {
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
	return e.postfix()
}

func (e *evaluator) postfix() *Value {
	val := e.primary()
	for {
		switch e.peek().t {
		case tokPlusPlus:
			// Postfix ++ in expression context (e.g., i++ in for update)
			e.advance()
			// Find the identifier that produced val and update scope
			if e.pos >= 2 {
				prev := e.tokens[e.pos-2]
				if prev.t == tokIdent {
					if v, ok := e.scope[prev.v]; ok {
						e.scope[prev.v] = newNum(v.toNum() + 1)
					}
				}
			}
			return val
		case tokMinusMinus:
			// Postfix -- in expression context
			e.advance()
			if e.pos >= 2 {
				prev := e.tokens[e.pos-2]
				if prev.t == tokIdent {
					if v, ok := e.scope[prev.v]; ok {
						e.scope[prev.v] = newNum(v.toNum() - 1)
					}
				}
			}
			return val
		case tokDot:
			e.advance()
			prop := e.advance()
			if prop.t == tokIdent {
				val = e.handlePropAccess(val, prop.v)
			}
		case tokOptChain:
			e.advance()
			prop := e.advance()
			if !val.truthy() || val.typ == TypeUndefined || val.typ == TypeNull {
				val = Undefined
				// skip any subsequent call
				if e.peek().t == tokLParen {
					e.skipBalanced(tokLParen, tokRParen)
				}
			} else if prop.t == tokIdent {
				val = e.handlePropAccess(val, prop.v)
			}
		case tokLBrack:
			e.advance()
			idx := e.expr()
			e.expect(tokRBrack)
			val = val.getProp(idx.toStr())
		case tokLParen:
			// Direct function call: val(args...)
			if val.typ == TypeFunc {
				val = e.evalFuncCall(val)
			} else {
				// Not a function — this ( belongs to something else
				return val
			}
		default:
			return val
		}
	}
}

// isArrowFunction looks ahead to check if the current ( starts an arrow function.
func (e *evaluator) isArrowFunction() bool {
	// Save position
	saved := e.pos
	defer func() { e.pos = saved }()

	e.pos++ // skip (
	depth := 1
	for e.pos < len(e.tokens) && depth > 0 {
		if e.tokens[e.pos].t == tokLParen {
			depth++
		} else if e.tokens[e.pos].t == tokRParen {
			depth--
		}
		e.pos++
	}
	// After the closing ), check for =>
	return e.pos < len(e.tokens) && e.tokens[e.pos].t == tokArrow
}

// parseArrowFunction parses an arrow function and returns it as a func value.
func (e *evaluator) parseArrowFunction() *Value {
	e.advance() // skip (
	// Collect params
	var params []string
	for e.peek().t != tokRParen && e.peek().t != tokEOF {
		if e.peek().t == tokIdent {
			params = append(params, e.advance().v)
		} else {
			e.advance()
		}
	}
	e.expect(tokRParen)
	e.expect(tokArrow)

	// Capture arrow body for deferred execution
	var bodyToks []tok
	isBlock := false
	if e.peek().t == tokLBrace {
		isBlock = true
		start := e.pos
		e.skipBalanced(tokLBrace, tokRBrace)
		// Copy tokens inside { } (excluding braces)
		if e.pos-start > 2 {
			bodyToks = make([]tok, e.pos-start-2)
			copy(bodyToks, e.tokens[start+1:e.pos-1])
		}
	} else {
		start := e.pos
		e.expr()
		bodyToks = make([]tok, e.pos-start)
		copy(bodyToks, e.tokens[start:e.pos])
	}
	bodyToks = append(bodyToks, tok{t: tokEOF})

	captured := &arrowFunc{params: params, tokens: bodyToks, isBlock: isBlock, scope: e.scope}
	arrowID := registerArrow(captured)
	return &Value{typ: TypeFunc, str: "__arrow", num: float64(arrowID)}
}

func (e *evaluator) evalFuncCall(fn *Value) *Value {
	e.advance() // skip (

	// Collect args
	var args []*Value
	for e.peek().t != tokRParen && e.peek().t != tokEOF {
		// For arrow function args, check if this is an arrow func being passed
		args = append(args, e.expr())
		if e.peek().t == tokComma {
			e.advance()
		}
	}
	e.expect(tokRParen)

	// Native Go function
	if fn.native != nil {
		return fn.native(args)
	}

	if fn.str == "__noop" {
		return Undefined
	}
	if fn.str == "__resolved" {
		if fn.object != nil {
			if v, ok := fn.object["__value"]; ok {
				return v
			}
		}
		return Undefined
	}
	if fn.str == "__arrow" {
		return callArrow(int(fn.num), args, e.scope)
	}

	// Function with body (from extractFunctions)
	if fn.fnBody != "" {
		props := make(map[string]*Value, len(args))
		if len(fn.fnParams) > 0 && len(args) > 0 {
			// fnParams may be ["param1,param2,param3"] — split and bind positionally
			paramStr := fn.fnParams[0]
			if strings.Contains(paramStr, ",") {
				params := strings.Split(paramStr, ",")
				for i, p := range params {
					p = strings.TrimSpace(p)
					if p != "" && i < len(args) {
						props[p] = args[i]
					}
				}
			} else {
				// Single param — could be destructured { x, y } or simple name
				props[paramStr] = args[0]
			}
		}
		return e.callFunc(fn, props)
	}

	return Undefined
}

func (e *evaluator) handlePropAccess(val *Value, prop string) *Value {
	// Check for method calls
	if e.peek().t == tokLParen {
		return e.handleMethodCall(val, prop)
	}
	return val.getProp(prop)
}

func (e *evaluator) handleMethodCall(val *Value, method string) *Value {
	e.advance() // skip (

	switch method {
	case "map":
		return e.evalMapFilter(val, method)
	case "filter":
		return e.evalMapFilter(val, method)
	case "find":
		return e.evalFind(val)
	case "findIndex":
		return e.evalFindIndex(val)
	case "some":
		return e.evalSomeEvery(val, "some")
	case "every":
		return e.evalSomeEvery(val, "every")
	case "join":
		arg := newStr(",")
		if e.peek().t != tokRParen {
			arg = e.expr()
		}
		e.expect(tokRParen)
		if val.typ == TypeArray {
			var parts []string
			for _, item := range val.array {
				parts = append(parts, item.toStr())
			}
			return newStr(strings.Join(parts, arg.str))
		}
		return internStr("")
	case "split":
		arg := internStr("")
		if e.peek().t != tokRParen {
			arg = e.expr()
		}
		e.expect(tokRParen)
		if val.typ == TypeString {
			parts := strings.Split(val.str, arg.str)
			arr := make([]*Value, len(parts))
			for i, p := range parts {
				arr[i] = newStr(p)
			}
			return newArr(arr)
		}
		return newArr(nil)
	case "trim":
		e.expect(tokRParen)
		if val.typ == TypeString {
			return newStr(strings.TrimSpace(val.str))
		}
		return val
	case "includes":
		arg := e.expr()
		e.expect(tokRParen)
		if val.typ == TypeString {
			return newBool(strings.Contains(val.str, arg.toStr()))
		}
		if val.typ == TypeArray {
			for _, item := range val.array {
				if strictEqual(item, arg) {
					return True
				}
			}
			return False
		}
		return False
	case "slice":
		start := 0
		end := -1
		if e.peek().t != tokRParen {
			start = int(e.expr().toNum())
			if e.peek().t == tokComma {
				e.advance()
				end = int(e.expr().toNum())
			}
		}
		e.expect(tokRParen)
		if val.typ == TypeArray {
			arr := val.array
			if start < 0 {
				start = len(arr) + start
			}
			if start < 0 {
				start = 0
			}
			if end < 0 {
				end = len(arr)
			}
			if end > len(arr) {
				end = len(arr)
			}
			if start >= end {
				return newArr(nil)
			}
			return newArr(arr[start:end])
		}
		if val.typ == TypeString {
			s := val.str
			if start < 0 {
				start = len(s) + start
			}
			if start < 0 {
				start = 0
			}
			if end < 0 {
				end = len(s)
			}
			if end > len(s) {
				end = len(s)
			}
			if start >= end {
				return internStr("")
			}
			return newStr(s[start:end])
		}
		return val
	case "toString":
		e.expect(tokRParen)
		return newStr(val.toStr())
	case "padStart":
		targetLen := 0
		padStr := " "
		if e.peek().t != tokRParen {
			targetLen = int(e.expr().toNum())
			if e.peek().t == tokComma {
				e.advance()
				padStr = e.expr().toStr()
			}
		}
		e.expect(tokRParen)
		s := val.toStr()
		for len(s) < targetLen {
			s = padStr + s
		}
		return newStr(s)
	case "padEnd":
		targetLen := 0
		padStr := " "
		if e.peek().t != tokRParen {
			targetLen = int(e.expr().toNum())
			if e.peek().t == tokComma {
				e.advance()
				padStr = e.expr().toStr()
			}
		}
		e.expect(tokRParen)
		s := val.toStr()
		for len(s) < targetLen {
			s = s + padStr
		}
		return newStr(s)
	case "toFixed":
		digits := 0
		if e.peek().t != tokRParen {
			digits = int(e.expr().toNum())
		}
		e.expect(tokRParen)
		return newStr(strconv.FormatFloat(val.toNum(), 'f', digits, 64))
	case "toLocaleString":
		// Skip args (locale, options) — not used in practice
		for e.peek().t != tokRParen && e.peek().t != tokEOF {
			e.expr()
			if e.peek().t == tokComma { e.advance() }
		}
		e.expect(tokRParen)
		n := val.toNum()
		if n == float64(int64(n)) {
			// Integer — format with thousand separators
			return newStr(formatWithCommas(int64(n)))
		}
		return newStr(strconv.FormatFloat(n, 'f', -1, 64))
	case "isArray":
		// Array.isArray(x)
		arg := e.expr()
		e.expect(tokRParen)
		return newBool(arg.typ == TypeArray)

	// ── String methods ──────────────────────────────────────
	case "replace":
		search := e.expr()
		e.expect(tokComma)
		replacement := e.expr()
		e.expect(tokRParen)
		if val.typ == TypeString {
			return newStr(strings.Replace(val.str, search.toStr(), replacement.toStr(), 1))
		}
		return val
	case "replaceAll":
		search := e.expr()
		e.expect(tokComma)
		replacement := e.expr()
		e.expect(tokRParen)
		if val.typ == TypeString {
			return newStr(strings.ReplaceAll(val.str, search.toStr(), replacement.toStr()))
		}
		return val
	case "startsWith":
		prefix := e.expr()
		e.expect(tokRParen)
		if val.typ == TypeString {
			return newBool(strings.HasPrefix(val.str, prefix.toStr()))
		}
		return False
	case "endsWith":
		suffix := e.expr()
		e.expect(tokRParen)
		if val.typ == TypeString {
			return newBool(strings.HasSuffix(val.str, suffix.toStr()))
		}
		return False
	case "repeat":
		count := 0
		if e.peek().t != tokRParen {
			count = int(e.expr().toNum())
		}
		e.expect(tokRParen)
		if val.typ == TypeString && count > 0 {
			return newStr(strings.Repeat(val.str, count))
		}
		return internStr("")
	case "toLowerCase":
		e.expect(tokRParen)
		return newStr(strings.ToLower(val.toStr()))
	case "toUpperCase":
		e.expect(tokRParen)
		return newStr(strings.ToUpper(val.toStr()))
	case "charAt":
		idx := 0
		if e.peek().t != tokRParen {
			idx = int(e.expr().toNum())
		}
		e.expect(tokRParen)
		s := val.toStr()
		if idx >= 0 && idx < len(s) {
			return newStr(string(s[idx]))
		}
		return internStr("")
	case "indexOf":
		search := e.expr()
		e.expect(tokRParen)
		if val.typ == TypeString {
			return newNum(float64(strings.Index(val.str, search.toStr())))
		}
		if val.typ == TypeArray {
			for i, item := range val.array {
				if strictEqual(item, search) {
					return internNum(float64(i))
				}
			}
			return internNum(-1)
		}
		return internNum(-1)
	case "lastIndexOf":
		search := e.expr()
		e.expect(tokRParen)
		if val.typ == TypeString {
			return newNum(float64(strings.LastIndex(val.str, search.toStr())))
		}
		return internNum(-1)
	case "substring":
		start := 0
		end := -1
		if e.peek().t != tokRParen {
			start = int(e.expr().toNum())
			if e.peek().t == tokComma {
				e.advance()
				end = int(e.expr().toNum())
			}
		}
		e.expect(tokRParen)
		s := val.toStr()
		if start < 0 {
			start = 0
		}
		if end < 0 {
			end = len(s)
		}
		if start > len(s) {
			start = len(s)
		}
		if end > len(s) {
			end = len(s)
		}
		if start > end {
			start, end = end, start
		}
		return newStr(s[start:end])
	case "trimStart", "trimLeft":
		e.expect(tokRParen)
		return newStr(strings.TrimLeft(val.toStr(), " \t\n\r"))
	case "trimEnd", "trimRight":
		e.expect(tokRParen)
		return newStr(strings.TrimRight(val.toStr(), " \t\n\r"))

	// ── Array methods ───────────────────────────────────────
	case "reduce":
		return e.evalReduce(val)
	case "concat":
		var result []*Value
		if val.typ == TypeArray {
			result = append(result, val.array...)
		}
		for e.peek().t != tokRParen {
			arg := e.expr()
			if arg.typ == TypeArray {
				result = append(result, arg.array...)
			} else {
				result = append(result, arg)
			}
			if e.peek().t == tokComma {
				e.advance()
			}
		}
		e.expect(tokRParen)
		return newArr(result)
	case "reverse":
		e.expect(tokRParen)
		if val.typ == TypeArray {
			n := len(val.array)
			result := make([]*Value, n)
			for i, v := range val.array {
				result[n-1-i] = v
			}
			return newArr(result)
		}
		return val
	case "sort":
		if e.peek().t == tokRParen {
			// No comparator — sort by string representation
			e.expect(tokRParen)
			if val.typ == TypeArray {
				result := make([]*Value, len(val.array))
				copy(result, val.array)
				sortValues(result)
				return newArr(result)
			}
			return val
		}
		// With comparator callback
		params := e.parseArrowParams()
		e.expect(tokArrow)
		bodyStart := e.pos
		hasBodyBrace := e.peek().t == tokLBrace
		if hasBodyBrace {
			e.skipBalanced(tokLBrace, tokRBrace)
		} else {
			// expression body — read until )
			depth := 1
			for e.pos < len(e.tokens) {
				if e.tokens[e.pos].t == tokLParen { depth++ }
				if e.tokens[e.pos].t == tokRParen { depth--; if depth == 0 { break } }
				e.pos++
			}
		}
		bodyEnd := e.pos
		e.expect(tokRParen)

		if val.typ == TypeArray {
			// Prepare tokens once
			sortBody := make([]tok, bodyEnd-bodyStart)
			copy(sortBody, e.tokens[bodyStart:bodyEnd])
			if hasBodyBrace && len(sortBody) >= 2 && sortBody[0].t == tokLBrace {
				sortBody = sortBody[1 : len(sortBody)-1]
			}
			sortBody = append(sortBody, tok{t: tokEOF})
			paramA := "a"
			paramB := "b"
			if len(params) > 0 { paramA = params[0] }
			if len(params) > 1 { paramB = params[1] }

			// Reuse scope for expression bodies
			if !hasBodyBrace {
				savedA, hasA := e.scope[paramA]
				savedB, hasB := e.scope[paramB]
				ev := &evaluator{tokens: sortBody, pos: 0, scope: e.scope}
				sort.Slice(val.array, func(i, j int) bool {
					e.scope[paramA] = val.array[i]
					e.scope[paramB] = val.array[j]
					ev.pos = 0
					v := ev.expr()
					if v == nil { return false }
					return v.toNum() < 0
				})
				if hasA { e.scope[paramA] = savedA } else { delete(e.scope, paramA) }
				if hasB { e.scope[paramB] = savedB } else { delete(e.scope, paramB) }
			} else {
				sort.Slice(val.array, func(i, j int) bool {
					childScope := getScope(e.scope)
					childScope[paramA] = val.array[i]
					childScope[paramB] = val.array[j]
					childEval := &evaluator{tokens: sortBody, pos: 0, scope: childScope}
					v := childEval.evalStatements()
					putScope(childScope)
					if v == nil { return false }
					return v.toNum() < 0
				})
			}
			return val // return same array (mutated)
		}
		return val
	case "flat":
		depth := 1
		if e.peek().t != tokRParen {
			depth = int(e.expr().toNum())
		}
		e.expect(tokRParen)
		if val.typ == TypeArray {
			return newArr(flattenArray(val.array, depth))
		}
		return val
	case "flatMap":
		mapped := e.evalMapFilter(val, "map")
		if mapped.typ == TypeArray {
			return newArr(flattenArray(mapped.array, 1))
		}
		return mapped
	case "push":
		// Mutates array, returns new length
		for e.peek().t != tokRParen {
			item := e.expr()
			val.array = append(val.array, item)
			if e.peek().t == tokComma {
				e.advance()
			}
		}
		e.expect(tokRParen)
		return newNum(float64(len(val.array)))
	case "pop":
		e.expect(tokRParen)
		if val.typ == TypeArray && len(val.array) > 0 {
			return val.array[len(val.array)-1]
		}
		return Undefined
	case "shift":
		e.expect(tokRParen)
		if val.typ == TypeArray && len(val.array) > 0 {
			return val.array[0]
		}
		return Undefined
	case "length":
		// .length() — shouldn't be called as method but handle gracefully
		e.expect(tokRParen)
		if val.typ == TypeArray {
			return internNum(float64(len(val.array)))
		}
		if val.typ == TypeString {
			return internNum(float64(len(val.str)))
		}
		return internNum(0)
	case "keys":
		// Object.keys() handled elsewhere, but arr.keys() returns indices
		e.expect(tokRParen)
		if val.typ == TypeObject && val.object != nil {
			keys := make([]*Value, 0, len(val.object))
			for k := range val.object {
				keys = append(keys, newStr(k))
			}
			return newArr(keys)
		}
		return newArr(nil)
	case "values":
		e.expect(tokRParen)
		if val.typ == TypeObject && val.object != nil {
			vals := make([]*Value, 0, len(val.object))
			for _, v := range val.object {
				vals = append(vals, v)
			}
			return newArr(vals)
		}
		return newArr(nil)
	case "entries":
		e.expect(tokRParen)
		if val.typ == TypeObject && val.object != nil {
			entries := make([]*Value, 0, len(val.object))
			for k, v := range val.object {
				entries = append(entries, newArr([]*Value{newStr(k), v}))
			}
			return newArr(entries)
		}
		return newArr(nil)
	case "assign":
		// Object.assign(target, ...sources)
		target := val
		if target.typ != TypeObject {
			target = &Value{typ: TypeObject, object: make(map[string]*Value)}
		}
		for e.peek().t != tokRParen {
			src := e.expr()
			if src.typ == TypeObject && src.object != nil {
				for k, v := range src.object {
					target.object[k] = v
				}
			}
			if e.peek().t == tokComma {
				e.advance()
			}
		}
		e.expect(tokRParen)
		return target

	default:
		// Check if method is a callable property on the object
		if val.typ == TypeObject && val.object != nil {
			if fn, ok := val.object[method]; ok && fn.typ == TypeFunc {
				// ( already consumed by handleMethodCall — collect args and call
				var args []*Value
				for e.peek().t != tokRParen && e.peek().t != tokEOF {
					args = append(args, e.expr())
					if e.peek().t == tokComma {
						e.advance()
					}
				}
				e.expect(tokRParen)
				if fn.native != nil {
					return fn.native(args)
				}
				if fn.str == "__arrow" {
					return callArrow(int(fn.num), args, e.scope)
				}
				return Undefined
			}
		}
		// Unknown method — skip args and return undefined
		for e.peek().t != tokRParen && e.peek().t != tokEOF {
			e.expr()
			if e.peek().t == tokComma {
				e.advance()
			}
		}
		e.expect(tokRParen)
		return Undefined
	}
}

func (e *evaluator) evalMapFilter(val *Value, method string) *Value {
	// Parse arrow function: param => expr  or  (param) => expr  or  (param, idx) => expr
	params := e.parseArrowParams()
	// skip =>
	e.expect(tokArrow)

	// Capture the body tokens until matching )
	bodyStart := e.pos
	// Check if body is wrapped in parens or braces
	hasBodyParen := e.peek().t == tokLParen
	hasBodyBrace := e.peek().t == tokLBrace
	if hasBodyParen {
		e.skipBalanced(tokLParen, tokRParen)
	} else if hasBodyBrace {
		e.skipBalanced(tokLBrace, tokRBrace)
	} else {
		// expression body — read until the closing ) of .map()
		depth := 1
		for e.pos < len(e.tokens) {
			if e.tokens[e.pos].t == tokLParen {
				depth++
			} else if e.tokens[e.pos].t == tokRParen {
				depth--
				if depth == 0 {
					break
				}
			}
			e.pos++
		}
	}
	bodyEnd := e.pos
	e.expect(tokRParen) // close .map()

	if val.typ != TypeArray {
		return newArr(nil)
	}

	// Prepare body tokens once outside the loop
	rawBody := make([]tok, bodyEnd-bodyStart)
	copy(rawBody, e.tokens[bodyStart:bodyEnd])

	isExprBody := !hasBodyBrace
	if hasBodyBrace && len(rawBody) >= 2 && rawBody[0].t == tokLBrace {
		rawBody = rawBody[1 : len(rawBody)-1] // strip { }
	} else if hasBodyParen && len(rawBody) >= 2 && rawBody[0].t == tokLParen {
		rawBody = rawBody[1 : len(rawBody)-1]
	}
	bodyTokens := append(rawBody, tok{t: tokEOF})

	// Check for destructured first param: ([a, b, c]) => ...
	isDestructured := len(params) > 0 && strings.HasPrefix(params[0], "__destructure__:")
	var destructNames []string
	if isDestructured {
		destructNames = strings.Split(params[0][len("__destructure__:"):], ",")
	}

	// For simple expression bodies, reuse scope and evaluator
	if isExprBody && len(params) <= 2 {
		results := make([]*Value, 0, len(val.array))
		ev := &evaluator{tokens: bodyTokens, pos: 0, scope: e.scope}

		// Save original param values
		var savedVars []struct{ name string; val *Value; has bool }
		if isDestructured {
			for _, name := range destructNames {
				v, ok := e.scope[name]
				savedVars = append(savedVars, struct{ name string; val *Value; has bool }{name, v, ok})
			}
		} else {
			v, ok := e.scope[params[0]]
			savedVars = append(savedVars, struct{ name string; val *Value; has bool }{params[0], v, ok})
		}
		if len(params) > 1 {
			v, ok := e.scope[params[1]]
			savedVars = append(savedVars, struct{ name string; val *Value; has bool }{params[1], v, ok})
		}

		for i, item := range val.array {
			if isDestructured {
				// Spread array item into named vars
				for j, name := range destructNames {
					if item.typ == TypeArray && j < len(item.array) {
						e.scope[name] = item.array[j]
					} else {
						e.scope[name] = Undefined
					}
				}
			} else {
				e.scope[params[0]] = item
			}
			if len(params) > 1 {
				e.scope[params[1]] = internNum(float64(i))
			}
			ev.pos = 0
			result := ev.expr()

			if method == "filter" {
				if result.truthy() {
					results = append(results, item)
				}
			} else {
				results = append(results, result)
			}
		}

		// Restore
		for _, sv := range savedVars {
			if sv.has {
				e.scope[sv.name] = sv.val
			} else {
				delete(e.scope, sv.name)
			}
		}
		return newArr(results)
	}

	// General path: block bodies or complex cases
	var results []*Value
	for i, item := range val.array {
		childScope := getScope(e.scope)
		if len(params) > 0 {
			childScope[params[0]] = item
		}
		if len(params) > 1 {
			childScope[params[1]] = internNum(float64(i))
		}

		bt := make([]tok, len(bodyTokens))
		copy(bt, bodyTokens)
		childEval := &evaluator{tokens: bt, pos: 0, scope: childScope}
		result := childEval.evalStatements()
		if result == nil {
			result = Undefined
		}
		putScope(childScope)

		if method == "filter" {
			if result.truthy() {
				results = append(results, item)
			}
		} else {
			results = append(results, result)
		}
	}

	return newArr(results)
}

// captureArrowCallback parses an arrow function callback inside a method call
// (e.g. .find(p => ...) ) and returns the param name and prepared body tokens.
// Caller must have already consumed the opening ( of the method call.
func (e *evaluator) captureArrowCallback() (paramName string, bodyTokens []tok) {
	params := e.parseArrowParams()
	paramName = "item"
	if len(params) > 0 {
		paramName = params[0]
	}
	e.expect(tokArrow)

	// Capture body tokens until the closing ) of the method call
	bodyStart := e.pos
	hasBodyParen := e.peek().t == tokLParen
	hasBodyBrace := e.peek().t == tokLBrace
	if hasBodyParen {
		e.skipBalanced(tokLParen, tokRParen)
	} else if hasBodyBrace {
		e.skipBalanced(tokLBrace, tokRBrace)
	} else {
		depth := 1
		for e.pos < len(e.tokens) {
			if e.tokens[e.pos].t == tokLParen {
				depth++
			} else if e.tokens[e.pos].t == tokRParen {
				depth--
				if depth == 0 {
					break
				}
			}
			e.pos++
		}
	}
	bodyEnd := e.pos
	e.expect(tokRParen) // close method call

	bodyTokens = make([]tok, bodyEnd-bodyStart)
	copy(bodyTokens, e.tokens[bodyStart:bodyEnd])

	if hasBodyParen && len(bodyTokens) >= 2 && bodyTokens[0].t == tokLParen {
		bodyTokens = bodyTokens[1 : len(bodyTokens)-1]
	}
	if hasBodyBrace && len(bodyTokens) >= 2 && bodyTokens[0].t == tokLBrace {
		bodyTokens = extractReturnFromBlock(bodyTokens)
	}
	bodyTokens = append(bodyTokens, tok{t: tokEOF})
	return
}

// evalFind handles array.find(item => condition)
func (e *evaluator) evalFind(val *Value) *Value {
	if val.typ != TypeArray {
		e.skipBalanced(tokLParen, tokRParen)
		return Undefined
	}

	paramName, bodyTokens := e.captureArrowCallback()

	for _, item := range val.array {
		childScope := getScope(e.scope)
		childScope[paramName] = item
		childEval := &evaluator{tokens: bodyTokens, pos: 0, scope: childScope}
		result := childEval.expr()
		putScope(childScope)
		if result.truthy() {
			return item
		}
	}
	return Undefined
}

// evalFindIndex handles array.findIndex(item => condition)
func (e *evaluator) evalFindIndex(val *Value) *Value {
	if val.typ != TypeArray {
		e.skipBalanced(tokLParen, tokRParen)
		return internNum(-1)
	}

	paramName, bodyTokens := e.captureArrowCallback()

	for i, item := range val.array {
		childScope := getScope(e.scope)
		childScope[paramName] = item
		childEval := &evaluator{tokens: bodyTokens, pos: 0, scope: childScope}
		result := childEval.expr()
		putScope(childScope)
		if result.truthy() {
			return internNum(float64(i))
		}
	}
	return internNum(-1)
}

// evalSomeEvery handles array.some/every(item => condition)
func (e *evaluator) evalSomeEvery(val *Value, method string) *Value {
	if val.typ != TypeArray {
		e.skipBalanced(tokLParen, tokRParen)
		if method == "every" {
			return True
		}
		return False
	}

	paramName, bodyTokens := e.captureArrowCallback()

	for _, item := range val.array {
		childScope := getScope(e.scope)
		childScope[paramName] = item
		childEval := &evaluator{tokens: bodyTokens, pos: 0, scope: childScope}
		result := childEval.expr()
		putScope(childScope)
		if method == "some" && result.truthy() {
			return True
		}
		if method == "every" && !result.truthy() {
			return False
		}
	}
	if method == "some" {
		return False
	}
	return True
}

// evalReduce handles array.reduce((acc, item) => expr, initialValue)
func (e *evaluator) evalReduce(val *Value) *Value {
	if val.typ != TypeArray {
		for e.peek().t != tokRParen && e.peek().t != tokEOF {
			e.advance()
		}
		e.expect(tokRParen)
		return Undefined
	}

	// Parse arrow params: (acc, item) or (acc, item, index)
	params := e.parseArrowParams()
	e.expect(tokArrow)

	// Capture body tokens — read until comma at depth 0 (before initialValue) or )
	bodyStart := e.pos
	hasBodyParen := e.peek().t == tokLParen
	hasBodyBrace := e.peek().t == tokLBrace
	if hasBodyParen {
		e.skipBalanced(tokLParen, tokRParen)
	} else if hasBodyBrace {
		e.skipBalanced(tokLBrace, tokRBrace)
	} else {
		depth := 0
		for e.pos < len(e.tokens) {
			t := e.tokens[e.pos]
			if t.t == tokLParen {
				depth++
			} else if t.t == tokRParen {
				if depth == 0 {
					break
				}
				depth--
			} else if t.t == tokComma && depth == 0 {
				break
			}
			e.pos++
		}
	}
	bodyEnd := e.pos

	// Parse initial value if present
	var accumulator *Value
	if e.peek().t == tokComma {
		e.advance()
		accumulator = e.expr()
	}
	e.expect(tokRParen)

	bodyTokens := make([]tok, bodyEnd-bodyStart)
	copy(bodyTokens, e.tokens[bodyStart:bodyEnd])
	if hasBodyParen && len(bodyTokens) >= 2 && bodyTokens[0].t == tokLParen {
		bodyTokens = bodyTokens[1 : len(bodyTokens)-1]
	}
	if hasBodyBrace && len(bodyTokens) >= 2 && bodyTokens[0].t == tokLBrace {
		bodyTokens = extractReturnFromBlock(bodyTokens)
	}
	bodyTokens = append(bodyTokens, tok{t: tokEOF})

	arr := val.array
	startIdx := 0
	if accumulator == nil {
		if len(arr) == 0 {
			return Undefined
		}
		accumulator = arr[0]
		startIdx = 1
	}

	accParam := "acc"
	itemParam := "item"
	if len(params) > 0 {
		accParam = params[0]
	}
	if len(params) > 1 {
		itemParam = params[1]
	}

	// Fast path: reuse scope for simple expression bodies
	if !hasBodyBrace {
		savedAcc, hasAcc := e.scope[accParam]
		savedItem, hasItem := e.scope[itemParam]
		ev := &evaluator{tokens: bodyTokens, pos: 0, scope: e.scope}
		for i := startIdx; i < len(arr); i++ {
			e.scope[accParam] = accumulator
			e.scope[itemParam] = arr[i]
			if len(params) > 2 {
				e.scope[params[2]] = internNum(float64(i))
			}
			ev.pos = 0
			accumulator = ev.expr()
		}
		if hasAcc {
			e.scope[accParam] = savedAcc
		} else {
			delete(e.scope, accParam)
		}
		if hasItem {
			e.scope[itemParam] = savedItem
		} else {
			delete(e.scope, itemParam)
		}
	} else {
		for i := startIdx; i < len(arr); i++ {
			childScope := getScope(e.scope)
			childScope[accParam] = accumulator
			childScope[itemParam] = arr[i]
			if len(params) > 2 {
				childScope[params[2]] = internNum(float64(i))
			}
			childEval := &evaluator{tokens: bodyTokens, pos: 0, scope: childScope}
			accumulator = childEval.expr()
			putScope(childScope)
		}
	}

	return accumulator
}

func sortValues(arr []*Value) {
	// Simple insertion sort by string representation
	for i := 1; i < len(arr); i++ {
		key := arr[i]
		keyStr := key.toStr()
		j := i - 1
		for j >= 0 && arr[j].toStr() > keyStr {
			arr[j+1] = arr[j]
			j--
		}
		arr[j+1] = key
	}
}

func flattenArray(arr []*Value, depth int) []*Value {
	if depth <= 0 {
		return arr
	}
	var result []*Value
	for _, item := range arr {
		if item.typ == TypeArray {
			result = append(result, flattenArray(item.array, depth-1)...)
		} else {
			result = append(result, item)
		}
	}
	return result
}

func extractReturnFromBlock(tokens []tok) []tok {
	// Strip outer { }
	if len(tokens) < 2 {
		return tokens
	}
	inner := tokens[1 : len(tokens)-1]
	// Find "return" and take everything after it until ; or end
	for i, t := range inner {
		if t.t == tokIdent && t.v == "return" {
			rest := inner[i+1:]
			// Strip trailing ;
			if len(rest) > 0 && rest[len(rest)-1].t == tokSemi {
				rest = rest[:len(rest)-1]
			}
			return rest
		}
	}
	return inner
}

func (e *evaluator) parseArrowParams() []string {
	var params []string
	if e.peek().t == tokLParen {
		e.advance() // skip (
		for e.peek().t != tokRParen && e.peek().t != tokEOF {
			if e.peek().t == tokIdent {
				params = append(params, e.advance().v)
			} else if e.peek().t == tokLBrack {
				// Array destructuring: ([a, b, c]) => ...
				// Store as a special "__destructure__:a,b,c" param
				e.advance() // skip [
				var names []string
				for e.peek().t != tokRBrack && e.peek().t != tokEOF {
					if e.peek().t == tokIdent {
						names = append(names, e.advance().v)
					}
					if e.peek().t == tokComma {
						e.advance()
					}
				}
				if e.peek().t == tokRBrack {
					e.advance()
				}
				params = append(params, "__destructure__:"+strings.Join(names, ","))
			} else {
				e.advance() // skip unknown token
			}
			// skip default values like = 0
			if e.peek().t == tokAssign {
				e.advance()
				e.expr()
			}
			if e.peek().t == tokComma {
				e.advance()
			}
		}
		e.advance() // skip )
	} else if e.peek().t == tokIdent {
		params = append(params, e.advance().v)
	}
	return params
}

func (e *evaluator) skipBalanced(open, close tokType) {
	depth := 1
	e.advance() // skip opening
	for depth > 0 && e.pos < len(e.tokens) {
		t := e.advance()
		if t.t == open {
			depth++
		} else if t.t == close {
			depth--
		}
	}
}

// evalSingleStatement handles a single statement (like "return expr;") without braces.
func (e *evaluator) evalSingleStatement() *Value {
	if e.peek().t == tokIdent && e.peek().v == "return" {
		e.advance() // skip "return"
		val := e.expr()
		// Skip optional semicolon
		if e.peek().t == tokSemi {
			e.advance()
		}
		return val
	}
	// Not a return — evaluate and discard
	e.expr()
	if e.peek().t == tokSemi {
		e.advance()
	}
	return nil
}

// skipSingleStatement skips a single statement without braces (e.g., "return expr;").
func (e *evaluator) skipSingleStatement() {
	depth := 0
	first := true
	for e.peek().t != tokEOF {
		t := e.peek()
		if t.t == tokLParen || t.t == tokLBrace || t.t == tokLBrack {
			depth++
		} else if t.t == tokRParen || t.t == tokRBrace || t.t == tokRBrack {
			depth--
		}
		if depth <= 0 && t.t == tokSemi {
			e.advance() // consume semicolon
			return
		}
		// After the first token, stop at statement keywords (don't consume)
		if !first && depth <= 0 && t.t == tokIdent && (t.v == "return" || t.v == "const" || t.v == "let" || t.v == "var" || t.v == "if" || t.v == "else" || t.v == "for") {
			return
		}
		first = false
		e.advance()
	}
}

func (e *evaluator) primary() *Value {
	t := e.peek()

	switch t.t {
	case tokStr:
		e.advance()
		return newStr(t.v)

	case tokTemplatePart:
		// Template literal with ${} interpolation — raw content stored in token
		raw := e.advance().v
		var sb strings.Builder
		i := 0
		for i < len(raw) {
			if i+1 < len(raw) && raw[i] == '$' && raw[i+1] == '{' {
				i += 2
				// Find matching }
				depth := 1
				start := i
				for i < len(raw) && depth > 0 {
					if raw[i] == '{' { depth++ } else if raw[i] == '}' { depth-- }
					if depth > 0 { i++ }
				}
				exprStr := raw[start:i]
				if i < len(raw) { i++ } // skip }
				// Evaluate the expression
				exprTokens := tokenizeCached(exprStr)
				exprEv := &evaluator{tokens: exprTokens, pos: 0, scope: e.scope}
				val := exprEv.expr()
				sb.WriteString(val.toStr())
			} else {
				sb.WriteByte(raw[i])
				i++
			}
		}
		return newStr(sb.String())

	case tokNum:
		e.advance()
		return newNum(t.n)

	case tokLParen:
		// Check if this is an arrow function: () => ... or (x) => ... or (x, y) => ...
		if e.isArrowFunction() {
			return e.parseArrowFunction()
		}
		e.advance()
		val := e.expr()
		e.expect(tokRParen)
		return val

	case tokLBrack:
		return e.parseArray()

	case tokLBrace:
		return e.parseObject()

	case tokIdent:
		switch t.v {
		case "new":
			e.advance()
			ctor := e.advance().v

			// new Intl.NumberFormat(locale, options) — handle before generic constructor
			if ctor == "Intl" && e.peek().t == tokDot {
				e.advance() // skip .
				subCtor := e.advance().v

				if subCtor == "NumberFormat" && e.peek().t == tokLParen {
					e.advance() // skip (
					// Parse locale (skip it)
					locale := ""
					if e.peek().t == tokStr {
						locale = e.advance().v
						_ = locale
					}
					// Parse options object
					var currency string
					style := ""
					minFrac := -1
					maxFrac := -1
					if e.peek().t == tokComma {
						e.advance()
						if e.peek().t == tokLBrace {
							opts := e.expr()
							if opts.typ == TypeObject && opts.object != nil {
								if s, ok := opts.object["style"]; ok {
									style = s.toStr()
								}
								if c, ok := opts.object["currency"]; ok {
									currency = c.toStr()
								}
								if v, ok := opts.object["minimumFractionDigits"]; ok {
									minFrac = int(v.toNum())
								}
								if v, ok := opts.object["maximumFractionDigits"]; ok {
									maxFrac = int(v.toNum())
								}
							}
						}
					}
					e.expect(tokRParen)

					// Return an object with a .format() method
					fmtStyle := style
					fmtCurrency := currency
					fmtMinFrac := minFrac
					fmtMaxFrac := maxFrac
					formatFn := NewNativeFunc(func(args []*Value) *Value {
						if len(args) == 0 {
							return internStr("")
						}
						n := args[0].toNum()
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
			}
			// Generic new Constructor(...) — skip args
			if e.peek().t == tokLParen {
				e.advance()
				for e.peek().t != tokRParen && e.peek().t != tokEOF {
					e.expr()
					if e.peek().t == tokComma {
						e.advance()
					}
				}
				e.expect(tokRParen)
			}
			if ctor == "Date" {
				noopStr := func(s string) *Value {
						id := registerArrow(&arrowFunc{
							tokens: append(tokenize(`"`+s+`"`), tok{t: tokEOF}),
							scope:  make(map[string]*Value),
						})
						return &Value{typ: TypeFunc, str: "__arrow", num: float64(id)}
					}
				noopNum := func(n float64) *Value {
						id := registerArrow(&arrowFunc{
							tokens: append(tokenize(strconv.FormatFloat(n, 'f', -1, 64)), tok{t: tokEOF}),
							scope:  make(map[string]*Value),
						})
						return &Value{typ: TypeFunc, str: "__arrow", num: float64(id)}
					}
				return newObj(map[string]*Value{
					"toLocaleTimeString": noopStr("00:00:00"),
					"toLocaleDateString": noopStr("1/1/2026"),
					"toISOString":        noopStr("2026-01-01T00:00:00.000Z"),
					"toString":           noopStr("Thu Jan 01 2026"),
					"getTime":            noopNum(0),
					"getFullYear":        noopNum(2026),
					"getMonth":           noopNum(0),
					"getDate":            noopNum(1),
					"getHours":           noopNum(0),
					"getMinutes":         noopNum(0),
					"getSeconds":         noopNum(0),
				})
			}
			return Undefined
		case "true":
			e.advance()
			return True
		case "false":
			e.advance()
			return False
		case "null":
			e.advance()
			return Null
		case "undefined":
			e.advance()
			return Undefined
		case "Array":
			e.advance()
			if e.peek().t == tokDot {
				e.advance()
				method := e.advance()
				if e.peek().t == tokLParen {
					e.advance()
					arg := e.expr()
					e.expect(tokRParen)
					switch method.v {
					case "isArray":
						return newBool(arg.typ == TypeArray)
					case "from":
						if arg.typ == TypeArray { return arg }
						if arg.typ == TypeString {
							arr := make([]*Value, len(arg.str))
							for i, ch := range arg.str { arr[i] = newStr(string(ch)) }
							return newArr(arr)
						}
						return newArr(nil)
					}
				}
			}
			return Undefined
		case "Object":
			e.advance()
			if e.peek().t == tokDot {
				e.advance()
				method := e.advance()
				if e.peek().t == tokLParen {
					e.advance()
					arg := e.expr()
					// Check for second arg (Object.assign)
					var extraArgs []*Value
					for e.peek().t == tokComma {
						e.advance()
						extraArgs = append(extraArgs, e.expr())
					}
					e.expect(tokRParen)
					switch method.v {
					case "keys":
						if arg.typ == TypeObject && arg.object != nil {
							keys := make([]*Value, 0, len(arg.object))
							for k := range arg.object {
								keys = append(keys, newStr(k))
							}
							return newArr(keys)
						}
						return newArr(nil)
					case "values":
						if arg.typ == TypeObject && arg.object != nil {
							vals := make([]*Value, 0, len(arg.object))
							for _, v := range arg.object {
								vals = append(vals, v)
							}
							return newArr(vals)
						}
						return newArr(nil)
					case "entries":
						if arg.typ == TypeObject && arg.object != nil {
							entries := make([]*Value, 0, len(arg.object))
							for k, v := range arg.object {
								entries = append(entries, newArr([]*Value{newStr(k), v}))
							}
							return newArr(entries)
						}
						return newArr(nil)
					case "assign":
						target := arg
						if target.typ != TypeObject || target.object == nil {
							target = &Value{typ: TypeObject, object: make(map[string]*Value)}
						}
						for _, src := range extraArgs {
							if src.typ == TypeObject && src.object != nil {
								for k, v := range src.object {
									target.object[k] = v
								}
							}
						}
						return target
					case "freeze":
						return arg // no-op
					}
				}
			}
			return Undefined
		case "JSON":
			e.advance()
			if e.peek().t == tokDot {
				e.advance()
				method := e.advance()
				if e.peek().t == tokLParen {
					e.advance()
					arg := e.expr()
					e.expect(tokRParen)
					switch method.v {
					case "stringify":
						b, _ := json.Marshal(valueToInterface(arg))
						return newStr(string(b))
					case "parse":
						return jsonToValue(json.RawMessage(arg.toStr()))
					}
				}
			}
			return Undefined
		case "Number":
			e.advance()
			if e.peek().t == tokLParen {
				e.advance()
				arg := e.expr()
				e.expect(tokRParen)
				return newNum(arg.toNum())
			}
			return &Value{typ: TypeFunc, str: "Number"}
		case "parseInt":
			e.advance()
			if e.peek().t == tokLParen {
				e.advance()
				arg := e.expr()
				// Optional radix
				if e.peek().t == tokComma { e.advance(); e.expr() }
				e.expect(tokRParen)
				n, err := strconv.ParseInt(strings.TrimSpace(arg.toStr()), 10, 64)
				if err != nil { return internNum(0) }
				return newNum(float64(n))
			}
			return &Value{typ: TypeFunc, str: "parseInt"}
		case "parseFloat":
			e.advance()
			if e.peek().t == tokLParen {
				e.advance()
				arg := e.expr()
				e.expect(tokRParen)
				n, err := strconv.ParseFloat(strings.TrimSpace(arg.toStr()), 64)
				if err != nil { return internNum(0) }
				return newNum(n)
			}
			return &Value{typ: TypeFunc, str: "parseFloat"}
		case "Boolean":
			e.advance()
			if e.peek().t == tokLParen {
				e.advance()
				arg := e.expr()
				e.expect(tokRParen)
				return newBool(arg.truthy())
			}
			return &Value{typ: TypeFunc, str: "Boolean"}
		case "console":
			e.advance()
			if e.peek().t == tokDot {
				e.advance()
				method := e.advance().v
				if e.peek().t == tokLParen {
					e.advance() // skip (
					var parts []string
					for e.peek().t != tokRParen && e.peek().t != tokEOF {
						val := e.expr()
						parts = append(parts, val.toStr())
						if e.peek().t == tokComma {
							e.advance()
						}
					}
					e.expect(tokRParen)
					line := strings.Join(parts, " ")
					switch method {
					case "warn", "error":
						fmt.Fprintln(os.Stderr, line)
					default:
						fmt.Println(line)
					}
				}
			}
			return Undefined
		case "Math":
			e.advance()
			if e.peek().t == tokDot {
				e.advance()
				method := e.advance().v
				if e.peek().t == tokLParen {
					e.advance()
					arg := e.expr()
					n := arg.toNum()
					switch method {
					case "floor":
						e.expect(tokRParen)
						return newNum(float64(int64(n)))
					case "ceil":
						e.expect(tokRParen)
						if n == float64(int64(n)) {
							return newNum(n)
						}
						return newNum(float64(int64(n) + 1))
					case "round":
						e.expect(tokRParen)
						return newNum(float64(int64(n + 0.5)))
					case "abs":
						e.expect(tokRParen)
						if n < 0 {
							return newNum(-n)
						}
						return newNum(n)
					case "min":
						if e.peek().t == tokComma {
							e.advance()
							b := e.expr().toNum()
							e.expect(tokRParen)
							if n < b {
								return newNum(n)
							}
							return newNum(b)
						}
						e.expect(tokRParen)
						return newNum(n)
					case "max":
						if e.peek().t == tokComma {
							e.advance()
							b := e.expr().toNum()
							e.expect(tokRParen)
							if n > b {
								return newNum(n)
							}
							return newNum(b)
						}
						e.expect(tokRParen)
						return newNum(n)
					case "random":
						e.expect(tokRParen)
						return internNum(0)
					}
				}
			}
			return Undefined
		case "String":
			e.advance()
			if e.peek().t == tokLParen {
				e.advance()
				arg := e.expr()
				e.expect(tokRParen)
				return newStr(arg.toStr())
			}
			return Undefined
		default:
			e.advance()
			// Look up in scope (cached)
			if val, ok := e.getVar(t.v); ok {
				return val
			}
			return Undefined
		}

	default:
		e.advance()
		return Undefined
	}
}

func (e *evaluator) callFunc(fn *Value, props map[string]*Value) *Value {
	if fn.typ != TypeFunc {
		return Undefined
	}

	// Try bytecode path — compile on first call, cache on the Value
	if fn.bc == nil && fn.fnBody != "" && !(len(fn.fnParams) == 1 && strings.HasPrefix(fn.fnParams[0], "{")) {
		bc := compileFuncBody(fn.fnBody)
		if bc != nil {
			// Cache param names on the bytecode for fast recursive calls
			if len(fn.fnParams) > 0 {
				bc.params = splitParams(fn.fnParams[0])
			}
			fn.bc = bc
		}
	}

	if fn.bc != nil {
		// Bytecode fast path
		childScope := make(map[string]*Value, len(e.scope)+len(props))
		for k, v := range e.scope {
			childScope[k] = v
		}
		for k, v := range props {
			childScope[k] = v
		}
		return execBytecode(fn.bc, childScope)
	}

	// Interpreter fallback
	childScope := make(map[string]*Value, len(e.scope)+len(props))
	for k, v := range e.scope {
		childScope[k] = v
	}
	if len(fn.fnParams) == 1 && strings.HasPrefix(fn.fnParams[0], "{") {
		for k, v := range props {
			childScope[k] = v
		}
		paramStr := fn.fnParams[0]
		paramStr = strings.TrimPrefix(paramStr, "{")
		paramStr = strings.TrimSuffix(strings.TrimSpace(paramStr), "}")
		for _, part := range strings.Split(paramStr, ",") {
			part = strings.TrimSpace(part)
			if eqIdx := strings.Index(part, "="); eqIdx > 0 {
				name := strings.TrimSpace(part[:eqIdx])
				if _, exists := childScope[name]; !exists {
					defaultStr := strings.TrimSpace(part[eqIdx+1:])
					defaultVal := evalExpr(defaultStr, childScope)
					childScope[name] = defaultVal
				}
			}
		}
	} else if len(fn.fnParams) > 0 {
		for k, v := range props {
			childScope[k] = v
		}
	}

	bodyTokens := tokenizeCached(fn.fnBody)
	childEval := &evaluator{tokens: bodyTokens, pos: 0, scope: childScope}
	result := childEval.evalStatements()
	if result == nil {
		return Undefined
	}
	return result
}

func (e *evaluator) evalStatements() *Value {
	for e.peek().t != tokEOF {
		t := e.peek()

		// const/let/var declaration
		if t.t == tokIdent && (t.v == "const" || t.v == "let" || t.v == "var") {
			e.advance()

			// Array destructuring: const [a, b] = expr
			if e.peek().t == tokLBrack {
				e.advance() // skip [
				var names []string
				for e.peek().t != tokRBrack && e.peek().t != tokEOF {
					if e.peek().t == tokIdent {
						names = append(names, e.advance().v)
					} else {
						e.advance() // skip commas etc
					}
				}
				e.expect(tokRBrack)
				e.expect(tokAssign)
				val := e.expr()
				// Destructure array
				if val.typ == TypeArray {
					for i, name := range names {
						if i < len(val.array) {
							e.scope[name] = val.array[i]
						} else {
							e.scope[name] = Undefined
						}
					}
				} else {
					for _, name := range names {
						e.scope[name] = Undefined
					}
				}
				if e.peek().t == tokSemi {
					e.advance()
				}
				continue
			}

			// Object destructuring: const { a, b } = expr
			if e.peek().t == tokLBrace {
				e.advance() // skip {
				var names []string
				for e.peek().t != tokRBrace && e.peek().t != tokEOF {
					if e.peek().t == tokIdent {
						names = append(names, e.advance().v)
					}
					if e.peek().t == tokComma {
						e.advance()
					}
				}
				e.expect(tokRBrace)
				e.expect(tokAssign)
				val := e.expr()
				if val.typ == TypeObject && val.object != nil {
					for _, name := range names {
						if v, ok := val.object[name]; ok {
							e.scope[name] = v
						} else {
							e.scope[name] = Undefined
						}
					}
				}
				if e.peek().t == tokSemi {
					e.advance()
				}
				continue
			}

			// Simple: const name = expr
			name := e.advance().v
			e.expect(tokAssign)
			val := e.expr()
			e.scope[name] = val
			if e.peek().t == tokSemi {
				e.advance()
			}
			continue
		}

		// return statement
		if t.t == tokIdent && t.v == "return" {
			e.advance()
			return e.expr()
		}

		// if statement
		if t.t == tokIdent && t.v == "if" {
			e.advance() // skip "if"
			e.expect(tokLParen)
			cond := e.expr()
			e.expect(tokRParen)

			if cond.truthy() {
				// Execute the if block
				if e.peek().t == tokLBrace {
					e.advance() // skip {
					result := e.evalBlock()
					if result != nil {
						return result // block had a return
					}
				} else if e.peek().t == tokIdent && e.peek().v == "return" {
					// Single-statement if: if (cond) return expr;
					result := e.evalSingleStatement()
					if result != nil {
						return result
					}
				}
				// Skip else if present
				if e.peek().t == tokIdent && e.peek().v == "else" {
					e.advance()
					if e.peek().t == tokLBrace {
						e.skipBalanced(tokLBrace, tokRBrace)
					} else if e.peek().t == tokIdent && e.peek().v == "if" {
						e.skipIfChain()
					} else {
						// else single-statement — skip it
						e.skipSingleStatement()
					}
				}
			} else {
				// Skip the if block
				if e.peek().t == tokLBrace {
					e.skipBalanced(tokLBrace, tokRBrace)
				} else {
					// Skip single-statement if body
					e.skipSingleStatement()
				}
				// Handle else
				if e.peek().t == tokIdent && e.peek().v == "else" {
					e.advance()
					if e.peek().t == tokIdent && e.peek().v == "if" {
						continue // loop will pick up "if" next iteration
					}
					if e.peek().t == tokLBrace {
						e.advance() // skip {
						result := e.evalBlock()
						if result != nil {
							return result
						}
					} else if e.peek().t == tokIdent && e.peek().v == "return" {
						result := e.evalSingleStatement()
						if result != nil {
							return result
						}
					}
				}
			}
			continue
		}

		// for statement
		if t.t == tokIdent && t.v == "for" {
			e.advance() // skip "for"
			e.expect(tokLParen)
			// Check for for...of: for (const x of arr)
			if e.peek().t == tokIdent && (e.peek().v == "const" || e.peek().v == "let" || e.peek().v == "var") {
				e.advance() // skip const/let/var
				varName := e.advance().v // variable name
				if e.peek().t == tokIdent && e.peek().v == "of" {
					e.advance() // skip "of"
					arr := e.expr()
					e.expect(tokRParen)
					// Capture body, then execute for each item
					if e.peek().t == tokLBrace {
						bodyStart := e.pos
						e.skipBalanced(tokLBrace, tokRBrace)
						bodyEnd := e.pos
						if arr.typ == TypeArray {
							rawBody := e.tokens[bodyStart:bodyEnd]
							var bodyTokens []tok
							if len(rawBody) >= 2 && rawBody[0].t == tokLBrace {
								bodyTokens = make([]tok, len(rawBody)-2+1)
								copy(bodyTokens, rawBody[1:len(rawBody)-1])
							} else {
								bodyTokens = make([]tok, len(rawBody)+1)
								copy(bodyTokens, rawBody)
							}
							bodyTokens[len(bodyTokens)-1] = tok{t: tokEOF}
							bodyEv := &evaluator{tokens: bodyTokens, pos: 0, scope: e.scope, useCache: true}
							for _, item := range arr.array {
								e.scope[varName] = item
								bodyEv.pos = 0; bodyEv.clearCache()
								result := bodyEv.evalStatements()
								if result != nil { return result }
							}
						}
					}
					continue
				}
				// Regular for (let i = 0; ...; ...)
				// Init: already consumed const/let/var and name
				e.expect(tokAssign)
				initVal := e.expr()
				e.scope[varName] = initVal
			} else {
				// for (; ...; ...) or for (expr; ...; ...)
				if e.peek().t != tokSemi {
					e.expr()
				}
			}
			e.expect(tokSemi)
			// Capture condition tokens (skip without evaluating)
			condStart := e.pos
			if e.peek().t != tokSemi {
				depth := 0
				for e.pos < len(e.tokens) {
					tk := e.tokens[e.pos]
					if tk.t == tokLParen { depth++ } else if tk.t == tokRParen { depth-- }
					if tk.t == tokSemi && depth <= 0 { break }
					e.pos++
				}
			}
			condEnd := e.pos
			e.expect(tokSemi)
			// Capture update tokens (skip without evaluating — just find the range)
			updateStart := e.pos
			if e.peek().t != tokRParen {
				depth := 0
				for e.pos < len(e.tokens) {
					tk := e.tokens[e.pos]
					if tk.t == tokLParen { depth++ } else if tk.t == tokRParen { if depth == 0 { break }; depth-- }
					e.pos++
				}
			}
			updateEnd := e.pos
			e.expect(tokRParen)
			// Capture body
			if e.peek().t == tokLBrace {
				bodyStart := e.pos
				e.skipBalanced(tokLBrace, tokRBrace)
				bodyEnd := e.pos

				// Prepare tokens ONCE outside the loop
				condTokens := make([]tok, condEnd-condStart+1)
				copy(condTokens, e.tokens[condStart:condEnd])
				condTokens[len(condTokens)-1] = tok{t: tokEOF}

				rawBody := e.tokens[bodyStart:bodyEnd]
				var bodyTokens []tok
				if len(rawBody) >= 2 && rawBody[0].t == tokLBrace {
					bodyTokens = make([]tok, len(rawBody)-2+1)
					copy(bodyTokens, rawBody[1:len(rawBody)-1])
				} else {
					bodyTokens = make([]tok, len(rawBody)+1)
					copy(bodyTokens, rawBody)
				}
				bodyTokens[len(bodyTokens)-1] = tok{t: tokEOF}

				updateTokens := make([]tok, updateEnd-updateStart+1)
				copy(updateTokens, e.tokens[updateStart:updateEnd])
				updateTokens[len(updateTokens)-1] = tok{t: tokEOF}

				// Detect simple i++ / i-- update
				isSimpleIncr := false
				var incrVar string
				var incrDelta float64
				if len(updateTokens) == 3 && updateTokens[0].t == tokIdent && updateTokens[2].t == tokEOF {
					if updateTokens[1].t == tokPlusPlus {
						isSimpleIncr = true
						incrVar = updateTokens[0].v
						incrDelta = 1
					} else if updateTokens[1].t == tokMinusMinus {
						isSimpleIncr = true
						incrVar = updateTokens[0].v
						incrDelta = -1
					}
				}

				// Native for-loop fast path:
				// Detect pattern: IDENT < NUM or IDENT < IDENT (with i++ update)
				// Execute as a Go for loop — no condition evaluation, no map lookup for counter
				nativeLoop := false
				if isSimpleIncr && len(condTokens) == 4 { // [IDENT, OP, NUM/IDENT, EOF]
					ct0 := condTokens[0]
					ct1 := condTokens[1]
					ct2 := condTokens[2]
					if ct0.t == tokIdent && ct0.v == incrVar &&
						(ct1.t == tokLt || ct1.t == tokLtEq || ct1.t == tokGt || ct1.t == tokGtEq) {
						var limit float64
						limitOk := false
						if ct2.t == tokNum {
							limit = ct2.n
							limitOk = true
						} else if ct2.t == tokIdent {
							if lv, ok := e.scope[ct2.v]; ok && lv.typ == TypeNumber {
								limit = lv.num
								limitOk = true
							}
						}
						if limitOk {
							nativeLoop = true
							counter := e.scope[incrVar].toNum()
							bodyEv := &evaluator{tokens: bodyTokens, pos: 0, scope: e.scope, useCache: true}
							checkCond := func(c float64) bool {
								switch ct1.t {
								case tokLt:
									return c < limit
								case tokLtEq:
									return c <= limit
								case tokGt:
									return c > limit
								case tokGtEq:
									return c >= limit
								}
								return false
							}
							for iter := 0; iter < 100000 && checkCond(counter); iter++ {
								e.scope[incrVar] = internNum(counter)
								bodyEv.pos = 0; bodyEv.clearCache()
								result := bodyEv.evalStatements()
								if result == breakSentinel { break }
								if result != nil && result != continueSentinel { return result }
								counter += incrDelta
							}
							e.scope[incrVar] = internNum(counter)
						}
					}
				}

				// General loop path
				if !nativeLoop {
					condEv := &evaluator{tokens: condTokens, pos: 0, scope: e.scope}
					bodyEv := &evaluator{tokens: bodyTokens, pos: 0, scope: e.scope, useCache: true}
					updateEv := &evaluator{tokens: updateTokens, pos: 0, scope: e.scope}

					for iter := 0; iter < 100000; iter++ {
						condEv.pos = 0
						if !condEv.expr().truthy() {
							break
						}
						bodyEv.pos = 0; bodyEv.clearCache()
						result := bodyEv.evalStatements()
						if result == breakSentinel { break }
						if result != nil && result != continueSentinel { return result }
						if isSimpleIncr {
							if v, ok := e.scope[incrVar]; ok {
								e.scope[incrVar] = internNum(v.toNum() + incrDelta)
							}
						} else {
							updateEv.pos = 0
							updateEv.expr()
						}
					}
				}
			}
			continue
		}

		// while statement
		if t.t == tokIdent && t.v == "while" {
			e.advance() // skip "while"
			e.expect(tokLParen)
			condStart := e.pos
			e.expr()
			condEnd := e.pos
			e.expect(tokRParen)
			if e.peek().t == tokLBrace {
				bodyStart := e.pos
				e.skipBalanced(tokLBrace, tokRBrace)
				bodyEnd := e.pos
				condTokens := make([]tok, condEnd-condStart+1)
				copy(condTokens, e.tokens[condStart:condEnd])
				condTokens[len(condTokens)-1] = tok{t: tokEOF}

				rawBody := e.tokens[bodyStart:bodyEnd]
				var bodyTokens []tok
				if len(rawBody) >= 2 && rawBody[0].t == tokLBrace {
					bodyTokens = make([]tok, len(rawBody)-2+1)
					copy(bodyTokens, rawBody[1:len(rawBody)-1])
				} else {
					bodyTokens = make([]tok, len(rawBody)+1)
					copy(bodyTokens, rawBody)
				}
				bodyTokens[len(bodyTokens)-1] = tok{t: tokEOF}

				condEv := &evaluator{tokens: condTokens, pos: 0, scope: e.scope}
				bodyEv := &evaluator{tokens: bodyTokens, pos: 0, scope: e.scope, useCache: true}
				for iter := 0; iter < 100000; iter++ {
					condEv.pos = 0
					if !condEv.expr().truthy() {
						break
					}
					bodyEv.pos = 0; bodyEv.clearCache()
					result := bodyEv.evalStatements()
					if result == breakSentinel { break }
					if result == continueSentinel { continue }
					if result != nil { return result }
				}
			}
			continue
		}

		// try/catch — execute try block, ignore errors
		if t.t == tokIdent && t.v == "try" {
			e.advance() // skip "try"
			if e.peek().t == tokLBrace {
				e.advance() // skip {
				result := e.evalBlock()
				if result != nil {
					// Skip catch/finally
					if e.peek().t == tokIdent && e.peek().v == "catch" {
						e.advance()
						if e.peek().t == tokLParen { e.skipBalanced(tokLParen, tokRParen) }
						if e.peek().t == tokLBrace { e.skipBalanced(tokLBrace, tokRBrace) }
					}
					if e.peek().t == tokIdent && e.peek().v == "finally" {
						e.advance()
						if e.peek().t == tokLBrace { e.advance(); e.evalBlock() }
					}
					return result
				}
			}
			// Skip catch
			if e.peek().t == tokIdent && e.peek().v == "catch" {
				e.advance()
				if e.peek().t == tokLParen { e.skipBalanced(tokLParen, tokRParen) }
				if e.peek().t == tokLBrace { e.skipBalanced(tokLBrace, tokRBrace) }
			}
			// Execute finally if present
			if e.peek().t == tokIdent && e.peek().v == "finally" {
				e.advance()
				if e.peek().t == tokLBrace { e.advance(); e.evalBlock() }
			}
			continue
		}

		// break/continue
		if t.t == tokIdent && t.v == "break" {
			e.advance()
			if e.peek().t == tokSemi { e.advance() }
			return breakSentinel
		}
		if t.t == tokIdent && t.v == "continue" {
			e.advance()
			if e.peek().t == tokSemi { e.advance() }
			return continueSentinel
		}

		// switch statement
		if t.t == tokIdent && t.v == "switch" {
			e.advance()
			e.expect(tokLParen)
			switchVal := e.expr()
			e.expect(tokRParen)
			e.expect(tokLBrace)
			matched := false
			fallThru := false
			done := false
			for e.peek().t != tokRBrace && e.peek().t != tokEOF {
				if done {
					// Skip remaining cases after break
					e.advance()
					continue
				}
				if e.peek().t == tokIdent && e.peek().v == "case" {
					e.advance()
					caseVal := e.expr()
					e.expect(tokColon)
					if !matched && !fallThru {
						if looseEqual(switchVal, caseVal) {
							matched = true
						}
					}
					if matched || fallThru {
						for e.peek().t != tokRBrace && e.peek().t != tokEOF {
							if e.peek().t == tokIdent && (e.peek().v == "case" || e.peek().v == "default") { break }
							if e.peek().t == tokIdent && e.peek().v == "break" {
								e.advance()
								if e.peek().t == tokSemi { e.advance() }
								done = true
								break
							}
							if e.peek().t == tokIdent && e.peek().v == "return" {
								e.advance()
								result := e.expr()
								// skip rest of switch
								for e.peek().t != tokRBrace { e.advance() }
								e.expect(tokRBrace)
								return result
							}
							// Execute statement
							if e.peek().t == tokIdent && (e.peek().v == "const" || e.peek().v == "let" || e.peek().v == "var") {
								e.advance(); name := e.advance().v; e.expect(tokAssign)
								e.scope[name] = e.expr()
								if e.peek().t == tokSemi { e.advance() }
							} else if e.peek().t == tokIdent {
								name := e.peek().v
								if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokAssign {
									e.advance(); e.advance()
									e.scope[name] = e.expr()
									if e.peek().t == tokSemi { e.advance() }
								} else {
									e.expr()
									if e.peek().t == tokSemi { e.advance() }
								}
							} else {
								e.advance()
							}
						}
					} else {
						// Skip case body
						for e.peek().t != tokRBrace && e.peek().t != tokEOF {
							if e.peek().t == tokIdent && (e.peek().v == "case" || e.peek().v == "default") { break }
							e.advance()
						}
					}
				} else if e.peek().t == tokIdent && e.peek().v == "default" {
					e.advance()
					e.expect(tokColon)
					if !matched {
						matched = true
					}
					if matched {
						for e.peek().t != tokRBrace && e.peek().t != tokEOF {
							if e.peek().t == tokIdent && e.peek().v == "break" {
								e.advance()
								if e.peek().t == tokSemi { e.advance() }
								break
							}
							if e.peek().t == tokIdent && e.peek().v == "return" {
								e.advance()
								result := e.expr()
								for e.peek().t != tokRBrace { e.advance() }
								e.expect(tokRBrace)
								return result
							}
							if e.peek().t == tokIdent {
								name := e.peek().v
								if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokAssign {
									e.advance(); e.advance()
									e.scope[name] = e.expr()
									if e.peek().t == tokSemi { e.advance() }
								} else {
									e.expr()
									if e.peek().t == tokSemi { e.advance() }
								}
							} else {
								e.advance()
							}
						}
					}
				} else {
					e.advance()
				}
			}
			e.expect(tokRBrace)
			continue
		}

		// function declaration — skip (already registered by ExtractFunctions)
		if t.t == tokIdent && t.v == "function" {
			e.advance()
			if e.peek().t == tokIdent { e.advance() }
			if e.peek().t == tokLParen { e.skipBalanced(tokLParen, tokRParen) }
			if e.peek().t == tokLBrace { e.skipBalanced(tokLBrace, tokRBrace) }
			continue
		}

		// console.log/warn/error
		if t.t == tokIdent && t.v == "console" {
			e.advance()
			if e.peek().t == tokDot {
				e.advance()
				method := e.advance().v
				if e.peek().t == tokLParen {
					e.advance() // skip (
					var parts []string
					for e.peek().t != tokRParen && e.peek().t != tokEOF {
						val := e.expr()
						parts = append(parts, val.toStr())
						if e.peek().t == tokComma {
							e.advance()
						}
					}
					e.expect(tokRParen)
					line := strings.Join(parts, " ")
					switch method {
					case "warn", "error":
						fmt.Fprintln(os.Stderr, line)
					default:
						fmt.Println(line)
					}
				}
			}
			if e.peek().t == tokSemi { e.advance() }
			continue
		}

		// Identifier — could be assignment, postfix ++/--, or function call
		if t.t == tokIdent {
			name := t.v
			// Check for postfix ++/--
			if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokPlusPlus {
				e.advance(); e.advance()
				if v, ok := e.getVar(name); ok {
					e.setVar(name, newNum(v.toNum()+1))
				}
				if e.peek().t == tokSemi { e.advance() }
				continue
			}
			if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokMinusMinus {
				e.advance(); e.advance()
				if v, ok := e.getVar(name); ok {
					e.setVar(name, newNum(v.toNum()-1))
				}
				if e.peek().t == tokSemi { e.advance() }
				continue
			}
			// Check for += / -=
			if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokPlusAssign {
				e.advance(); e.advance()
				val := e.expr()
				if v, ok := e.getVar(name); ok {
					if v.typ == TypeString || val.typ == TypeString {
						e.setVar(name, newStr(v.toStr()+val.toStr()))
					} else {
						e.setVar(name, newNum(v.toNum()+val.toNum()))
					}
				}
				if e.peek().t == tokSemi { e.advance() }
				continue
			}
			if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokMinusAssign {
				e.advance(); e.advance()
				val := e.expr()
				if v, ok := e.getVar(name); ok {
					e.setVar(name, newNum(v.toNum()-val.toNum()))
				}
				if e.peek().t == tokSemi { e.advance() }
				continue
			}
			// Check for simple reassignment: name = expr
			if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokAssign {
				e.advance(); e.advance()
				e.setVar(name, e.expr())
				if e.peek().t == tokSemi { e.advance() }
				continue
			}
			// Property assignment: name.prop = expr or name["key"] = expr
			// Lookahead to check if this is an assignment (not a method call)
			if e.pos+1 < len(e.tokens) && (e.tokens[e.pos+1].t == tokDot || e.tokens[e.pos+1].t == tokLBrack) {
				if e.isPropAssignment() {
					obj := e.scope[name]
					if obj != nil {
						e.advance() // skip name
						e.evalPropAssignment(obj)
						if e.peek().t == tokSemi { e.advance() }
						continue
					}
				}
			}
			// Function call or other expression
			e.expr()
			if e.peek().t == tokSemi { e.advance() }
			continue
		}

		// skip other tokens
		e.advance()
	}
	return nil
}

// evalStatementsWithLastValue is like evalStatements but returns the value
// of the last expression (not just return statements). Used by Eval() for
// multi-statement code like: `var x = 1; x + 2` → 3
func (e *evaluator) evalStatementsWithLastValue() *Value {
	var lastVal *Value
	for e.peek().t != tokEOF {
		t := e.peek()

		// const/let/var declaration
		if t.t == tokIdent && (t.v == "const" || t.v == "let" || t.v == "var") {
			e.advance()
			if e.peek().t == tokLBrack {
				// Array destructuring
				e.advance()
				var names []string
				for e.peek().t != tokRBrack && e.peek().t != tokEOF {
					if e.peek().t == tokIdent { names = append(names, e.advance().v) } else { e.advance() }
				}
				e.expect(tokRBrack)
				e.expect(tokAssign)
				val := e.expr()
				if val.typ == TypeArray {
					for i, name := range names {
						if i < len(val.array) { e.scope[name] = val.array[i] } else { e.scope[name] = Undefined }
					}
				}
			} else if e.peek().t == tokLBrace {
				// Object destructuring
				e.advance()
				var names []string
				for e.peek().t != tokRBrace && e.peek().t != tokEOF {
					if e.peek().t == tokIdent { names = append(names, e.advance().v) }
					if e.peek().t == tokComma { e.advance() }
				}
				e.expect(tokRBrace)
				e.expect(tokAssign)
				val := e.expr()
				if val.typ == TypeObject && val.object != nil {
					for _, name := range names {
						if v, ok := val.object[name]; ok { e.scope[name] = v } else { e.scope[name] = Undefined }
					}
				}
			} else {
				name := e.advance().v
				e.expect(tokAssign)
				val := e.expr()
				e.scope[name] = val
			}
			if e.peek().t == tokSemi { e.advance() }
			continue
		}

		// return statement
		if t.t == tokIdent && t.v == "return" {
			e.advance()
			return e.expr()
		}

		// function declaration — skip (already registered by ExtractFunctions)
		if t.t == tokIdent && t.v == "function" {
			e.advance()
			if e.peek().t == tokIdent { e.advance() }
			if e.peek().t == tokLParen { e.skipBalanced(tokLParen, tokRParen) }
			if e.peek().t == tokLBrace { e.skipBalanced(tokLBrace, tokRBrace) }
			continue
		}

		// Expression statement — capture its value as last value
		if t.t == tokIdent || t.t == tokNum || t.t == tokStr || t.t == tokLParen || t.t == tokLBrack || t.t == tokNot || t.t == tokTemplatePart {
			lastVal = e.expr()
			if e.peek().t == tokSemi { e.advance() }
			continue
		}

		e.advance()
	}
	return lastVal
}

// evalBlock evaluates statements inside { } until the closing }.
// Returns non-nil if a return statement was encountered.
func (e *evaluator) evalBlock() *Value {
	for e.peek().t != tokRBrace && e.peek().t != tokEOF {
		t := e.peek()

		if t.t == tokIdent && t.v == "return" {
			e.advance()
			result := e.expr()
			// Skip to closing brace
			for e.peek().t != tokRBrace && e.peek().t != tokEOF {
				e.advance()
			}
			if e.peek().t == tokRBrace {
				e.advance()
			}
			return result
		}

		// Handle const/let/var declarations
		if t.t == tokIdent && (t.v == "const" || t.v == "let" || t.v == "var") {
			e.advance()

			// Array destructuring: const [a, b] = expr
			if e.peek().t == tokLBrack {
				e.advance() // skip [
				var names []string
				for e.peek().t != tokRBrack && e.peek().t != tokEOF {
					if e.peek().t == tokIdent {
						names = append(names, e.advance().v)
					} else {
						e.advance()
					}
				}
				e.expect(tokRBrack)
				e.expect(tokAssign)
				val := e.expr()
				if val.typ == TypeArray {
					for i, name := range names {
						if i < len(val.array) {
							e.scope[name] = val.array[i]
						} else {
							e.scope[name] = Undefined
						}
					}
				} else {
					for _, name := range names {
						e.scope[name] = Undefined
					}
				}
				if e.peek().t == tokSemi {
					e.advance()
				}
				continue
			}

			// Object destructuring: const { a, b } = expr
			if e.peek().t == tokLBrace {
				e.advance() // skip {
				var names []string
				for e.peek().t != tokRBrace && e.peek().t != tokEOF {
					if e.peek().t == tokIdent {
						names = append(names, e.advance().v)
					}
					if e.peek().t == tokComma {
						e.advance()
					}
				}
				e.expect(tokRBrace)
				e.expect(tokAssign)
				val := e.expr()
				if val.typ == TypeObject && val.object != nil {
					for _, name := range names {
						if v, ok := val.object[name]; ok {
							e.scope[name] = v
						} else {
							e.scope[name] = Undefined
						}
					}
				}
				if e.peek().t == tokSemi {
					e.advance()
				}
				continue
			}

			name := e.advance().v
			e.expect(tokAssign)
			val := e.expr()
			e.scope[name] = val
			if e.peek().t == tokSemi {
				e.advance()
			}
			continue
		}

		// Handle if statements inside block
		if t.t == tokIdent && t.v == "if" {
			e.advance() // skip "if"
			e.expect(tokLParen)
			cond := e.expr()
			e.expect(tokRParen)

			if cond.truthy() {
				if e.peek().t == tokLBrace {
					e.advance()
					result := e.evalBlock()
					if result != nil {
						// Skip to closing brace of outer block
						depth := 1
						for depth > 0 && e.peek().t != tokEOF {
							if e.peek().t == tokLBrace { depth++ }
							if e.peek().t == tokRBrace { depth-- }
							if depth > 0 { e.advance() }
						}
						if e.peek().t == tokRBrace { e.advance() }
						return result
					}
				} else if e.peek().t == tokIdent && e.peek().v == "return" {
					result := e.evalSingleStatement()
					if result != nil {
						for e.peek().t != tokRBrace && e.peek().t != tokEOF { e.advance() }
						if e.peek().t == tokRBrace { e.advance() }
						return result
					}
				} else {
					e.expr()
					if e.peek().t == tokSemi { e.advance() }
				}
				// Skip else
				if e.peek().t == tokIdent && e.peek().v == "else" {
					e.advance()
					if e.peek().t == tokLBrace {
						e.skipBalanced(tokLBrace, tokRBrace)
					} else if e.peek().t == tokIdent && e.peek().v == "if" {
						e.skipIfChain()
					} else {
						e.skipSingleStatement()
					}
				}
			} else {
				if e.peek().t == tokLBrace {
					e.skipBalanced(tokLBrace, tokRBrace)
				} else {
					e.skipSingleStatement()
				}
				if e.peek().t == tokIdent && e.peek().v == "else" {
					e.advance()
					if e.peek().t == tokIdent && e.peek().v == "if" {
						// Re-check condition in next iteration isn't possible here,
						// handle inline
						e.advance() // skip "if"
						e.expect(tokLParen)
						cond2 := e.expr()
						e.expect(tokRParen)
						if cond2.truthy() {
							if e.peek().t == tokLBrace {
								e.advance()
								result := e.evalBlock()
								if result != nil {
									for e.peek().t != tokRBrace && e.peek().t != tokEOF { e.advance() }
									if e.peek().t == tokRBrace { e.advance() }
									return result
								}
							}
						} else {
							if e.peek().t == tokLBrace { e.skipBalanced(tokLBrace, tokRBrace) }
						}
						if e.peek().t == tokIdent && e.peek().v == "else" {
							e.advance()
							if e.peek().t == tokLBrace {
								e.advance()
								result := e.evalBlock()
								if result != nil {
									for e.peek().t != tokRBrace && e.peek().t != tokEOF { e.advance() }
									if e.peek().t == tokRBrace { e.advance() }
									return result
								}
							}
						}
					} else if e.peek().t == tokLBrace {
						e.advance()
						result := e.evalBlock()
						if result != nil {
							for e.peek().t != tokRBrace && e.peek().t != tokEOF { e.advance() }
							if e.peek().t == tokRBrace { e.advance() }
							return result
						}
					} else if e.peek().t == tokIdent && e.peek().v == "return" {
						result := e.evalSingleStatement()
						if result != nil {
							for e.peek().t != tokRBrace && e.peek().t != tokEOF { e.advance() }
							if e.peek().t == tokRBrace { e.advance() }
							return result
						}
					}
				}
			}
			continue
		}

		// Handle reassignment, ++, +=, etc. inside block
		if t.t == tokIdent {
			name := t.v
			// postfix ++
			if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokPlusPlus {
				e.advance(); e.advance()
				if v, ok := e.scope[name]; ok {
					e.scope[name] = newNum(v.toNum() + 1)
				}
				if e.peek().t == tokSemi { e.advance() }
				continue
			}
			// postfix --
			if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokMinusMinus {
				e.advance(); e.advance()
				if v, ok := e.scope[name]; ok {
					e.scope[name] = newNum(v.toNum() - 1)
				}
				if e.peek().t == tokSemi { e.advance() }
				continue
			}
			// +=
			if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokPlusAssign {
				e.advance(); e.advance()
				val := e.expr()
				if v, ok := e.scope[name]; ok {
					if v.typ == TypeString || val.typ == TypeString {
						e.scope[name] = newStr(v.toStr() + val.toStr())
					} else {
						e.scope[name] = newNum(v.toNum() + val.toNum())
					}
				}
				if e.peek().t == tokSemi { e.advance() }
				continue
			}
			// -=
			if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokMinusAssign {
				e.advance(); e.advance()
				val := e.expr()
				if v, ok := e.scope[name]; ok {
					e.scope[name] = newNum(v.toNum() - val.toNum())
				}
				if e.peek().t == tokSemi { e.advance() }
				continue
			}
			// simple reassignment
			if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokAssign {
				e.advance(); e.advance()
				e.scope[name] = e.expr()
				if e.peek().t == tokSemi { e.advance() }
				continue
			}
			// Property assignment: name.prop = expr or name["key"] = expr
			if e.pos+1 < len(e.tokens) && (e.tokens[e.pos+1].t == tokDot || e.tokens[e.pos+1].t == tokLBrack) {
				if name != "console" && e.isPropAssignment() {
					obj := e.scope[name]
					if obj != nil {
						e.advance() // skip name
						e.evalPropAssignment(obj)
						if e.peek().t == tokSemi { e.advance() }
						continue
					}
				}
			}
			// console.log/warn/error
			if name == "console" {
				e.advance()
				if e.peek().t == tokDot {
					e.advance()
					method := e.advance().v
					if e.peek().t == tokLParen {
						e.advance() // skip (
						var parts []string
						for e.peek().t != tokRParen && e.peek().t != tokEOF {
							val := e.expr()
							parts = append(parts, val.toStr())
							if e.peek().t == tokComma {
								e.advance()
							}
						}
						e.expect(tokRParen)
						line := strings.Join(parts, " ")
						switch method {
						case "warn", "error":
							fmt.Fprintln(os.Stderr, line)
						default:
							fmt.Println(line)
						}
					}
				}
				if e.peek().t == tokSemi { e.advance() }
				continue
			}
			// for loops inside blocks
			if name == "for" {
				e.advance()
				e.expect(tokLParen)
				if e.peek().t == tokIdent && (e.peek().v == "const" || e.peek().v == "let" || e.peek().v == "var") {
					e.advance()
					vn := e.advance().v
					if e.peek().t == tokIdent && e.peek().v == "of" {
						e.advance()
						arr := e.expr()
						e.expect(tokRParen)
						if e.peek().t == tokLBrace {
							bs := e.pos
							e.skipBalanced(tokLBrace, tokRBrace)
							be := e.pos
							if arr.typ == TypeArray {
								for _, item := range arr.array {
									e.scope[vn] = item
									bt := make([]tok, be-bs)
									copy(bt, e.tokens[bs:be])
									if len(bt) >= 2 && bt[0].t == tokLBrace {
										bt = bt[1 : len(bt)-1]
									}
									bt = append(bt, tok{t: tokEOF})
									bev := &evaluator{tokens: bt, pos: 0, scope: e.scope}
									result := bev.evalStatements()
									if result != nil { return result }
								}
							}
						}
						continue
					}
					e.expect(tokAssign)
					e.scope[vn] = e.expr()
				} else {
					if e.peek().t != tokSemi { e.expr() }
				}
				e.expect(tokSemi)
				cs := e.pos
				if e.peek().t != tokSemi { e.expr() }
				ce := e.pos
				e.expect(tokSemi)
				us := e.pos
				if e.peek().t != tokRParen { e.expr() }
				ue := e.pos
				e.expect(tokRParen)
				if e.peek().t == tokLBrace {
					bs := e.pos
					e.skipBalanced(tokLBrace, tokRBrace)
					be := e.pos
					for iter := 0; iter < 10000; iter++ {
						ct := make([]tok, ce-cs)
						copy(ct, e.tokens[cs:ce])
						ct = append(ct, tok{t: tokEOF})
						cev := &evaluator{tokens: ct, pos: 0, scope: e.scope}
						if !cev.expr().truthy() { break }
						bt := make([]tok, be-bs)
						copy(bt, e.tokens[bs:be])
						if len(bt) >= 2 && bt[0].t == tokLBrace { bt = bt[1 : len(bt)-1] }
						bt = append(bt, tok{t: tokEOF})
						bev := &evaluator{tokens: bt, pos: 0, scope: e.scope}
						result := bev.evalStatements()
						if result != nil { return result }
						ut := make([]tok, ue-us)
						copy(ut, e.tokens[us:ue])
						ut = append(ut, tok{t: tokEOF})
						uev := &evaluator{tokens: ut, pos: 0, scope: e.scope}
						uev.expr()
					}
				}
				continue
			}
		}

		e.advance()
	}
	if e.peek().t == tokRBrace {
		e.advance()
	}
	return nil // no return in block
}

// skipIfChain skips an entire if/else if/else chain without evaluating.
func (e *evaluator) skipIfChain() {
	// Skip "if"
	e.advance()
	// Skip condition (...)
	if e.peek().t == tokLParen {
		e.skipBalanced(tokLParen, tokRParen)
	}
	// Skip block {...}
	if e.peek().t == tokLBrace {
		e.skipBalanced(tokLBrace, tokRBrace)
	}
	// Handle else
	if e.peek().t == tokIdent && e.peek().v == "else" {
		e.advance()
		if e.peek().t == tokIdent && e.peek().v == "if" {
			e.skipIfChain()
		} else if e.peek().t == tokLBrace {
			e.skipBalanced(tokLBrace, tokRBrace)
		}
	}
}

func (e *evaluator) parseArray() *Value {
	e.expect(tokLBrack)
	var items []*Value
	for e.peek().t != tokRBrack && e.peek().t != tokEOF {
		items = append(items, e.expr())
		if e.peek().t == tokComma {
			e.advance()
		}
	}
	e.expect(tokRBrack)
	return newArr(items)
}

func (e *evaluator) parseObject() *Value {
	e.expect(tokLBrace)
	obj := make(map[string]*Value)
	for e.peek().t != tokRBrace && e.peek().t != tokEOF {
		// spread: ...expr
		if e.peek().t == tokSpread {
			e.advance()
			src := e.expr()
			if src.typ == TypeObject && src.object != nil {
				for k, v := range src.object {
					obj[k] = v
				}
			}
			if e.peek().t == tokComma {
				e.advance()
			}
			continue
		}

		// key: can be identifier or string
		var key string
		if e.peek().t == tokStr {
			key = e.advance().v
		} else if e.peek().t == tokIdent {
			key = e.advance().v
		} else {
			e.advance()
			continue
		}

		// Shorthand: { key } (no colon, value same as key name)
		if e.peek().t == tokComma || e.peek().t == tokRBrace {
			if val, ok := e.scope[key]; ok {
				obj[key] = val
			} else {
				obj[key] = Undefined
			}
			if e.peek().t == tokComma {
				e.advance()
			}
			continue
		}

		e.expect(tokColon)
		val := e.expr()
		obj[key] = val
		if e.peek().t == tokComma {
			e.advance()
		}
	}
	e.expect(tokRBrace)
	return newObj(obj)
}

// ─── JSON → Value ───────────────────────────────────────────────

func jsonToValue(data json.RawMessage) *Value {
	if data == nil {
		return Null
	}
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Null
	}
	return interfaceToValue(raw)
}

func interfaceToValue(v interface{}) *Value {
	if v == nil {
		return Null
	}
	switch val := v.(type) {
	case bool:
		return newBool(val)
	case float64:
		return newNum(val)
	case string:
		return newStr(val)
	case []interface{}:
		arr := make([]*Value, len(val))
		for i, item := range val {
			arr[i] = interfaceToValue(item)
		}
		return newArr(arr)
	case map[string]interface{}:
		obj := make(map[string]*Value, len(val))
		for k, item := range val {
			obj[k] = interfaceToValue(item)
		}
		return newObj(obj)
	}
	return Null
}

// formatWithCommas formats an integer with thousand separators (e.g. 1000 → "1,000")
func formatWithCommas(n int64) string {
	if n < 0 {
		return "-" + formatWithCommas(-n)
	}
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// isPropAssignment does a lookahead to check if the current position is a
// property assignment chain (e.g. obj.x = val, obj["k"] = val, obj.a.b = val)
// vs a method call (e.g. obj.push(1), obj.method()).
// Does NOT consume tokens.
func (e *evaluator) isPropAssignment() bool {
	saved := e.pos
	defer func() { e.pos = saved }()
	e.advance() // skip name
	for {
		if e.peek().t == tokDot {
			e.advance() // skip .
			e.advance() // skip prop
			next := e.peek().t
			if next == tokAssign || next == tokPlusAssign || next == tokMinusAssign {
				return true
			}
			// continue chain
		} else if e.peek().t == tokLBrack {
			e.advance() // skip [
			depth := 1
			for depth > 0 && e.peek().t != tokEOF {
				if e.peek().t == tokLBrack { depth++ }
				if e.peek().t == tokRBrack { depth-- }
				e.advance()
			}
			next := e.peek().t
			if next == tokAssign || next == tokPlusAssign || next == tokMinusAssign {
				return true
			}
		} else {
			return false
		}
	}
}

// evalPropAssignment evaluates a property assignment chain on obj.
// Assumes we've already consumed the variable name.
func (e *evaluator) evalPropAssignment(obj *Value) {
	for {
		if e.peek().t == tokDot {
			e.advance() // skip .
			prop := e.advance().v
			if e.peek().t == tokAssign {
				e.advance()
				val := e.expr()
				if obj.typ == TypeObject && obj.object != nil {
					obj.object[prop] = val
				}
				return
			}
			if e.peek().t == tokPlusAssign {
				e.advance()
				val := e.expr()
				if obj.typ == TypeObject && obj.object != nil {
					if prev, ok := obj.object[prop]; ok {
						if prev.typ == TypeString || val.typ == TypeString {
							obj.object[prop] = newStr(prev.toStr() + val.toStr())
						} else {
							obj.object[prop] = newNum(prev.toNum() + val.toNum())
						}
					} else {
						obj.object[prop] = val
					}
				}
				return
			}
			if e.peek().t == tokMinusAssign {
				e.advance()
				val := e.expr()
				if obj.typ == TypeObject && obj.object != nil {
					if prev, ok := obj.object[prop]; ok {
						obj.object[prop] = newNum(prev.toNum() - val.toNum())
					}
				}
				return
			}
			// Continue chain
			if obj.typ == TypeObject && obj.object != nil {
				if next, ok := obj.object[prop]; ok {
					obj = next
					continue
				}
			}
			return
		} else if e.peek().t == tokLBrack {
			e.advance() // skip [
			key := e.expr()
			e.expect(tokRBrack)
			keyStr := key.toStr()
			if e.peek().t == tokAssign {
				e.advance()
				val := e.expr()
				if obj.typ == TypeObject && obj.object != nil {
					obj.object[keyStr] = val
				} else if obj.typ == TypeArray {
					idx := int(key.toNum())
					if idx >= 0 && idx < len(obj.array) {
						obj.array[idx] = val
					}
				}
				return
			}
			// Continue chain
			if obj.typ == TypeObject && obj.object != nil {
				if next, ok := obj.object[keyStr]; ok {
					obj = next
					continue
				}
			}
			return
		} else {
			return
		}
	}
}
