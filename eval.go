package espresso

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// ─── Arrow Function Registry ────────────────────────────────────
// Stores captured arrow functions so they can be called later.

type arrowFunc struct {
	params    []string
	tokens    []tok
	isBlock   bool
	scope     map[string]*Value
	bc        *bytecode // lazily compiled bytecode; nil = not yet attempted
	bcTried   bool      // true if compilation was attempted (avoid retrying)
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

	// Try bytecode path — compile on first call, cache on the arrowFunc
	// Skip bytecoding for arrows that contain await (async semantics differ)
	if !af.bcTried {
		af.bcTried = true
		if !needsInterpreter(af.tokens) {
			var bodyTokens []tok
			if af.isBlock {
				// Block arrow: tokens include { ... }, strip outer braces
				if len(af.tokens) >= 2 && af.tokens[0].t == tokLBrace {
					bodyTokens = af.tokens[1 : len(af.tokens)-1]
				} else {
					bodyTokens = af.tokens
				}
			} else {
				// Expression arrow: wrap as "return expr"
				bodyTokens = make([]tok, 0, len(af.tokens)+2)
				bodyTokens = append(bodyTokens, tok{t: tokIdent, v: "return"})
				bodyTokens = append(bodyTokens, af.tokens...)
			}
			// Append EOF
			bodyTokens = append(bodyTokens, tok{t: tokEOF})
			af.bc = compileFuncBodyTokens(bodyTokens)
			if af.bc != nil && len(af.params) > 0 {
				af.bc.params = af.params
			}
		}
	}
	if af.bc != nil {
		// Build scope for bytecode execution
		scope := make(map[string]*Value, len(callerScope)+len(af.scope)+len(af.params))
		if af.scope != nil {
			for k, v := range af.scope {
				scope[k] = v
			}
		}
		if callerScope != nil {
			for k, v := range callerScope {
				if _, ok := af.scope[k]; !ok {
					scope[k] = v
				}
			}
		}
		// Bind params
		for i, name := range af.params {
			if strings.HasPrefix(name, "__destructure__:") || strings.HasPrefix(name, "__obj_destructure__:") || strings.HasPrefix(name, "__rest__:") {
				// Complex params — fall through to interpreter
				goto interpreterPath
			}
			if i < len(args) {
				scope[name] = args[i]
			} else {
				scope[name] = Undefined
			}
		}
		{
			var result *Value
			func() {
				defer func() {
					if r := recover(); r != nil {
						result = Undefined // JS throw escaped — return undefined
					}
				}()
				result = execBytecode(af.bc, scope)
			}()
			// Write back scope mutations
			if callerScope != nil {
				for k, v := range scope {
					if _, existed := callerScope[k]; existed {
						callerScope[k] = v
					}
				}
			}
			return result
		}
	}
interpreterPath:

	// Fast path: simple expression arrows (n => n * 2) — reuse caller scope
	if !af.isBlock && len(af.params) <= 2 && len(af.scope) == 0 {
		saved := make([]*Value, 0, 8)
		var savedNames []string
		for i, name := range af.params {
			if strings.HasPrefix(name, "__destructure__:") {
				dnames := strings.Split(name[len("__destructure__:"):], ",")
				var arr *Value
				if i < len(args) { arr = args[i] }
				for j, dn := range dnames {
					savedNames = append(savedNames, dn)
					saved = append(saved, callerScope[dn])
					if arr != nil && arr.typ == TypeArray && j < len(arr.array) {
						callerScope[dn] = arr.array[j]
					} else {
						callerScope[dn] = Undefined
					}
				}
			} else if strings.HasPrefix(name, "__obj_destructure__:") {
				dnames := strings.Split(name[len("__obj_destructure__:"):], ",")
				var obj *Value
				if i < len(args) { obj = args[i] }
				for _, dn := range dnames {
					savedNames = append(savedNames, dn)
					saved = append(saved, callerScope[dn])
					if obj != nil && obj.typ == TypeObject && obj.object != nil {
						if v, ok := obj.object[dn]; ok { callerScope[dn] = v } else { callerScope[dn] = Undefined }
					} else {
						callerScope[dn] = Undefined
					}
				}
			} else {
				savedNames = append(savedNames, name)
				saved = append(saved, callerScope[name])
				if i < len(args) {
					callerScope[name] = args[i]
				} else {
					callerScope[name] = Undefined
				}
			}
		}
		ev := &evaluator{tokens: af.tokens, pos: 0, scope: callerScope}
		result := ev.expr()
		for i, name := range savedNames {
			if saved[i] != nil {
				callerScope[name] = saved[i]
			} else {
				delete(callerScope, name)
			}
		}
		return result
	}

	// General path: build child scope.
	// Arrow functions use LEXICAL scope — af.scope (captured at creation) is authoritative
	// for free variables. callerScope only contributes names that aren't already in af.scope
	// (e.g. `this` passed by a method call), so caller state can't shadow closure captures.
	childScope := make(map[string]*Value, len(af.scope)+len(callerScope)+len(af.params))
	for k, v := range af.scope {
		childScope[k] = v
	}
	for k, v := range callerScope {
		if _, ok := af.scope[k]; !ok {
			childScope[k] = v
		}
	}
	for i, name := range af.params {
		if strings.HasPrefix(name, "__rest__:") {
			restName := name[len("__rest__:"):]
			var restArr []*Value
			if i < len(args) {
				restArr = args[i:]
			}
			childScope[restName] = newArr(restArr)
		} else if strings.HasPrefix(name, "__destructure__:") {
			dnames := strings.Split(name[len("__destructure__:"):], ",")
			var arr *Value
			if i < len(args) { arr = args[i] }
			for j, dn := range dnames {
				if arr != nil && arr.typ == TypeArray && j < len(arr.array) {
					childScope[dn] = arr.array[j]
				} else {
					childScope[dn] = Undefined
				}
			}
		} else if strings.HasPrefix(name, "__obj_destructure__:") {
			dnames := strings.Split(name[len("__obj_destructure__:"):], ",")
			var obj *Value
			if i < len(args) { obj = args[i] }
			for _, dn := range dnames {
				if obj != nil && obj.typ == TypeObject && obj.object != nil {
					if v, ok := obj.object[dn]; ok {
						childScope[dn] = v
					} else {
						childScope[dn] = Undefined
					}
				} else {
					childScope[dn] = Undefined
				}
			}
		} else if i < len(args) {
			childScope[name] = args[i]
		} else {
			childScope[name] = Undefined
		}
	}

	ev := &evaluator{tokens: af.tokens, pos: 0, scope: childScope}

	if af.isBlock {
		result := ev.evalStatements()
		// Write back scope mutations to callerScope so side effects are visible
		if callerScope != nil {
			for k, v := range childScope {
				if _, existed := callerScope[k]; existed {
					callerScope[k] = v
				}
			}
		}
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
	tokStarAssign   // *=
	tokSlashAssign  // /=
	tokNullAssign   // ??=
	tokOrAssign     // ||=
	tokAndAssign    // &&=
	tokRegExp       // /pattern/flags
	tokBitAnd       // &
	tokBitOr        // |
	tokBitXor       // ^
	tokBitNot       // ~
	tokLShift       // <<
	tokRShift       // >>
	tokURShift      // >>>
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

		// template literal — always tokTemplatePart (even without interpolation)
		if ch == '`' {
			i++
			var sb strings.Builder
			for i < len(src) && src[i] != '`' {
				if src[i] == '\\' && i+1 < len(src) { i++; sb.WriteByte(src[i]) } else { sb.WriteByte(src[i]) }
				i++
			}
			if i < len(src) { i++ }
			tokens = append(tokens, tok{t: tokTemplatePart, v: sb.String()})
			continue
		}

		// number (decimal, hex 0x, octal 0o, binary 0b)
		if ch >= '0' && ch <= '9' {
			start := i
			if ch == '0' && i+1 < len(src) && (src[i+1] == 'x' || src[i+1] == 'X') {
				// hex: 0xDEAD
				i += 2
				for i < len(src) && ((src[i] >= '0' && src[i] <= '9') || (src[i] >= 'a' && src[i] <= 'f') || (src[i] >= 'A' && src[i] <= 'F')) {
					i++
				}
				n, _ := strconv.ParseInt(src[start+2:i], 16, 64)
				tokens = append(tokens, tok{t: tokNum, v: src[start:i], n: float64(n)})
			} else if ch == '0' && i+1 < len(src) && (src[i+1] == 'o' || src[i+1] == 'O') {
				// octal: 0o777
				i += 2
				for i < len(src) && src[i] >= '0' && src[i] <= '7' {
					i++
				}
				n, _ := strconv.ParseInt(src[start+2:i], 8, 64)
				tokens = append(tokens, tok{t: tokNum, v: src[start:i], n: float64(n)})
			} else if ch == '0' && i+1 < len(src) && (src[i+1] == 'b' || src[i+1] == 'B') {
				// binary: 0b1010
				i += 2
				for i < len(src) && (src[i] == '0' || src[i] == '1') {
					i++
				}
				n, _ := strconv.ParseInt(src[start+2:i], 2, 64)
				tokens = append(tokens, tok{t: tokNum, v: src[start:i], n: float64(n)})
			} else {
				// decimal (possibly float)
				for i < len(src) && ((src[i] >= '0' && src[i] <= '9') || src[i] == '.') {
					i++
				}
				// Handle exponent: 1e5, 1.5e-3
				if i < len(src) && (src[i] == 'e' || src[i] == 'E') {
					i++
					if i < len(src) && (src[i] == '+' || src[i] == '-') { i++ }
					for i < len(src) && src[i] >= '0' && src[i] <= '9' { i++ }
				}
				n, _ := strconv.ParseFloat(src[start:i], 64)
				tokens = append(tokens, tok{t: tokNum, v: src[start:i], n: n})
			}
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

		// private field #name — tokenize as identifier with # prefix
		if ch == '#' && i+1 < len(src) && isJSIdentStart(src[i+1]) {
			start := i
			i++ // skip #
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
			if i+2 < len(src) && src[i+2] == '=' {
				tokens = append(tokens, tok{t: tokAndAssign})
				i += 3
			} else {
				tokens = append(tokens, tok{t: tokAnd})
				i += 2
			}
			continue
		}
		if ch == '|' && i+1 < len(src) && src[i+1] == '|' {
			if i+2 < len(src) && src[i+2] == '=' {
				tokens = append(tokens, tok{t: tokOrAssign})
				i += 3
			} else {
				tokens = append(tokens, tok{t: tokOr})
				i += 2
			}
			continue
		}
		if ch == '>' && i+2 < len(src) && src[i+1] == '>' && src[i+2] == '>' {
			tokens = append(tokens, tok{t: tokURShift})
			i += 3
			continue
		}
		if ch == '>' && i+1 < len(src) && src[i+1] == '>' {
			tokens = append(tokens, tok{t: tokRShift})
			i += 2
			continue
		}
		if ch == '>' && i+1 < len(src) && src[i+1] == '=' {
			tokens = append(tokens, tok{t: tokGtEq})
			i += 2
			continue
		}
		if ch == '<' && i+1 < len(src) && src[i+1] == '<' {
			tokens = append(tokens, tok{t: tokLShift})
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
			if i+1 < len(src) && src[i+1] == '=' {
				tokens = append(tokens, tok{t: tokStarAssign})
				i++
			} else {
				tokens = append(tokens, tok{t: tokStar})
			}
		case '/':
			// Determine if this is a regex literal or division operator.
			// It's a regex if the previous meaningful token can't end an expression value.
			isRegex := true
			if len(tokens) > 0 {
				prev := tokens[len(tokens)-1]
				switch prev.t {
				case tokNum, tokStr, tokRParen, tokRBrack, tokPlusPlus, tokMinusMinus:
					isRegex = false
				case tokIdent:
					// keywords like return, typeof, etc. mean regex; identifiers/values mean division
					switch prev.v {
					case "return", "typeof", "instanceof", "in", "delete", "void", "throw", "case", "new":
						isRegex = true
					default:
						isRegex = false
					}
				}
			}
			if isRegex {
				// Parse regex literal /pattern/flags
				i++ // skip opening /
				var pattern strings.Builder
				for i < len(src) && src[i] != '/' {
					if src[i] == '\\' && i+1 < len(src) {
						pattern.WriteByte(src[i])
						i++
						pattern.WriteByte(src[i])
					} else {
						pattern.WriteByte(src[i])
					}
					i++
				}
				if i < len(src) {
					i++ // skip closing /
				}
				// Read flags
				var flags strings.Builder
				for i < len(src) && ((src[i] >= 'a' && src[i] <= 'z') || (src[i] >= 'A' && src[i] <= 'Z')) {
					flags.WriteByte(src[i])
					i++
				}
				// Store pattern\x00flags in v
				tokens = append(tokens, tok{t: tokRegExp, v: pattern.String() + "\x00" + flags.String()})
				continue // skip the i++ at the end of the switch
			}
			if i+1 < len(src) && src[i+1] == '=' {
				tokens = append(tokens, tok{t: tokSlashAssign})
				i++
			} else {
				tokens = append(tokens, tok{t: tokSlash})
			}
		case '%':
			tokens = append(tokens, tok{t: tokPercent})
		case '>':
			tokens = append(tokens, tok{t: tokGt})
		case '<':
			tokens = append(tokens, tok{t: tokLt})
		case '!':
			tokens = append(tokens, tok{t: tokNot})
		case '?':
			if i+2 < len(src) && src[i+1] == '?' && src[i+2] == '=' {
				tokens = append(tokens, tok{t: tokNullAssign})
				i += 2
			} else if i+1 < len(src) && src[i+1] == '?' {
				tokens = append(tokens, tok{t: tokNullCoalesce})
				i++
			} else {
				tokens = append(tokens, tok{t: tokQuestion})
			}
		case '=':
			tokens = append(tokens, tok{t: tokAssign})
		case '&':
			tokens = append(tokens, tok{t: tokBitAnd})
		case '|':
			tokens = append(tokens, tok{t: tokBitOr})
		case '^':
			tokens = append(tokens, tok{t: tokBitXor})
		case '~':
			tokens = append(tokens, tok{t: tokBitNot})
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

// ─── RegExp helpers ─────────────────────────────────────────────

// regexpData stores compiled regexp and metadata inside a Value.Custom.
type regexpData struct {
	Re     *regexp.Regexp
	Source string
	Flags  string
	Global bool
}

// compileJSRegexp converts a JS pattern+flags into a Go *regexp.Regexp.
func compileJSRegexp(pattern, flags string) (*regexp.Regexp, error) {
	var prefix strings.Builder
	prefix.WriteString("(?")
	hasFlags := false
	for _, f := range flags {
		switch f {
		case 'i':
			prefix.WriteByte('i')
			hasFlags = true
		case 'm':
			prefix.WriteByte('m')
			hasFlags = true
		case 's':
			prefix.WriteByte('s')
			hasFlags = true
		}
	}
	goPattern := pattern
	if hasFlags {
		goPattern = prefix.String() + ")" + pattern
	}
	return regexp.Compile(goPattern)
}

// newRegexpValue creates a RegExp Value from pattern and flags strings.
func newRegexpValue(pattern, flags string) *Value {
	re, err := compileJSRegexp(pattern, flags)
	if err != nil {
		return Undefined
	}
	global := strings.Contains(flags, "g")
	rd := &regexpData{Re: re, Source: pattern, Flags: flags, Global: global}
	obj := &Value{
		typ:    TypeObject,
		object: make(map[string]*Value),
		Custom: rd,
	}
	obj.object["source"] = newStr(pattern)
	obj.object["flags"] = newStr(flags)
	obj.object["global"] = newBool(global)
	// test method
	obj.object["test"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 {
			return False
		}
		return newBool(rd.Re.MatchString(args[0].toStr()))
	})
	// exec method
	obj.object["exec"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 {
			return Null
		}
		s := args[0].toStr()
		m := rd.Re.FindStringSubmatch(s)
		if m == nil {
			return Null
		}
		arr := make([]*Value, len(m))
		for i, v := range m {
			arr[i] = newStr(v)
		}
		result := newArr(arr)
		result.object = map[string]*Value{
			"index": newNum(float64(rd.Re.FindStringIndex(s)[0])),
		}
		return result
	})
	return obj
}

// getRegexpData extracts regexpData from a Value, if it is a RegExp object.
func getRegexpData(v *Value) *regexpData {
	if v == nil {
		return nil
	}
	if rd, ok := v.Custom.(*regexpData); ok {
		return rd
	}
	return nil
}

// jsReplacementToGo converts JS replacement string ($1, $2) to Go (${1}, ${2}).
func jsReplacementToGo(repl string) string {
	var sb strings.Builder
	for i := 0; i < len(repl); i++ {
		if repl[i] == '$' && i+1 < len(repl) {
			next := repl[i+1]
			if next >= '1' && next <= '9' {
				sb.WriteString("${")
				i++
				for i < len(repl) && repl[i] >= '0' && repl[i] <= '9' {
					sb.WriteByte(repl[i])
					i++
				}
				sb.WriteByte('}')
				i-- // loop will i++
				continue
			}
		}
		sb.WriteByte(repl[i])
	}
	return sb.String()
}

// ─── Evaluator ──────────────────────────────────────────────────

type evaluator struct {
	tokens   []tok
	pos      int
	scope    map[string]*Value
	thisVal  *Value // current `this` binding
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
	precNullish    = 1  // ??
	precOr         = 2  // ||
	precAnd        = 3  // &&
	precBitOr      = 4  // |
	precBitXor     = 5  // ^
	precBitAnd     = 6  // &
	precEquality   = 7  // === !== == !=
	precComparison = 8  // > < >= <=
	precShift      = 9  // << >> >>>
	precAdditive   = 10 // + -
	precMultiply   = 11 // * / %
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
	case tokBitOr:
		return precBitOr
	case tokBitXor:
		return precBitXor
	case tokBitAnd:
		return precBitAnd
	case tokGt, tokLt, tokGtEq, tokLtEq:
		return precComparison
	case tokLShift, tokRShift, tokURShift:
		return precShift
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

		// in — keyword-based infix: "key" in obj → boolean
		if e.peek().t == tokIdent && e.peek().v == "in" && minPrec <= precComparison {
			e.advance() // skip "in"
			right := e.pratt(precComparison + 1)
			key := left.toStr()
			if right.typ == TypeObject && right.object != nil {
				_, found := right.object[key]
				left = newBool(found)
			} else if right.typ == TypeArray {
				idx := int(left.toNum())
				left = newBool(idx >= 0 && idx < len(right.array))
			} else {
				left = newBool(false)
			}
			continue
		}

		// instanceof — keyword-based infix operator
		if e.peek().t == tokIdent && e.peek().v == "instanceof" && minPrec <= precComparison {
			e.advance() // skip "instanceof"
			ctorName := ""
			if e.peek().t == tokIdent {
				ctorName = e.advance().v
			}
			// Check constructor name
			match := false
			if left.typ == TypeObject && left.object != nil {
				if name, ok := left.object["__constructor__"]; ok {
					match = name.str == ctorName
				}
				// Check full constructor chain (for class hierarchy: Child extends Parent extends ...)
				if !match {
					if chain, ok := left.object["__constructors__"]; ok && chain.typ == TypeArray {
						for _, c := range chain.array {
							if c.str == ctorName {
								match = true
								break
							}
						}
					}
				}
			}
			// Walk prototype chain for class hierarchy instanceof
			if !match && left.typ == TypeObject {
				if ctorVal, ok := e.scope[ctorName]; ok && ctorVal.typ == TypeFunc && ctorVal.object != nil {
					if targetProto, ok := ctorVal.object["__prototype__"]; ok {
						for cur := left.proto; cur != nil; cur = cur.proto {
							if cur == targetProto {
								match = true
								break
							}
						}
					}
				}
			}
			// Also check Error types
			if !match && left.typ == TypeObject && left.object != nil {
				if name, ok := left.object["name"]; ok && name.str == ctorName {
					match = true
				}
			}
			left = newBool(match)
			continue
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
		// Bitwise
		case tokBitAnd:
			left = newNum(float64(int64(left.toNum()) & int64(right.toNum())))
		case tokBitOr:
			left = newNum(float64(int64(left.toNum()) | int64(right.toNum())))
		case tokBitXor:
			left = newNum(float64(int64(left.toNum()) ^ int64(right.toNum())))
		// Shift
		case tokLShift:
			left = newNum(float64(int64(left.toNum()) << uint64(int64(right.toNum())&63)))
		case tokRShift:
			left = newNum(float64(int64(left.toNum()) >> uint64(int64(right.toNum())&63)))
		case tokURShift:
			left = newNum(float64(uint32(left.toNum()) >> (uint32(right.toNum()) & 31)))
		}
	}

	return left
}

// skipExpr skips a complete expression without evaluating it.
// It counts balanced parens/brackets/braces to handle nested expressions.
func (e *evaluator) skipExpr() {
	depth := 0
	ternaryDepth := 0 // tracks ? ... : pairs
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
		case tokQuestion:
			if depth == 0 {
				ternaryDepth++
			}
			e.pos++
		case tokColon:
			if depth == 0 && ternaryDepth > 0 {
				ternaryDepth--
				e.pos++
				continue
			}
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
	if e.peek().t == tokBitNot {
		e.advance()
		val := e.unary()
		return newNum(float64(^int64(val.toNum())))
	}
	// await — unwrap a promise synchronously
	if e.peek().t == tokIdent && e.peek().v == "await" {
		e.advance()
		val := e.unary()
		if p := getPromise(val); p != nil {
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.state == PromiseFulfilled {
				return p.value
			}
			if p.state == PromiseRejected {
				return newThrow(p.value)
			}
			return Undefined // pending
		}
		return val // not a promise, return as-is
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
	// delete obj.prop or delete obj[key]
	if e.peek().t == tokIdent && e.peek().v == "delete" {
		e.advance()
		// Parse the target expression to get obj and prop
		objName := ""
		if e.peek().t == tokIdent {
			objName = e.advance().v
		}
		obj := e.scope[objName]
		if obj == nil {
			return True
		}
		if e.peek().t == tokDot {
			e.advance()
			prop := e.advance().v
			if obj.typ == TypeObject && obj.object != nil {
				delete(obj.object, prop)
			}
			return True
		}
		if e.peek().t == tokLBrack {
			e.advance()
			key := e.expr()
			e.expect(tokRBrack)
			if obj.typ == TypeObject && obj.object != nil {
				delete(obj.object, key.toStr())
			}
			return True
		}
		return True
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
				// Check if next is assignment: obj.prop = value
				if e.peek().t == tokAssign {
					e.advance() // skip =
					rhs := e.expr()
					// Try setter first
					if val.typ == TypeObject && val.getset != nil {
						if desc, ok := val.getset[prop.v]; ok && desc.Set != nil {
							if desc.Set.native != nil {
								desc.Set.native([]*Value{rhs})
							} else if desc.Set.str == "__arrow" {
								scope := map[string]*Value{"this": val}
								callArrow(int(desc.Set.num), []*Value{rhs}, scope)
							}
							return rhs
						}
					}
					// Regular property
					if val.typ == TypeObject && val.object != nil {
						val.object[prop.v] = rhs
					}
					return rhs
				}
				if e.peek().t == tokPlusAssign {
					e.advance()
					rhs := e.expr()
					if val.typ == TypeObject && val.object != nil {
						if prev, ok := val.object[prop.v]; ok {
							if prev.typ == TypeString || rhs.typ == TypeString {
								val.object[prop.v] = newStr(prev.toStr() + rhs.toStr())
							} else {
								val.object[prop.v] = newNum(prev.toNum() + rhs.toNum())
							}
						} else {
							val.object[prop.v] = rhs
						}
					}
					return rhs
				}
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
			// Check for assignment: val[idx] = expr
			if e.peek().t == tokAssign {
				e.advance()
				rhs := e.expr()
				key := idx.toStr()
				if val.typ == TypeObject && val.object != nil {
					val.object[key] = rhs
				} else if val.typ == TypeArray {
					i, err := strconv.Atoi(key)
					if err == nil && i >= 0 && i < len(val.array) {
						val.array[i] = rhs
					}
				}
				val = rhs // assignment expression returns the assigned value
			} else {
				val = val.getProp(idx.toStr())
			}
		case tokLParen:
			// Direct function call: val(args...)
			if val.typ == TypeFunc {
				val = e.evalFuncCall(val)
			} else {
				// Not a function — this ( belongs to something else
				return val
			}
		case tokTemplatePart:
			// Tagged template literal: fn`text ${expr} text`
			if val.typ == TypeFunc {
				val = e.evalTaggedTemplate(val)
			} else {
				// Not a function — evaluate as regular template
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
	// Collect params (handles plain names, array destructure, object destructure)
	var params []string
	for e.peek().t != tokRParen && e.peek().t != tokEOF {
		if e.peek().t == tokIdent {
			params = append(params, e.advance().v)
		} else if e.peek().t == tokLBrace {
			// Object destructuring: ({x, y}) => ...
			e.advance()
			var names []string
			for e.peek().t != tokRBrace && e.peek().t != tokEOF {
				if e.peek().t == tokIdent {
					nm := e.advance().v
					if e.peek().t == tokColon { e.advance(); if e.peek().t == tokIdent { nm = e.advance().v } }
					if e.peek().t == tokAssign { e.advance(); e.expr() }
					names = append(names, nm)
				} else { e.advance() }
				if e.peek().t == tokComma { e.advance() }
			}
			if e.peek().t == tokRBrace { e.advance() }
			params = append(params, "__obj_destructure__:"+strings.Join(names, ","))
		} else if e.peek().t == tokLBrack {
			// Array destructuring: ([a, b]) => ...
			e.advance()
			var names []string
			for e.peek().t != tokRBrack && e.peek().t != tokEOF {
				if e.peek().t == tokIdent { names = append(names, e.advance().v) }
				if e.peek().t == tokComma { e.advance() }
			}
			if e.peek().t == tokRBrack { e.advance() }
			params = append(params, "__destructure__:"+strings.Join(names, ","))
		} else {
			e.advance()
		}
		if e.peek().t == tokAssign { e.advance(); e.expr() }
		if e.peek().t == tokComma { e.advance() }
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
		// Scan expression tokens without evaluating (avoids side effects)
		start := e.pos
		depth := 0
		for e.pos < len(e.tokens) && e.tokens[e.pos].t != tokEOF {
			tt := e.tokens[e.pos].t
			if tt == tokLParen || tt == tokLBrack || tt == tokLBrace { depth++ }
			if tt == tokRParen || tt == tokRBrack || tt == tokRBrace {
				if depth == 0 { break }
				depth--
			}
			if depth == 0 && (tt == tokComma || tt == tokSemi) { break }
			e.pos++
		}
		bodyToks = make([]tok, e.pos-start)
		copy(bodyToks, e.tokens[start:e.pos])
	}
	bodyToks = append(bodyToks, tok{t: tokEOF})

	captured := &arrowFunc{params: params, tokens: bodyToks, isBlock: isBlock, scope: e.scope}
	arrowID := registerArrow(captured)
	return &Value{typ: TypeFunc, str: "__arrow", num: float64(arrowID)}
}

// evalTaggedTemplate handles fn`text ${expr} text` tagged template literals.
// Calls fn(strings, ...values) where strings is an array of static parts.
func (e *evaluator) evalTaggedTemplate(fn *Value) *Value {
	raw := e.advance().v // consume the template token

	var strings []*Value
	var values []*Value

	// Parse the template: split on ${...} boundaries
	i := 0
	var sb stringsBuilder
	for i < len(raw) {
		if i+1 < len(raw) && raw[i] == '$' && raw[i+1] == '{' {
			// End current string part
			strings = append(strings, newStr(sb.String()))
			sb.Reset()
			i += 2
			// Find matching }
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
				i++ // skip closing }
			}
			// Evaluate the expression
			exprTokens := tokenizeCached(exprStr)
			exprEv := &evaluator{tokens: exprTokens, pos: 0, scope: e.scope}
			values = append(values, exprEv.expr())
		} else {
			sb.WriteByte(raw[i])
			i++
		}
	}
	// Final string part
	strings = append(strings, newStr(sb.String()))

	// Build args: [stringsArray, ...values]
	args := make([]*Value, 0, 1+len(values))
	strArr := newArr(strings)
	// Set strings.raw to match the spec
	strArr.object = map[string]*Value{"raw": newArr(strings)}
	args = append(args, strArr)
	args = append(args, values...)

	return callFuncValue(fn, args, e.scope)
}

// stringsBuilder is a minimal string builder to avoid import conflicts.
type stringsBuilder struct {
	buf []byte
}

func (b *stringsBuilder) WriteByte(c byte) {
	b.buf = append(b.buf, c)
}

func (b *stringsBuilder) String() string {
	return string(b.buf)
}

func (b *stringsBuilder) Reset() {
	b.buf = b.buf[:0]
}

func (e *evaluator) evalFuncCall(fn *Value) *Value {
	e.advance() // skip (

	// Collect args, handling spread operator
	var args []*Value
	for e.peek().t != tokRParen && e.peek().t != tokEOF {
		if e.peek().t == tokSpread {
			e.advance() // skip ...
			arr := e.expr()
			if arr.typ == TypeArray && arr.array != nil {
				args = append(args, arr.array...)
			}
		} else {
			args = append(args, e.expr())
		}
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

	// Custom object methods take priority over built-in handlers.
	// Check object and prototype chain for methods (native + arrow).
	if val.typ == TypeObject {
		fn := lookupMethod(val, method)
		if fn != nil && fn.typ == TypeFunc {
			var args []*Value
			for e.peek().t != tokRParen && e.peek().t != tokEOF {
				if e.peek().t == tokSpread {
					e.advance()
					arr := e.expr()
					if arr.typ == TypeArray && arr.array != nil {
						args = append(args, arr.array...)
					}
				} else {
					args = append(args, e.expr())
				}
				if e.peek().t == tokComma { e.advance() }
			}
			e.expect(tokRParen)
			if fn.native != nil {
				return fn.native(args)
			}
			if fn.str == "__arrow" {
				// Only pass 'this' binding — arrow's af.scope has the module scope
				scope := map[string]*Value{"this": val}
				return callArrow(int(fn.num), args, scope)
			}
			if fn.fnBody != "" {
				// fnBody function (from ExtractFunctions)
				props := make(map[string]*Value, len(args)+1)
				props["this"] = val
				if len(fn.fnParams) > 0 && len(args) > 0 {
					paramStr := fn.fnParams[0]
					if strings.Contains(paramStr, ",") {
						params := strings.Split(paramStr, ",")
						for i, p := range params {
							p = strings.TrimSpace(p)
							if p != "" && i < len(args) {
								props[p] = args[i]
							}
						}
					} else if paramStr != "" && len(args) > 0 {
						props[paramStr] = args[0]
					}
				}
				return e.callFunc(fn, props)
			}
			return Undefined
		}
	}
	// Static methods on class constructors (TypeFunc with object map)
	if val.typ == TypeFunc && val.object != nil {
		if fn, ok := val.object[method]; ok && fn.typ == TypeFunc {
			var args []*Value
			for e.peek().t != tokRParen && e.peek().t != tokEOF {
				if e.peek().t == tokSpread {
					e.advance()
					arr := e.expr()
					if arr.typ == TypeArray && arr.array != nil {
						args = append(args, arr.array...)
					}
				} else {
					args = append(args, e.expr())
				}
				if e.peek().t == tokComma { e.advance() }
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
	case "forEach":
		return e.evalForEach(val)
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
			if rd := getRegexpData(arg); rd != nil {
				parts := rd.Re.Split(val.str, -1)
				arr := make([]*Value, len(parts))
				for i, p := range parts {
					arr[i] = newStr(p)
				}
				return newArr(arr)
			}
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
		// Check if object has a custom toString method first
		if val.typ == TypeObject && val.object != nil {
			if fn, ok := val.object["toString"]; ok && fn.typ == TypeFunc {
				var args []*Value
				for e.peek().t != tokRParen && e.peek().t != tokEOF {
					args = append(args, e.expr())
					if e.peek().t == tokComma { e.advance() }
				}
				e.expect(tokRParen)
				return callFuncValue(fn, args, e.scope)
			}
		}
		// Number.toString(radix) support
		if val.typ == TypeNumber {
			var radix int
			if e.peek().t != tokRParen {
				radix = int(e.expr().toNum())
			}
			e.expect(tokRParen)
			if radix > 0 && radix != 10 {
				return newStr(strconv.FormatInt(int64(val.num), radix))
			}
		} else {
			e.expect(tokRParen)
		}
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
			if rd := getRegexpData(search); rd != nil {
				repl := jsReplacementToGo(replacement.toStr())
				if rd.Global {
					return newStr(rd.Re.ReplaceAllString(val.str, repl))
				}
				count := 0
				result := rd.Re.ReplaceAllStringFunc(val.str, func(match string) string {
					if count > 0 {
						return match
					}
					count++
					return rd.Re.ReplaceAllString(match, repl)
				})
				return newStr(result)
			}
			return newStr(strings.Replace(val.str, search.toStr(), replacement.toStr(), 1))
		}
		return val
	case "replaceAll":
		search := e.expr()
		e.expect(tokComma)
		replacement := e.expr()
		e.expect(tokRParen)
		if val.typ == TypeString {
			if rd := getRegexpData(search); rd != nil {
				repl := jsReplacementToGo(replacement.toStr())
				return newStr(rd.Re.ReplaceAllString(val.str, repl))
			}
			return newStr(strings.ReplaceAll(val.str, search.toStr(), replacement.toStr()))
		}
		return val
	case "match":
		arg := e.expr()
		e.expect(tokRParen)
		if val.typ == TypeString {
			if rd := getRegexpData(arg); rd != nil {
				if rd.Global {
					matches := rd.Re.FindAllString(val.str, -1)
					if matches == nil {
						return Null
					}
					arr := make([]*Value, len(matches))
					for i, m := range matches {
						arr[i] = newStr(m)
					}
					return newArr(arr)
				}
				m := rd.Re.FindStringSubmatch(val.str)
				if m == nil {
					return Null
				}
				arr := make([]*Value, len(m))
				for i, v := range m {
					arr[i] = newStr(v)
				}
				return newArr(arr)
			}
			// String arg: create regexp from string and find all matches
			if arg.typ == TypeString {
				re, err := regexp.Compile(arg.str)
				if err != nil {
					return Null
				}
				matches := re.FindAllString(val.str, -1)
				if matches == nil {
					return Null
				}
				arr := make([]*Value, len(matches))
				for i, m := range matches {
					arr[i] = newStr(m)
				}
				return newArr(arr)
			}
		}
		return Null
	case "search":
		arg := e.expr()
		e.expect(tokRParen)
		if val.typ == TypeString {
			if rd := getRegexpData(arg); rd != nil {
				loc := rd.Re.FindStringIndex(val.str)
				if loc == nil {
					return newNum(-1)
				}
				return newNum(float64(loc[0]))
			}
		}
		return newNum(-1)
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
		runes := []rune(val.toStr())
		if idx >= 0 && idx < len(runes) {
			return newStr(string(runes[idx]))
		}
		return internStr("")
	case "charCodeAt":
		idx := 0
		if e.peek().t != tokRParen {
			idx = int(e.expr().toNum())
		}
		e.expect(tokRParen)
		runes := []rune(val.toStr())
		if idx >= 0 && idx < len(runes) {
			return newNum(float64(runes[idx]))
		}
		return newNum(math.NaN())
	case "codePointAt":
		idx := 0
		if e.peek().t != tokRParen {
			idx = int(e.expr().toNum())
		}
		e.expect(tokRParen)
		runes := []rune(val.toStr())
		if idx >= 0 && idx < len(runes) {
			return newNum(float64(runes[idx]))
		}
		return Undefined
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
			last := val.array[len(val.array)-1]
			val.array = val.array[:len(val.array)-1]
			return last
		}
		return Undefined
	case "shift":
		e.expect(tokRParen)
		if val.typ == TypeArray && len(val.array) > 0 {
			first := val.array[0]
			val.array = val.array[1:]
			return first
		}
		return Undefined
	case "unshift":
		var items []*Value
		for e.peek().t != tokRParen && e.peek().t != tokEOF {
			items = append(items, e.expr())
			if e.peek().t == tokComma { e.advance() }
		}
		e.expect(tokRParen)
		if val.typ == TypeArray {
			val.array = append(items, val.array...)
		}
		return newNum(float64(len(val.array)))
	case "splice":
		var args []*Value
		for e.peek().t != tokRParen && e.peek().t != tokEOF {
			args = append(args, e.expr())
			if e.peek().t == tokComma { e.advance() }
		}
		e.expect(tokRParen)
		if val.typ != TypeArray { return NewArr(nil) }
		start := 0
		deleteCount := len(val.array)
		if len(args) > 0 {
			start = int(args[0].toNum())
			if start < 0 { start = len(val.array) + start }
			if start < 0 { start = 0 }
			if start > len(val.array) { start = len(val.array) }
		}
		if len(args) > 1 {
			deleteCount = int(args[1].toNum())
			if deleteCount < 0 { deleteCount = 0 }
			if start+deleteCount > len(val.array) { deleteCount = len(val.array) - start }
		} else {
			deleteCount = len(val.array) - start
		}
		// Collect removed elements
		removed := make([]*Value, deleteCount)
		copy(removed, val.array[start:start+deleteCount])
		// Build new items to insert
		var insertItems []*Value
		if len(args) > 2 { insertItems = args[2:] }
		// Rebuild array: before + insertItems + after
		newArr := make([]*Value, 0, len(val.array)-deleteCount+len(insertItems))
		newArr = append(newArr, val.array[:start]...)
		newArr = append(newArr, insertItems...)
		newArr = append(newArr, val.array[start+deleteCount:]...)
		val.array = newArr
		return NewArr(removed)
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

	// ── Function methods ────────────────────────────────────
	case "bind":
		if val.typ == TypeFunc {
			thisArg := Undefined
			if e.peek().t != tokRParen {
				thisArg = e.expr()
			}
			e.expect(tokRParen)
			// Capture the original function value
			origFn := val
			boundThis := thisArg
			return NewNativeFunc(func(args []*Value) *Value {
				return e.callFuncWithThis(origFn, boundThis, args)
			})
		}
		e.expect(tokRParen)
		return Undefined
	case "call":
		if val.typ == TypeFunc {
			thisArg := Undefined
			if e.peek().t != tokRParen {
				thisArg = e.expr()
			}
			var args []*Value
			for e.peek().t == tokComma {
				e.advance()
				args = append(args, e.expr())
			}
			e.expect(tokRParen)
			return e.callFuncWithThis(val, thisArg, args)
		}
		e.expect(tokRParen)
		return Undefined
	case "apply":
		if val.typ == TypeFunc {
			thisArg := Undefined
			if e.peek().t != tokRParen {
				thisArg = e.expr()
			}
			var args []*Value
			if e.peek().t == tokComma {
				e.advance()
				argsArr := e.expr()
				if argsArr.typ == TypeArray {
					args = argsArr.array
				}
			}
			e.expect(tokRParen)
			return e.callFuncWithThis(val, thisArg, args)
		}
		e.expect(tokRParen)
		return Undefined

	default:
		// Check if method is a callable property on the object
		if val.typ == TypeObject {
			fn := lookupMethod(val, method)
			if fn != nil && fn.typ == TypeFunc {
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
					// Bind this to the object for arrow calls
					savedThis := e.scope["this"]
					e.scope["this"] = val
					result := callArrow(int(fn.num), args, e.scope)
					if savedThis != nil {
						e.scope["this"] = savedThis
					} else {
						delete(e.scope, "this")
					}
					return result
				}
				// Regular function with body — bind this
				if fn.fnBody != "" {
					childScope := make(map[string]*Value, len(e.scope)+len(fn.fnParams)+1)
					for k, v := range e.scope {
						childScope[k] = v
					}
					childScope["this"] = val
					if len(fn.fnParams) > 0 {
						params := strings.Split(fn.fnParams[0], ",")
						for i, p := range params {
							p = strings.TrimSpace(p)
							if p != "" && i < len(args) {
								childScope[p] = args[i]
							}
						}
					}
					bodyTokens := tokenizeCached(fn.fnBody)
					childEval := &evaluator{tokens: bodyTokens, pos: 0, scope: childScope, thisVal: val}
					result := childEval.evalStatements()
					if result == nil { return Undefined }
					return result
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

	// Check if callback is a function expression or scope reference
	if e.peek().t == tokIdent && e.peek().v == "function" || (e.peek().t == tokIdent && e.peek().v != "function" && (e.pos+1 >= len(e.tokens) || e.tokens[e.pos+1].t != tokArrow)) {
		fn := e.expr()
		e.expect(tokRParen)
		for i, item := range val.array {
			result := callFuncValue(fn, []*Value{item, internNum(float64(i))}, e.scope)
			if result.truthy() {
				return item
			}
		}
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

// evalSomeEvery handles array.some/every(item => condition) or .some(function(x){...})
func (e *evaluator) evalSomeEvery(val *Value, method string) *Value {
	if val.typ != TypeArray {
		e.skipBalanced(tokLParen, tokRParen)
		if method == "every" {
			return True
		}
		return False
	}

	// Function expression or scope-bound callback (non-arrow) — evaluate as value, invoke per item
	if e.peek().t == tokIdent && e.peek().v == "function" || (e.peek().t == tokIdent && e.peek().v != "function" && (e.pos+1 >= len(e.tokens) || e.tokens[e.pos+1].t != tokArrow)) {
		fn := e.expr()
		e.expect(tokRParen)
		for i, item := range val.array {
			result := callFuncValue(fn, []*Value{item, internNum(float64(i)), val}, e.scope)
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

// evalForEach handles array.forEach((item, index) => { ... })
func (e *evaluator) evalForEach(val *Value) *Value {
	if val.typ != TypeArray {
		for e.peek().t != tokRParen && e.peek().t != tokEOF {
			e.advance()
		}
		e.expect(tokRParen)
		return Undefined
	}

	// Function expression or scope-bound callback (non-arrow) — evaluate as value, invoke per item
	if e.peek().t == tokIdent && e.peek().v == "function" || (e.peek().t == tokIdent && e.peek().v != "function" && (e.pos+1 >= len(e.tokens) || e.tokens[e.pos+1].t != tokArrow)) {
		fn := e.expr()
		e.expect(tokRParen)
		for i, item := range val.array {
			callFuncValue(fn, []*Value{item, internNum(float64(i)), val}, e.scope)
		}
		return Undefined
	}

	params := e.parseArrowParams()
	e.expect(tokArrow)

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
	e.expect(tokRParen)

	rawBody := make([]tok, bodyEnd-bodyStart)
	copy(rawBody, e.tokens[bodyStart:bodyEnd])

	isExprBody := !hasBodyBrace
	if hasBodyBrace && len(rawBody) >= 2 && rawBody[0].t == tokLBrace {
		rawBody = rawBody[1 : len(rawBody)-1]
	} else if hasBodyParen && len(rawBody) >= 2 && rawBody[0].t == tokLParen {
		rawBody = rawBody[1 : len(rawBody)-1]
	}
	bodyTokens := append(rawBody, tok{t: tokEOF})

	if isExprBody && len(params) <= 2 {
		ev := &evaluator{tokens: bodyTokens, pos: 0, scope: e.scope}

		var savedVars []struct {
			name string
			val  *Value
			has  bool
		}
		if len(params) > 0 {
			v, ok := e.scope[params[0]]
			savedVars = append(savedVars, struct {
				name string
				val  *Value
				has  bool
			}{params[0], v, ok})
		}
		if len(params) > 1 {
			v, ok := e.scope[params[1]]
			savedVars = append(savedVars, struct {
				name string
				val  *Value
				has  bool
			}{params[1], v, ok})
		}

		for i, item := range val.array {
			if len(params) > 0 {
				e.scope[params[0]] = item
			}
			if len(params) > 1 {
				e.scope[params[1]] = internNum(float64(i))
			}
			ev.pos = 0
			ev.expr()
		}

		for _, sv := range savedVars {
			if sv.has {
				e.scope[sv.name] = sv.val
			} else {
				delete(e.scope, sv.name)
			}
		}
		return Undefined
	}

	// General path: block bodies — use parent scope so side effects are visible
	var savedVars []struct {
		name string
		val  *Value
		has  bool
	}
	if len(params) > 0 {
		v, ok := e.scope[params[0]]
		savedVars = append(savedVars, struct {
			name string
			val  *Value
			has  bool
		}{params[0], v, ok})
	}
	if len(params) > 1 {
		v, ok := e.scope[params[1]]
		savedVars = append(savedVars, struct {
			name string
			val  *Value
			has  bool
		}{params[1], v, ok})
	}

	for i, item := range val.array {
		if len(params) > 0 {
			e.scope[params[0]] = item
		}
		if len(params) > 1 {
			e.scope[params[1]] = internNum(float64(i))
		}
		bt := make([]tok, len(bodyTokens))
		copy(bt, bodyTokens)
		childEval := &evaluator{tokens: bt, pos: 0, scope: e.scope}
		childEval.evalStatements()
	}

	for _, sv := range savedVars {
		if sv.has {
			e.scope[sv.name] = sv.val
		} else {
			delete(e.scope, sv.name)
		}
	}

	return Undefined
}

// callFuncWithThis calls a function value with a specific this binding and arguments.
func (e *evaluator) callFuncWithThis(fn *Value, thisArg *Value, args []*Value) *Value {
	if fn.native != nil {
		return fn.native(args)
	}
	if fn.str == "__arrow" {
		savedThis := e.scope["this"]
		e.scope["this"] = thisArg
		result := callArrow(int(fn.num), args, e.scope)
		if savedThis != nil {
			e.scope["this"] = savedThis
		} else {
			delete(e.scope, "this")
		}
		return result
	}
	if fn.fnBody != "" {
		childScope := make(map[string]*Value, len(e.scope)+len(fn.fnParams)+1)
		for k, v := range e.scope {
			childScope[k] = v
		}
		childScope["this"] = thisArg
		if len(fn.fnParams) > 0 {
			fnParamList := strings.Split(fn.fnParams[0], ",")
			for i, p := range fnParamList {
				p = strings.TrimSpace(p)
				if p != "" && i < len(args) {
					childScope[p] = args[i]
				}
			}
		}
		bodyToks := tokenizeCached(fn.fnBody)
		childEval := &evaluator{tokens: bodyToks, pos: 0, scope: childScope, thisVal: thisArg}
		result := childEval.evalStatements()
		if result == nil {
			return Undefined
		}
		return result
	}
	return Undefined
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
				e.advance() // skip [
				var names []string
				for e.peek().t != tokRBrack && e.peek().t != tokEOF {
					if e.peek().t == tokIdent { names = append(names, e.advance().v) }
					if e.peek().t == tokComma { e.advance() }
				}
				if e.peek().t == tokRBrack { e.advance() }
				params = append(params, "__destructure__:"+strings.Join(names, ","))
			} else if e.peek().t == tokLBrace {
				// Object destructuring: ({x, y}) => ...
				e.advance() // skip {
				var names []string
				for e.peek().t != tokRBrace && e.peek().t != tokEOF {
					if e.peek().t == tokIdent {
						name := e.advance().v
						// Handle rename: {x: y} — skip the colon and alias
						if e.peek().t == tokColon { e.advance(); if e.peek().t == tokIdent { name = e.advance().v } }
						// Handle default: {x = 10} — skip for now
						if e.peek().t == tokAssign { e.advance(); e.expr() }
						names = append(names, name)
					} else { e.advance() }
					if e.peek().t == tokComma { e.advance() }
				}
				if e.peek().t == tokRBrace { e.advance() }
				params = append(params, "__obj_destructure__:"+strings.Join(names, ","))
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
	// Control-flow statements can appear as the single body of an if/else/
	// while/for — they are *statements*, not expressions. Route them through
	// evalStatements (which has full for/while/if handlers) so the for-loop
	// actually executes instead of being swallowed by e.expr() as a no-op.
	if e.peek().t == tokIdent {
		kw := e.peek().v
		if kw == "for" || kw == "while" || kw == "if" || kw == "switch" || kw == "do" ||
			kw == "const" || kw == "let" || kw == "var" || kw == "try" || kw == "throw" {
			// Build a single-statement evaluator over the remaining tokens so we
			// can reuse evalStatements' dispatch. It stops at the first
			// end-of-statement since we drive a fresh evaluator on a sub-slice.
			bodyStart := e.pos
			e.skipSingleStatement()
			bodyEnd := e.pos
			bodyToks := make([]tok, bodyEnd-bodyStart+1)
			copy(bodyToks, e.tokens[bodyStart:bodyEnd])
			bodyToks[len(bodyToks)-1] = tok{t: tokEOF}
			sub := &evaluator{tokens: bodyToks, pos: 0, scope: e.scope}
			return sub.evalStatements()
		}
	}
	// Not a return or statement keyword — evaluate and discard.
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

	case tokRegExp:
		e.advance()
		parts := strings.SplitN(t.v, "\x00", 2)
		pattern := parts[0]
		flags := ""
		if len(parts) > 1 {
			flags = parts[1]
		}
		return newRegexpValue(pattern, flags)

	case tokLParen:
		// Check if this is an arrow function: () => ... or (x) => ... or (x, y) => ...
		if e.isArrowFunction() {
			return e.parseArrowFunction()
		}
		e.advance()
		val := e.expr()
		// Comma operator: (expr1, expr2, ..., exprN) → returns last value
		for e.peek().t == tokComma {
			e.advance()
			val = e.expr()
		}
		e.expect(tokRParen)
		return val

	case tokLBrack:
		return e.parseArray()

	case tokLBrace:
		return e.parseObject()

	case tokIdent:
		// Single-param arrow: name => expr  or  name => { ... }
		if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokArrow {
			paramName := e.advance().v // consume ident
			e.advance()                // consume =>
			params := []string{paramName}
			var bodyToks []tok
			isBlock := false
			if e.peek().t == tokLBrace {
				isBlock = true
				start := e.pos
				e.skipBalanced(tokLBrace, tokRBrace)
				if e.pos-start > 2 {
					bodyToks = make([]tok, e.pos-start-2)
					copy(bodyToks, e.tokens[start+1:e.pos-1])
				}
			} else {
				start := e.pos
				depth := 0
				for e.pos < len(e.tokens) && e.tokens[e.pos].t != tokEOF {
					tt := e.tokens[e.pos].t
					if tt == tokLParen || tt == tokLBrack || tt == tokLBrace { depth++ }
					if tt == tokRParen || tt == tokRBrack || tt == tokRBrace {
						if depth == 0 { break }
						depth--
					}
					if depth == 0 && (tt == tokComma || tt == tokSemi) { break }
					e.pos++
				}
				bodyToks = make([]tok, e.pos-start)
				copy(bodyToks, e.tokens[start:e.pos])
			}
			bodyToks = append(bodyToks, tok{t: tokEOF})
			af := &arrowFunc{params: params, tokens: bodyToks, isBlock: isBlock, scope: e.scope}
			return &Value{typ: TypeFunc, str: "__arrow", num: float64(registerArrow(af))}
		}
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
			// new Promise((resolve, reject) => { ... })
			// new Map() / new Set()
			if ctor == "Map" {
				var initArg *Value
				if e.peek().t == tokLParen {
					e.advance()
					if e.peek().t != tokRParen {
						initArg = e.expr()
						for e.peek().t == tokComma { e.advance(); e.expr() }
					}
					e.expect(tokRParen)
				}
				return newMapValue(initArg)
			}
			if ctor == "Set" {
				// Parse optional initial values
				var initArr *Value
				if e.peek().t == tokLParen {
					e.advance()
					if e.peek().t != tokRParen { initArr = e.expr() }
					e.expect(tokRParen)
				}
				return newSetValue(initArr)
			}
			if ctor == "WeakMap" {
				if e.peek().t == tokLParen {
					e.advance()
					for e.peek().t != tokRParen && e.peek().t != tokEOF { e.advance() }
					e.expect(tokRParen)
				}
				return newWeakMapValue()
			}
			if ctor == "WeakSet" {
				if e.peek().t == tokLParen {
					e.advance()
					for e.peek().t != tokRParen && e.peek().t != tokEOF { e.advance() }
					e.expect(tokRParen)
				}
				return newWeakSetValue()
			}
			if ctor == "AbortController" {
				if e.peek().t == tokLParen {
					e.advance()
					e.expect(tokRParen)
				}
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
			}
			if ctor == "Proxy" && e.peek().t == tokLParen {
				e.advance() // skip (
				target := e.expr()
				if e.peek().t == tokComma { e.advance() }
				handler := e.expr()
				e.expect(tokRParen)
				return newProxyValue(target, handler, e.scope)
			}
			if (ctor == "Event" || ctor == "CustomEvent") && e.peek().t == tokLParen {
			e.advance() // skip (
			eventType := ""
			if e.peek().t != tokRParen {
				eventType = e.expr().toStr()
			}
			bubbles := false
			cancelable := false
			var detail *Value
			if e.peek().t == tokComma {
				e.advance()
				opts := e.expr()
				if opts.typ == TypeObject && opts.object != nil {
					if b, ok := opts.object["bubbles"]; ok { bubbles = b.truthy() }
					if c, ok := opts.object["cancelable"]; ok { cancelable = c.truthy() }
					if d, ok := opts.object["detail"]; ok { detail = d }
				}
			}
			e.expect(tokRParen)

			prevented := false
			stopped := false
			ev := newObj(map[string]*Value{
				"type":       newStr(eventType),
				"bubbles":    newBool(bubbles),
				"cancelable": newBool(cancelable),
			})
			ev.object["stopPropagation"] = NewNativeFunc(func(args []*Value) *Value {
				stopped = true; _ = stopped; return Undefined
			})
			ev.object["stopImmediatePropagation"] = NewNativeFunc(func(args []*Value) *Value {
				stopped = true; return Undefined
			})
			ev.object["preventDefault"] = NewNativeFunc(func(args []*Value) *Value {
				prevented = true; return Undefined
			})
			ev.getset = map[string]*PropDescriptor{
				"defaultPrevented": {Get: NewNativeFunc(func(args []*Value) *Value {
					return newBool(prevented)
				})},
			}
			if ctor == "CustomEvent" && detail != nil {
				ev.object["detail"] = detail
			}
			return ev
		}
		if ctor == "Promise" && e.peek().t == tokLParen {
				e.advance() // skip (
				// Parse the executor function
				executor := e.expr()
				e.expect(tokRParen)
				p := &promise{state: PromisePending}
				pv := newPromiseValue(p)
				registerPromiseMethods(pv, p, e.scope)
				// Create resolve and reject functions
				resolveFn := NewNativeFunc(func(args []*Value) *Value {
					val := Undefined
					if len(args) > 0 { val = args[0] }
					resolvePromise(p, val, e.scope)
					return Undefined
				})
				rejectFn := NewNativeFunc(func(args []*Value) *Value {
					val := Undefined
					if len(args) > 0 { val = args[0] }
					rejectPromise(p, val, e.scope)
					return Undefined
				})
				// Call executor(resolve, reject)
				callFuncValue(executor, []*Value{resolveFn, rejectFn}, e.scope)
				return pv
			}
			// new RegExp(pattern, flags)
			if ctor == "RegExp" && e.peek().t == tokLParen {
				e.advance() // skip (
				pattern := ""
				flags := ""
				if e.peek().t != tokRParen {
					pattern = e.expr().toStr()
					if e.peek().t == tokComma {
						e.advance()
						flags = e.expr().toStr()
					}
				}
				e.expect(tokRParen)
				return newRegexpValue(pattern, flags)
			}
			// new Error/TypeError/RangeError/SyntaxError(message)
			if ctor == "Error" || ctor == "TypeError" || ctor == "RangeError" || ctor == "SyntaxError" {
				msg := ""
				if e.peek().t == tokLParen {
					e.advance()
					if e.peek().t != tokRParen {
						msg = e.expr().toStr()
					}
					e.expect(tokRParen)
				}
				return newError(ctor, msg)
			}
			// Resolve constructor: handle new X.Y.Z(...) chains
			var ctorVal *Value
			if v, ok := e.scope[ctor]; ok {
				ctorVal = v
			}
			// Follow dot-access chain: new X.Y(...)
			for ctorVal != nil && e.peek().t == tokDot {
				e.advance() // skip .
				prop := e.advance().v
				ctorVal = ctorVal.getProp(prop)
			}
			// Call the constructor
			if ctorVal != nil && ctorVal.typ == TypeFunc {
				var args []*Value
				if e.peek().t == tokLParen {
					e.advance()
					for e.peek().t != tokRParen && e.peek().t != tokEOF {
						args = append(args, e.expr())
						if e.peek().t == tokComma {
							e.advance()
						}
					}
					e.expect(tokRParen)
				}
				// Native constructor (from parseClass)
				if ctorVal.native != nil {
					return ctorVal.native(args)
				}
				// JS function used as constructor (new Func(args))
				// Create a new this object and call the function with it
				thisObj := newObj(make(map[string]*Value))
				if ctorVal.str == "__arrow" {
					scope := make(map[string]*Value, len(e.scope)+1)
					for k, v := range e.scope {
						scope[k] = v
					}
					scope["this"] = thisObj
					result := callArrow(int(ctorVal.num), args, scope)
					// If function returns an object, use that (JS new semantics)
					if result != nil && result.typ == TypeObject {
						return result
					}
					return thisObj
				}
				if ctorVal.fnBody != "" {
					props := make(map[string]*Value, len(args)+1)
					props["this"] = thisObj
					if len(ctorVal.fnParams) > 0 && len(args) > 0 {
						paramStr := ctorVal.fnParams[0]
						if strings.Contains(paramStr, ",") {
							params := strings.Split(paramStr, ",")
							for i, p := range params {
								p = strings.TrimSpace(p)
								if p != "" && i < len(args) {
									props[p] = args[i]
								}
							}
						} else if paramStr != "" {
							props[paramStr] = args[0]
						}
					}
					ev := &evaluator{scope: make(map[string]*Value)}
					for k, v := range e.scope {
						ev.scope[k] = v
					}
					for k, v := range props {
						ev.scope[k] = v
					}
					result := ev.callFunc(ctorVal, props)
					if result != nil && result.typ == TypeObject {
						return result
					}
					return thisObj
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
		case "function":
			e.advance() // skip "function"
			// Generator function: function* name() { ... yield ... }
			if e.peek().t == tokStar {
				e.advance() // skip *
				funcName := ""
				if e.peek().t == tokIdent {
					funcName = e.advance().v
				}
				e.expect(tokLParen)
				var params []string
				for e.peek().t != tokRParen && e.peek().t != tokEOF {
					if e.peek().t == tokIdent { params = append(params, e.advance().v) }
					if e.peek().t == tokComma { e.advance() }
				}
				e.expect(tokRParen)
				if e.peek().t == tokLBrace {
					start := e.pos
					e.skipBalanced(tokLBrace, tokRBrace)
					bodyToks := make([]tok, e.pos-start-2)
					copy(bodyToks, e.tokens[start+1:e.pos-1])
					genFn := newGeneratorFunc(params, bodyToks, e.scope)
					if funcName != "" {
						e.scope[funcName] = genFn
					}
					return genFn
				}
				return Undefined
			}
			// Anonymous function expression: function() { ... } or function name() { ... }
			funcName := ""
			if e.peek().t == tokIdent {
				funcName = e.advance().v
			}
			e.expect(tokLParen)
			var params []string
			var restParam string
			for e.peek().t != tokRParen && e.peek().t != tokEOF {
				if e.peek().t == tokSpread {
					e.advance() // skip ...
					if e.peek().t == tokIdent {
						restParam = e.advance().v
					}
				} else if e.peek().t == tokIdent {
					params = append(params, e.advance().v)
				} else {
					// Unknown token — advance to avoid infinite loop
					e.advance()
					continue
				}
				// Skip optional default value: = expr (up to next comma or rparen)
				if e.peek().t == tokAssign {
					e.advance()
					e.skipExpr()
				}
				if e.peek().t == tokComma { e.advance() }
			}
			e.expect(tokRParen)
			// Parse body
			if e.peek().t == tokLBrace {
				start := e.pos
				e.skipBalanced(tokLBrace, tokRBrace)
				bodyToks := make([]tok, e.pos-start-2)
				copy(bodyToks, e.tokens[start+1:e.pos-1])
				bodyToks = append(bodyToks, tok{t: tokEOF})
				allParams := params
				if restParam != "" {
					allParams = append(allParams, "__rest__:"+restParam)
				}
				af := &arrowFunc{params: allParams, tokens: bodyToks, isBlock: true, scope: e.scope}
				fnVal := &Value{typ: TypeFunc, str: "__arrow", num: float64(registerArrow(af))}
				if funcName != "" {
					e.scope[funcName] = fnVal
				}
				return fnVal
			}
			return Undefined
		case "async":
			// async arrow: async (args) => { ... } or async name => { ... }
			e.advance()
			// Parse as normal expression — will be arrow function or function
			innerFn := e.expr()
			if innerFn.typ != TypeFunc {
				return innerFn
			}
			// Wrap the function to return a Promise
			innerCopy := innerFn
			scopeRef := e.scope
			return NewNativeFunc(func(args []*Value) *Value {
				result := callFuncValue(innerCopy, args, scopeRef)
				// If result is already a promise, return it
				if getPromise(result) != nil {
					return result
				}
				// If it's a throw, return rejected promise
				if isThrow(result) {
					return MakeRejectedPromise(thrownValue(result))
				}
				return MakeResolvedPromise(result)
			})
		case "this":
			e.advance()
			if e.thisVal != nil {
				return e.thisVal
			}
			// Fallback: check scope for this (set by external callers)
			if v, ok := e.scope["this"]; ok {
				return v
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
		case "NaN":
			e.advance()
			return newNum(math.NaN())
		case "Infinity":
			e.advance()
			return newNum(math.Inf(1))
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
						// Iterable objects (Set, Map.keys(), etc.) — call values() if available
						if arg.typ == TypeObject && arg.object != nil {
							if valsFn, ok := arg.object["values"]; ok && valsFn.typ == TypeFunc {
								result := callFuncValue(valsFn, nil, e.scope)
								if result.typ == TypeArray {
									return result
								}
							}
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
				// Object.prototype — return an object with hasOwnProperty
				if method.v == "prototype" {
					proto := NewObj(map[string]*Value{
						"hasOwnProperty": NewNativeFunc(func(args []*Value) *Value {
							// This is used via .call(obj, key)
							return NewNativeFunc(func(args []*Value) *Value {
								return Undefined // placeholder, .call handled below
							})
						}),
					})
					// Handle .hasOwnProperty.call(obj, key) chain
					if e.peek().t == tokDot {
						e.advance()
						propName := e.advance().v
						if propName == "hasOwnProperty" && e.peek().t == tokDot {
							e.advance()
							callMethod := e.advance().v
							if callMethod == "call" && e.peek().t == tokLParen {
								e.advance()
								obj := e.expr()
								var key *Value
								if e.peek().t == tokComma {
									e.advance()
									key = e.expr()
								}
								e.expect(tokRParen)
								if key != nil && obj.typ == TypeObject && obj.object != nil {
									_, has := obj.object[key.toStr()]
									return newBool(has)
								}
								return False
							}
						}
					}
					return proto
				}
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
					case "create":
						// Object.create(proto) — create object with prototype
						obj := &Value{typ: TypeObject, object: make(map[string]*Value)}
						if arg.typ == TypeObject || arg.typ == TypeNull {
							if arg.typ != TypeNull {
								obj.proto = arg
							}
						}
						return obj
					case "defineProperty":
						// Object.defineProperty(obj, prop, descriptor)
						if len(extraArgs) >= 2 {
							prop := extraArgs[0].toStr()
							desc := extraArgs[1]
							if desc.typ == TypeObject && desc.object != nil {
								getFn, hasGet := desc.object["get"]
								setFn, hasSet := desc.object["set"]
								if hasGet || hasSet {
									if arg.getset == nil {
										arg.getset = make(map[string]*PropDescriptor)
									}
									pd := &PropDescriptor{}
									if hasGet && getFn.typ == TypeFunc { pd.Get = getFn }
									if hasSet && setFn.typ == TypeFunc { pd.Set = setFn }
									arg.getset[prop] = pd
								} else if valProp, hasVal := desc.object["value"]; hasVal {
									if arg.object == nil {
										arg.object = make(map[string]*Value)
									}
									arg.object[prop] = valProp
								}
							}
						}
						return arg
					case "fromEntries":
						// Object.fromEntries(iterable) — convert [[k,v],...] to object
						obj := &Value{typ: TypeObject, object: make(map[string]*Value)}
						if arg.typ == TypeArray && arg.array != nil {
							for _, entry := range arg.array {
								if entry.typ == TypeArray && len(entry.array) >= 2 {
									obj.object[entry.array[0].toStr()] = entry.array[1]
								}
							}
						}
						return obj
					case "setPrototypeOf":
						// Object.setPrototypeOf(obj, proto)
						if len(extraArgs) >= 1 && arg.typ == TypeObject {
							if extraArgs[0].typ == TypeObject {
								arg.proto = extraArgs[0]
							} else if extraArgs[0].typ == TypeNull {
								arg.proto = nil
							}
						}
						return arg
					case "getOwnPropertyDescriptor":
						// Object.getOwnPropertyDescriptor(obj, prop)
						if len(extraArgs) >= 1 {
							prop := extraArgs[0].toStr()
							if arg.typ == TypeObject || arg.typ == TypeFunc {
								// Check getters/setters first
								if arg.getset != nil {
									if desc, ok := arg.getset[prop]; ok {
										d := NewObj(map[string]*Value{
											"enumerable":   True,
											"configurable": True,
										})
										if desc.Get != nil {
											d.object["get"] = desc.Get
										}
										if desc.Set != nil {
											d.object["set"] = desc.Set
										}
										return d
									}
								}
								// Regular property
								if arg.object != nil {
									if val, ok := arg.object[prop]; ok {
										return NewObj(map[string]*Value{
											"value":        val,
											"writable":     True,
											"enumerable":   True,
											"configurable": True,
										})
									}
								}
							}
						}
						return Undefined
					case "getOwnPropertyNames":
						// Object.getOwnPropertyNames(obj) — all own string-keyed props
						names := []*Value{}
						if arg.typ == TypeObject || arg.typ == TypeFunc {
							if arg.object != nil {
								for k := range arg.object {
									names = append(names, newStr(k))
								}
							}
							if arg.getset != nil {
								for k := range arg.getset {
									// Only add if not already in object
									if arg.object == nil {
										names = append(names, newStr(k))
									} else if _, ok := arg.object[k]; !ok {
										names = append(names, newStr(k))
									}
								}
							}
						}
						return newArr(names)
					case "hasOwn":
						// Object.hasOwn(obj, prop)
						if len(extraArgs) >= 1 {
							prop := extraArgs[0].toStr()
							if arg.typ == TypeObject || arg.typ == TypeFunc {
								if arg.object != nil {
									if _, ok := arg.object[prop]; ok {
										return True
									}
								}
								if arg.getset != nil {
									if _, ok := arg.getset[prop]; ok {
										return True
									}
								}
							}
						}
						return False
					case "getPrototypeOf":
						if arg.proto != nil {
							return arg.proto
						}
						return Null
					}
				}
			}
			return Undefined
		case "Promise":
			e.advance()
			if e.peek().t == tokDot {
				e.advance()
				method := e.advance()
				if e.peek().t == tokLParen {
					e.advance()
					switch method.v {
					case "resolve":
						val := Undefined
						if e.peek().t != tokRParen {
							val = e.expr()
						}
						e.expect(tokRParen)
						return MakeResolvedPromise(val)
					case "reject":
						val := Undefined
						if e.peek().t != tokRParen {
							val = e.expr()
						}
						e.expect(tokRParen)
						return MakeRejectedPromise(val)
					case "all":
						arr := e.expr()
						e.expect(tokRParen)
						if arr.typ == TypeArray {
							results := make([]*Value, len(arr.array))
							allResolved := true
							for i, item := range arr.array {
								if p := getPromise(item); p != nil {
									p.mu.Lock()
									if p.state == PromiseFulfilled {
										results[i] = p.value
									} else {
										allResolved = false
									}
									p.mu.Unlock()
								} else {
									results[i] = item
								}
							}
							if allResolved {
								return MakeResolvedPromise(newArr(results))
							}
							// Return pending promise for truly async case
							pv, resolve, _ := MakePromise()
							_ = resolve // will be called when all resolve
							return pv
						}
						return MakeResolvedPromise(newArr(nil))
					}
					// Unknown method — skip args
					for e.peek().t != tokRParen && e.peek().t != tokEOF {
						e.expr()
						if e.peek().t == tokComma { e.advance() }
					}
					e.expect(tokRParen)
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
					switch method.v {
					case "stringify":
						// Accept the JS-standard signature
						// JSON.stringify(value, replacer, indent). Replacer
						// is ignored; indent accepts a number (N spaces) or
						// a string token.
						indent := ""
						if e.peek().t == tokComma {
							e.advance()
							_ = e.expr() // replacer
							if e.peek().t == tokComma {
								e.advance()
								third := e.expr()
								switch third.typ {
								case TypeNumber:
									n := int(third.num)
									if n > 10 {
										n = 10
									}
									if n > 0 {
										indent = strings.Repeat(" ", n)
									}
								case TypeString:
									s := third.toStr()
									if len(s) > 10 {
										s = s[:10]
									}
									indent = s
								}
							}
						}
						e.expect(tokRParen)
						iface := valueToInterface(arg)
						var b []byte
						if indent != "" {
							b, _ = json.MarshalIndent(iface, "", indent)
						} else {
							b, _ = json.Marshal(iface)
						}
						return newStr(string(b))
					case "parse":
						e.expect(tokRParen)
						return jsonToValue(json.RawMessage(arg.toStr()))
					}
				}
			}
			return Undefined
		case "Number":
			e.advance()
			if e.peek().t == tokDot {
				e.advance()
				method := e.advance().v
				switch method {
				case "isInteger":
					if e.peek().t == tokLParen { e.advance() }
					arg := e.expr()
					e.expect(tokRParen)
					n := arg.toNum()
					return newBool(n == float64(int64(n)) && !math.IsInf(n, 0) && !math.IsNaN(n))
				case "isNaN":
					if e.peek().t == tokLParen { e.advance() }
					arg := e.expr()
					e.expect(tokRParen)
					return newBool(arg.typ == TypeNumber && math.IsNaN(arg.num))
				case "isFinite":
					if e.peek().t == tokLParen { e.advance() }
					arg := e.expr()
					e.expect(tokRParen)
					n := arg.toNum()
					return newBool(!math.IsInf(n, 0) && !math.IsNaN(n))
				case "parseInt":
					if e.peek().t == tokLParen { e.advance() }
					arg := e.expr()
					if e.peek().t == tokComma { e.advance(); e.expr() }
					e.expect(tokRParen)
					n, err := strconv.ParseInt(strings.TrimSpace(arg.toStr()), 10, 64)
					if err != nil { return internNum(0) }
					return newNum(float64(n))
				case "parseFloat":
					if e.peek().t == tokLParen { e.advance() }
					arg := e.expr()
					e.expect(tokRParen)
					n, err := strconv.ParseFloat(strings.TrimSpace(arg.toStr()), 64)
					if err != nil { return internNum(0) }
					return newNum(n)
				case "MAX_SAFE_INTEGER":
					return newNum(9007199254740991)
				case "MIN_SAFE_INTEGER":
					return newNum(-9007199254740991)
				case "EPSILON":
					return newNum(2.220446049250313e-16)
				}
				return Undefined
			}
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
		case "isNaN":
			e.advance()
			if e.peek().t == tokLParen {
				e.advance()
				arg := e.expr()
				e.expect(tokRParen)
				// isNaN coerces to number: undefined → NaN, "hello" → NaN
				if arg.typ == TypeUndefined { return True }
				if arg.typ == TypeNumber { return newBool(math.IsNaN(arg.num)) }
				if arg.typ == TypeString {
					_, err := strconv.ParseFloat(strings.TrimSpace(arg.str), 64)
					return newBool(err != nil)
				}
				return False
			}
			return &Value{typ: TypeFunc, str: "isNaN"}
		case "isFinite":
			e.advance()
			if e.peek().t == tokLParen {
				e.advance()
				arg := e.expr()
				e.expect(tokRParen)
				n := arg.toNum()
				return newBool(!math.IsInf(n, 0) && !math.IsNaN(n))
			}
			return &Value{typ: TypeFunc, str: "isFinite"}
		case "encodeURIComponent":
			e.advance()
			if e.peek().t == tokLParen {
				e.advance()
				arg := e.expr()
				e.expect(tokRParen)
				return newStr(url.QueryEscape(arg.toStr()))
			}
			return &Value{typ: TypeFunc, str: "encodeURIComponent"}
		case "decodeURIComponent":
			e.advance()
			if e.peek().t == tokLParen {
				e.advance()
				arg := e.expr()
				e.expect(tokRParen)
				decoded, err := url.QueryUnescape(arg.toStr())
				if err != nil { return newStr(arg.toStr()) }
				return newStr(decoded)
			}
			return &Value{typ: TypeFunc, str: "decodeURIComponent"}
		case "encodeURI":
			e.advance()
			if e.peek().t == tokLParen {
				e.advance()
				arg := e.expr()
				e.expect(tokRParen)
				// encodeURI doesn't encode :/?#[]@!$&'()*+,;=
				s := arg.toStr()
				s = url.QueryEscape(s)
				for _, pair := range [][2]string{{"%3A", ":"}, {"%2F", "/"}, {"%3F", "?"}, {"%23", "#"}, {"%5B", "["}, {"%5D", "]"}, {"%40", "@"}, {"%21", "!"}, {"%24", "$"}, {"%26", "&"}, {"%27", "'"}, {"%28", "("}, {"%29", ")"}, {"%2A", "*"}, {"%2B", "+"}, {"%2C", ","}, {"%3B", ";"}, {"%3D", "="}} {
					s = strings.ReplaceAll(s, pair[0], pair[1])
				}
				return newStr(s)
			}
			return &Value{typ: TypeFunc, str: "encodeURI"}
		case "decodeURI":
			e.advance()
			if e.peek().t == tokLParen {
				e.advance()
				arg := e.expr()
				e.expect(tokRParen)
				decoded, err := url.QueryUnescape(arg.toStr())
				if err != nil { return newStr(arg.toStr()) }
				return newStr(decoded)
			}
			return &Value{typ: TypeFunc, str: "decodeURI"}
		case "queueMicrotask":
			e.advance()
			if e.peek().t == tokLParen {
				e.advance()
				fn := e.expr()
				e.expect(tokRParen)
				// Execute synchronously (no real microtask queue yet)
				callFuncValue(fn, nil, e.scope)
				return Undefined
			}
			return &Value{typ: TypeFunc, str: "queueMicrotask"}
		case "requestIdleCallback":
			e.advance()
			if e.peek().t == tokLParen {
				e.advance()
				fn := e.expr()
				// Skip optional options arg
				if e.peek().t == tokComma { e.advance(); e.expr() }
				e.expect(tokRParen)
				// Execute synchronously (stub — real browsers defer this)
				deadline := newObj(map[string]*Value{
					"timeRemaining": NewNativeFunc(func(args []*Value) *Value { return newNum(50) }),
					"didTimeout": False,
				})
				callFuncValue(fn, []*Value{deadline}, e.scope)
				return newNum(1) // fake ID
			}
			return &Value{typ: TypeFunc, str: "requestIdleCallback"}
		case "cancelIdleCallback":
			e.advance()
			if e.peek().t == tokLParen {
				e.advance()
				e.expr() // consume ID
				e.expect(tokRParen)
			}
			return Undefined
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
				// Constants (no parens)
				switch method {
				case "PI": return newNum(math.Pi)
				case "E": return newNum(math.E)
				case "LN2": return newNum(math.Ln2)
				case "LN10": return newNum(math.Log(10))
				case "LOG2E": return newNum(math.Log2E)
				case "LOG10E": return newNum(math.Log10E)
				case "SQRT2": return newNum(math.Sqrt2)
				case "SQRT1_2": return newNum(1 / math.Sqrt2)
				}
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
						return newNum(rand.Float64())
					case "pow":
						if e.peek().t == tokComma { e.advance() }
						b := e.expr().toNum()
						e.expect(tokRParen)
						return newNum(math.Pow(n, b))
					case "sqrt":
						e.expect(tokRParen)
						return newNum(math.Sqrt(n))
					case "log":
						e.expect(tokRParen)
						return newNum(math.Log(n))
					case "log2":
						e.expect(tokRParen)
						return newNum(math.Log2(n))
					case "log10":
						e.expect(tokRParen)
						return newNum(math.Log10(n))
					case "sin":
						e.expect(tokRParen)
						return newNum(math.Sin(n))
					case "cos":
						e.expect(tokRParen)
						return newNum(math.Cos(n))
					case "tan":
						e.expect(tokRParen)
						return newNum(math.Tan(n))
					case "atan2":
						if e.peek().t == tokComma { e.advance() }
						b := e.expr().toNum()
						e.expect(tokRParen)
						return newNum(math.Atan2(n, b))
					case "trunc":
						e.expect(tokRParen)
						return newNum(math.Trunc(n))
					case "sign":
						e.expect(tokRParen)
						if n > 0 { return internNum(1) }
						if n < 0 { return internNum(-1) }
						return internNum(0)
					case "cbrt":
						e.expect(tokRParen)
						return newNum(math.Cbrt(n))
					case "hypot":
						if e.peek().t == tokComma { e.advance() }
						b := e.expr().toNum()
						e.expect(tokRParen)
						return newNum(math.Hypot(n, b))
					case "exp":
						e.expect(tokRParen)
						return newNum(math.Exp(n))
					case "clz32":
						e.expect(tokRParen)
						u := uint32(n)
						count := 0
						for i := 31; i >= 0; i-- {
							if u & (1 << uint(i)) != 0 { break }
							count++
						}
						return internNum(float64(count))
					}
				}
			}
			return Undefined
		case "String":
			e.advance()
			if e.peek().t == tokDot {
				e.advance()
				method := e.advance().v
				if method == "fromCharCode" && e.peek().t == tokLParen {
					e.advance()
					var runes []rune
					for e.peek().t != tokRParen && e.peek().t != tokEOF {
						runes = append(runes, rune(e.expr().toNum()))
						if e.peek().t == tokComma { e.advance() }
					}
					e.expect(tokRParen)
					return newStr(string(runes))
				}
				if method == "fromCodePoint" && e.peek().t == tokLParen {
					e.advance()
					var runes []rune
					for e.peek().t != tokRParen && e.peek().t != tokEOF {
						runes = append(runes, rune(e.expr().toNum()))
						if e.peek().t == tokComma { e.advance() }
					}
					e.expect(tokRParen)
					return newStr(string(runes))
				}
				return Undefined
			}
			if e.peek().t == tokLParen {
				e.advance()
				arg := e.expr()
				e.expect(tokRParen)
				return newStr(arg.toStr())
			}
			return Undefined
		default:
			e.advance()
			// Assignment expression: name = expr (returns assigned value)
			if e.peek().t == tokAssign {
				e.advance() // skip =
				val := e.expr()
				e.setVar(t.v, val)
				return val
			}
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
	if fn.bc == nil && fn.fnBody != "" {
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
		// Destructure: function({x, y}) called with either:
		//   f({x: 3, y: 4})       → props keyed by "<single>"  (one positional arg object)
		//   h(Comp, {x: 3, y: 4}) → props keyed directly as {x:3, y:4}  (JSX h-call)
		// Prefer direct name-lookup in props (JSX style); fall back to
		// "treat single value as arg object" for the f({...}) style.
		var argObj *Value
		for _, v := range props {
			if v != nil && v.typ == TypeObject && v.object != nil {
				argObj = v // single positional arg object
				break
			}
		}
		paramStr := fn.fnParams[0]
		paramStr = strings.TrimPrefix(paramStr, "{")
		paramStr = strings.TrimSuffix(strings.TrimSpace(paramStr), "}")
		for _, part := range strings.Split(paramStr, ",") {
			part = strings.TrimSpace(part)
			name := part
			defaultExpr := ""
			if eqIdx := strings.Index(part, "="); eqIdx > 0 {
				name = strings.TrimSpace(part[:eqIdx])
				defaultExpr = strings.TrimSpace(part[eqIdx+1:])
			}
			if name == "" { continue }
			// 1. JSX h-call style — props map keyed by param names directly.
			if v, ok := props[name]; ok {
				childScope[name] = v
				continue
			}
			// 2. f({...}) style — destructure from the single arg object.
			if argObj != nil && argObj.typ == TypeObject && argObj.object != nil {
				if v, ok := argObj.object[name]; ok {
					childScope[name] = v
				} else if defaultExpr != "" {
					childScope[name] = evalExpr(defaultExpr, childScope)
				} else {
					childScope[name] = Undefined
				}
			} else if defaultExpr != "" {
				childScope[name] = evalExpr(defaultExpr, childScope)
			} else {
				childScope[name] = Undefined
			}
		}
	} else if len(fn.fnParams) > 0 {
		for k, v := range props {
			childScope[k] = v
		}
	}

	// Inject captured module scope if available (for module-exported functions)
	if fn.fnScope != nil {
		for k, v := range fn.fnScope {
			if _, exists := childScope[k]; !exists {
				childScope[k] = v
			}
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

			// Array destructuring: const [a, b, ...rest] = expr
			if e.peek().t == tokLBrack {
				e.advance() // skip [
				var names []string
				restName := ""
				for e.peek().t != tokRBrack && e.peek().t != tokEOF {
					if e.peek().t == tokSpread {
						e.advance() // skip ...
						if e.peek().t == tokIdent {
							restName = e.advance().v
						}
					} else if e.peek().t == tokIdent {
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
					if restName != "" {
						restIdx := len(names)
						if restIdx < len(val.array) {
							e.scope[restName] = newArr(val.array[restIdx:])
						} else {
							e.scope[restName] = newArr(nil)
						}
					}
				} else {
					for _, name := range names {
						e.scope[name] = Undefined
					}
					if restName != "" {
						e.scope[restName] = newArr(nil)
					}
				}
				if e.peek().t == tokSemi {
					e.advance()
				}
				continue
			}

			// Object destructuring: const { a, b, x: y, z = 10, ...rest } = expr
			if e.peek().t == tokLBrace {
				e.advance() // skip {
				type destrEntry struct {
					key      string // property name in source
					local    string // local variable name
					defExpr  string // default value expression (empty if none)
					isRest   bool   // ...rest
				}
				var entries []destrEntry
				for e.peek().t != tokRBrace && e.peek().t != tokEOF {
					// ...rest
					if e.peek().t == tokSpread {
						e.advance()
						if e.peek().t == tokIdent {
							entries = append(entries, destrEntry{local: e.advance().v, isRest: true})
						}
						if e.peek().t == tokComma { e.advance() }
						continue
					}
					if e.peek().t == tokIdent {
						name := e.advance().v
						entry := destrEntry{key: name, local: name}
						// x: renamed  or  x: {nested}
						if e.peek().t == tokColon {
							e.advance()
							if e.peek().t == tokIdent {
								entry.local = e.advance().v
							} else {
								// skip nested destructuring for now
								entry.local = name
								for e.peek().t != tokComma && e.peek().t != tokRBrace && e.peek().t != tokEOF {
									e.advance()
								}
							}
						}
						// x = defaultValue
						if e.peek().t == tokAssign {
							e.advance()
							// capture default expression tokens until , or }
							start := e.pos
							depth := 0
							for e.peek().t != tokEOF {
								if e.peek().t == tokLBrace || e.peek().t == tokLParen || e.peek().t == tokLBrack { depth++ }
								if e.peek().t == tokRBrace || e.peek().t == tokRParen || e.peek().t == tokRBrack {
									if depth == 0 { break }
									depth--
								}
								if e.peek().t == tokComma && depth == 0 { break }
								e.advance()
							}
							// Build expression string from tokens
							var defStr strings.Builder
							for ti := start; ti < e.pos; ti++ {
								if e.tokens[ti].t == tokStr {
									defStr.WriteString("\"" + e.tokens[ti].v + "\"")
								} else if e.tokens[ti].t == tokNum {
									defStr.WriteString(e.tokens[ti].v)
								} else {
									defStr.WriteString(e.tokens[ti].v)
								}
								defStr.WriteByte(' ')
							}
							entry.defExpr = defStr.String()
						}
						entries = append(entries, entry)
					} else {
						e.advance() // skip unknown tokens
					}
					if e.peek().t == tokComma { e.advance() }
				}
				e.expect(tokRBrace)
				e.expect(tokAssign)
				val := e.expr()
				// Assign values
				usedKeys := map[string]bool{}
				for _, entry := range entries {
					if entry.isRest {
						rest := make(map[string]*Value)
						if val.typ == TypeObject && val.object != nil {
							for k, v := range val.object {
								if !usedKeys[k] { rest[k] = v }
							}
						}
						e.scope[entry.local] = newObj(rest)
						continue
					}
					usedKeys[entry.key] = true
					if val.typ == TypeObject && val.object != nil {
						if v, ok := val.object[entry.key]; ok {
							e.scope[entry.local] = v
						} else if entry.defExpr != "" {
							e.scope[entry.local] = evalExpr(entry.defExpr, e.scope)
						} else {
							e.scope[entry.local] = Undefined
						}
					} else if entry.defExpr != "" {
						e.scope[entry.local] = evalExpr(entry.defExpr, e.scope)
					} else {
						e.scope[entry.local] = Undefined
					}
				}
				if e.peek().t == tokSemi { e.advance() }
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
				} else if e.peek().t == tokIdent && e.peek().v == "throw" {
					// Single-statement if: if (cond) throw expr;
					e.advance()
					val := e.expr()
					if e.peek().t == tokSemi {
						e.advance()
					}
					return newThrow(val)
				} else {
					// Generic single-statement body (e.g. `console.log(...)` or
					// an assignment). Evaluate it as one expression statement
					// so the body actually runs and the parser advances past
					// the trailing `;`.
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
					} else {
						// Generic single-statement else body — execute it.
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
			// for await (...of...) — skip await keyword
			if e.peek().t == tokIdent && e.peek().v == "await" {
				e.advance()
			}
			e.expect(tokLParen)
			// Check for for...of: for (const x of arr)
			if e.peek().t == tokIdent && (e.peek().v == "const" || e.peek().v == "let" || e.peek().v == "var") {
				e.advance() // skip const/let/var
				varName := e.advance().v // variable name
				_ = varName // used below
				if e.peek().t == tokIdent && e.peek().v == "of" {
					e.advance() // skip "of"
					arr := e.expr()
					e.expect(tokRParen)
					_ = varName
					// Capture body, then execute for each item
					if e.peek().t == tokLBrace {
						bodyStart := e.pos
						e.skipBalanced(tokLBrace, tokRBrace)
						bodyEnd := e.pos
						// Convert Map/Set to iterable array
						if arr.typ == TypeObject && arr.object != nil {
							if jm, ok := arr.Custom.(*jsMap); ok {
								entries := make([]*Value, len(jm.keys))
								for ii, key := range jm.keys {
									entries[ii] = newArr([]*Value{newStr(key), jm.values[key]})
								}
								arr = newArr(entries)
							} else if js, ok := arr.Custom.(*jsSet); ok {
								items := make([]*Value, len(js.items))
								for ii, key := range js.items {
									items[ii] = js.values[key]
								}
								arr = newArr(items)
							}
						}
						if arr.typ == TypeArray || arr.typ == TypeString {
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
							if arr.typ == TypeArray {
								for _, item := range arr.array {
									// Unwrap promise values (for await...of)
									if p := getPromise(item); p != nil {
										p.mu.Lock()
										if p.state == PromiseFulfilled {
											item = p.value
										}
										p.mu.Unlock()
									}
									e.scope[varName] = item
									bodyEv.pos = 0; bodyEv.clearCache()
									result := bodyEv.evalStatements()
									_ = result
									if result == breakSentinel { break }
									if result != nil && result != continueSentinel { return result }
								}
							} else if arr.typ == TypeString {
								for _, ch := range arr.str {
									e.scope[varName] = newStr(string(ch))
									bodyEv.pos = 0; bodyEv.clearCache()
									result := bodyEv.evalStatements()
									if result == breakSentinel { break }
									if result != nil && result != continueSentinel { return result }
								}
							}
						}
					}
					continue
				}
				// for...in: for (const key in obj)
				if e.peek().t == tokIdent && e.peek().v == "in" {
					e.advance() // skip "in"
					obj := e.expr()
					e.expect(tokRParen)
					// Collect body tokens — braced or single statement
					var bodyTokens []tok
					if e.peek().t == tokLBrace {
						bodyStart := e.pos
						e.skipBalanced(tokLBrace, tokRBrace)
						rawBody := e.tokens[bodyStart:e.pos]
						if len(rawBody) >= 2 && rawBody[0].t == tokLBrace {
							bodyTokens = make([]tok, len(rawBody)-2)
							copy(bodyTokens, rawBody[1:len(rawBody)-1])
						} else {
							bodyTokens = make([]tok, len(rawBody))
							copy(bodyTokens, rawBody)
						}
					} else {
						// Single statement body (no braces)
						stmtStart := e.pos
						depth := 0
						for e.pos < len(e.tokens) && e.tokens[e.pos].t != tokEOF {
							tt := e.tokens[e.pos].t
							if tt == tokLParen || tt == tokLBrack || tt == tokLBrace { depth++ }
							if tt == tokRParen || tt == tokRBrack || tt == tokRBrace {
								if depth == 0 { break }
								depth--
							}
							if tt == tokSemi && depth == 0 { e.pos++; break }
							e.pos++
						}
						bodyTokens = make([]tok, e.pos-stmtStart)
						copy(bodyTokens, e.tokens[stmtStart:e.pos])
					}
					bodyTokens = append(bodyTokens, tok{t: tokEOF})
					if obj.typ == TypeObject && obj.object != nil {
						bodyEv := &evaluator{tokens: bodyTokens, pos: 0, scope: e.scope, useCache: true}
						// Collect all enumerable keys (object props + getset props)
						seen := make(map[string]bool)
						var allKeys []string
						for key := range obj.object {
							if !seen[key] { seen[key] = true; allKeys = append(allKeys, key) }
						}
						if obj.getset != nil {
							for key := range obj.getset {
								if !seen[key] { seen[key] = true; allKeys = append(allKeys, key) }
							}
						}
								for _, key := range allKeys {
							e.scope[varName] = newStr(key)
							bodyEv.pos = 0; bodyEv.clearCache()
							result := bodyEv.evalStatements()
							if result == breakSentinel { break }
							if result != nil && result != continueSentinel { return result }
						}
					} else if obj.typ == TypeArray {
						bodyEv := &evaluator{tokens: bodyTokens, pos: 0, scope: e.scope, useCache: true}
						for i := range obj.array {
							e.scope[varName] = newStr(strconv.Itoa(i))
							bodyEv.pos = 0; bodyEv.clearCache()
							result := bodyEv.evalStatements()
							if result == breakSentinel { break }
							if result != nil && result != continueSentinel { return result }
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

		// do...while statement
		if t.t == tokIdent && t.v == "do" {
			e.advance() // skip "do"
			if e.peek().t == tokLBrace {
				bodyStart := e.pos
				e.skipBalanced(tokLBrace, tokRBrace)
				bodyEnd := e.pos

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

				// Expect "while" (condition)
				if e.peek().t == tokIdent && e.peek().v == "while" {
					e.advance() // skip "while"
				}
				e.expect(tokLParen)
				condStart := e.pos
				e.expr()
				condEnd := e.pos
				e.expect(tokRParen)
				if e.peek().t == tokSemi { e.advance() }

				condTokens := make([]tok, condEnd-condStart+1)
				copy(condTokens, e.tokens[condStart:condEnd])
				condTokens[len(condTokens)-1] = tok{t: tokEOF}

				bodyEv := &evaluator{tokens: bodyTokens, pos: 0, scope: e.scope, useCache: true}
				condEv := &evaluator{tokens: condTokens, pos: 0, scope: e.scope}
				for iter := 0; iter < 100000; iter++ {
					bodyEv.pos = 0; bodyEv.clearCache()
					result := bodyEv.evalStatements()
					if result == breakSentinel { break }
					if result == continueSentinel { goto doWhileCond }
					if result != nil { return result }
				doWhileCond:
					condEv.pos = 0
					if !condEv.expr().truthy() { break }
				}
			}
			continue
		}

		// try/catch/finally — execute try block, catch thrown errors
		if t.t == tokIdent && t.v == "try" {
			e.advance() // skip "try"
			var tryResult *Value
			if e.peek().t == tokLBrace {
				e.advance() // skip {
				tryResult = e.evalBlock()
				// evalBlock may return early (e.g. throw/return in for loop)
				// without consuming the closing }. Consume it if present.
				if e.peek().t == tokRBrace { e.advance() }
			}

			if isThrow(tryResult) {
				// Error was thrown — execute catch block
				if e.peek().t == tokIdent && e.peek().v == "catch" {
					e.advance()
					// Parse catch parameter: catch (e) or catch
					var catchVar string
					if e.peek().t == tokLParen {
						e.advance()
						if e.peek().t == tokIdent {
							catchVar = e.advance().v
						}
						e.expect(tokRParen)
					}
					if e.peek().t == tokLBrace {
						e.advance()
						if catchVar != "" {
							e.scope[catchVar] = thrownValue(tryResult)
						}
						catchResult := e.evalBlock()
						// Execute finally if present
						if e.peek().t == tokIdent && e.peek().v == "finally" {
							e.advance()
							if e.peek().t == tokLBrace { e.advance(); e.evalBlock() }
						}
						if catchResult != nil {
							return catchResult
						}
						continue
					}
				}
				// No catch — execute finally then propagate
				if e.peek().t == tokIdent && e.peek().v == "finally" {
					e.advance()
					if e.peek().t == tokLBrace { e.advance(); e.evalBlock() }
				}
				return tryResult // propagate throw
			} else if tryResult != nil {
				// Normal return from try — skip catch, run finally
				if e.peek().t == tokIdent && e.peek().v == "catch" {
					e.advance()
					if e.peek().t == tokLParen { e.skipBalanced(tokLParen, tokRParen) }
					if e.peek().t == tokLBrace { e.skipBalanced(tokLBrace, tokRBrace) }
				}
				if e.peek().t == tokIdent && e.peek().v == "finally" {
					e.advance()
					if e.peek().t == tokLBrace { e.advance(); e.evalBlock() }
				}
				return tryResult
			} else {
				// No return, no throw — skip catch, run finally
				if e.peek().t == tokIdent && e.peek().v == "catch" {
					e.advance()
					if e.peek().t == tokLParen { e.skipBalanced(tokLParen, tokRParen) }
					if e.peek().t == tokLBrace { e.skipBalanced(tokLBrace, tokRBrace) }
				}
				if e.peek().t == tokIdent && e.peek().v == "finally" {
					e.advance()
					if e.peek().t == tokLBrace { e.advance(); e.evalBlock() }
				}
				continue
			}
		}

		// throw statement
		if t.t == tokIdent && t.v == "throw" {
			e.advance()
			val := e.expr()
			if e.peek().t == tokSemi { e.advance() }
			return newThrow(val)
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

		// function declaration — parse fully to support rest params and closures
		if t.t == tokIdent && t.v == "function" {
			e.advance()
			if e.peek().t == tokStar {
				// Generator function declaration: function* name() { ... yield ... }
				e.advance() // skip *
				funcName := ""
				if e.peek().t == tokIdent {
					funcName = e.advance().v
				}
				e.expect(tokLParen)
				var gparams []string
				for e.peek().t != tokRParen && e.peek().t != tokEOF {
					if e.peek().t == tokIdent { gparams = append(gparams, e.advance().v) }
					if e.peek().t == tokComma { e.advance() }
				}
				e.expect(tokRParen)
				if e.peek().t == tokLBrace {
					start := e.pos
					e.skipBalanced(tokLBrace, tokRBrace)
					bodyToks := make([]tok, e.pos-start-2)
					copy(bodyToks, e.tokens[start+1:e.pos-1])
					genFn := newGeneratorFunc(gparams, bodyToks, e.scope)
					if funcName != "" {
						e.scope[funcName] = genFn
					}
				}
				continue
			}
			// Regular function declaration
			funcName := ""
			if e.peek().t == tokIdent { funcName = e.advance().v }
			if e.peek().t == tokLParen {
				e.advance()
				var fparams []string
				var fRestParam string
				type defaultDef struct { name string; toks []tok }
				var defaults []defaultDef
				for e.peek().t != tokRParen && e.peek().t != tokEOF {
					if e.peek().t == tokSpread {
						e.advance()
						if e.peek().t == tokIdent { fRestParam = e.advance().v }
					} else if e.peek().t == tokLBrace {
						// Object destructuring: function f({x, y}) { ... }
						e.advance()
						var names []string
						for e.peek().t != tokRBrace && e.peek().t != tokEOF {
							if e.peek().t == tokIdent {
								nm := e.advance().v
								if e.peek().t == tokColon { e.advance(); if e.peek().t == tokIdent { nm = e.advance().v } }
								if e.peek().t == tokAssign { e.advance(); e.expr() }
								names = append(names, nm)
							} else { e.advance() }
							if e.peek().t == tokComma { e.advance() }
						}
						if e.peek().t == tokRBrace { e.advance() }
						fparams = append(fparams, "__obj_destructure__:"+strings.Join(names, ","))
					} else if e.peek().t == tokLBrack {
						// Array destructuring: function f([a, b]) { ... }
						e.advance()
						var names []string
						for e.peek().t != tokRBrack && e.peek().t != tokEOF {
							if e.peek().t == tokIdent { names = append(names, e.advance().v) }
							if e.peek().t == tokComma { e.advance() }
						}
						if e.peek().t == tokRBrack { e.advance() }
						fparams = append(fparams, "__destructure__:"+strings.Join(names, ","))
					} else if e.peek().t == tokIdent {
						pname := e.advance().v
						fparams = append(fparams, pname)
						// Default value: param = expr
						if e.peek().t == tokAssign {
							e.advance() // skip =
							var defToks []tok
							depth := 0
							for e.peek().t != tokEOF {
								if e.peek().t == tokComma && depth == 0 { break }
								if e.peek().t == tokRParen && depth == 0 { break }
								if e.peek().t == tokLParen || e.peek().t == tokLBrack || e.peek().t == tokLBrace { depth++ }
								if e.peek().t == tokRParen || e.peek().t == tokRBrack || e.peek().t == tokRBrace { depth-- }
								defToks = append(defToks, e.advance())
							}
							defaults = append(defaults, defaultDef{pname, defToks})
						}
					} else {
						e.advance() // skip unexpected tokens
					}
					if e.peek().t == tokComma { e.advance() }
				}
				e.expect(tokRParen)
				if e.peek().t == tokLBrace {
					start := e.pos
					e.skipBalanced(tokLBrace, tokRBrace)
					bodyToks := make([]tok, e.pos-start-2)
					copy(bodyToks, e.tokens[start+1:e.pos-1])
					// Prepend default value assignments: if (param === undefined) param = defaultExpr;
					var prefix []tok
					for _, d := range defaults {
						// if (name === undefined) name = expr;
						prefix = append(prefix,
							tok{t: tokIdent, v: "if"},
							tok{t: tokLParen},
							tok{t: tokIdent, v: d.name},
							tok{t: tokEqEqEq},
							tok{t: tokIdent, v: "undefined"},
							tok{t: tokRParen},
							tok{t: tokIdent, v: d.name},
							tok{t: tokAssign, v: "="},
						)
						prefix = append(prefix, d.toks...)
						prefix = append(prefix, tok{t: tokSemi})
					}
					bodyToks = append(prefix, bodyToks...)
					bodyToks = append(bodyToks, tok{t: tokEOF})
					allParams := fparams
					if fRestParam != "" {
						allParams = append(allParams, "__rest__:"+fRestParam)
					}
					af := &arrowFunc{params: allParams, tokens: bodyToks, isBlock: true, scope: e.scope}
					fnVal := &Value{typ: TypeFunc, str: "__arrow", num: float64(registerArrow(af))}
					if funcName != "" {
						e.scope[funcName] = fnVal
					}
				}
			} else {
				if e.peek().t == tokLBrace { e.skipBalanced(tokLBrace, tokRBrace) }
			}
			continue
		}

		// class declaration
		if t.t == tokIdent && t.v == "class" {
			e.advance() // skip "class"
			className, ctorFn := e.parseClass()
			if className != "" {
				e.scope[className] = ctorFn
			}
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
			// ??=
			if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokNullAssign {
				e.advance(); e.advance()
				val := e.expr()
				if v, ok := e.getVar(name); ok {
					if v.typ == TypeNull || v.typ == TypeUndefined {
						e.setVar(name, val)
					}
				} else {
					e.setVar(name, val)
				}
				if e.peek().t == tokSemi { e.advance() }
				continue
			}
			// ||=
			if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokOrAssign {
				e.advance(); e.advance()
				val := e.expr()
				if v, ok := e.getVar(name); ok {
					if !v.truthy() {
						e.setVar(name, val)
					}
				}
				if e.peek().t == tokSemi { e.advance() }
				continue
			}
			// &&=
			if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokAndAssign {
				e.advance(); e.advance()
				val := e.expr()
				if v, ok := e.getVar(name); ok {
					if v.truthy() {
						e.setVar(name, val)
					}
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

		// function declaration — parse fully to support rest params and closures
		if t.t == tokIdent && t.v == "function" {
			e.advance()
			if e.peek().t == tokStar {
				// Generator function declaration: function* name() { ... yield ... }
				e.advance() // skip *
				funcName := ""
				if e.peek().t == tokIdent {
					funcName = e.advance().v
				}
				e.expect(tokLParen)
				var gparams []string
				for e.peek().t != tokRParen && e.peek().t != tokEOF {
					if e.peek().t == tokIdent { gparams = append(gparams, e.advance().v) }
					if e.peek().t == tokComma { e.advance() }
				}
				e.expect(tokRParen)
				if e.peek().t == tokLBrace {
					start := e.pos
					e.skipBalanced(tokLBrace, tokRBrace)
					bodyToks := make([]tok, e.pos-start-2)
					copy(bodyToks, e.tokens[start+1:e.pos-1])
					genFn := newGeneratorFunc(gparams, bodyToks, e.scope)
					if funcName != "" {
						e.scope[funcName] = genFn
					}
				}
				continue
			}
			// Regular function declaration
			funcName := ""
			if e.peek().t == tokIdent { funcName = e.advance().v }
			if e.peek().t == tokLParen {
				e.advance()
				var fparams []string
				var fRestParam string
				type defaultDef struct { name string; toks []tok }
				var defaults []defaultDef
				for e.peek().t != tokRParen && e.peek().t != tokEOF {
					if e.peek().t == tokSpread {
						e.advance()
						if e.peek().t == tokIdent { fRestParam = e.advance().v }
					} else if e.peek().t == tokLBrace {
						// Object destructuring: function f({x, y}) { ... }
						e.advance()
						var names []string
						for e.peek().t != tokRBrace && e.peek().t != tokEOF {
							if e.peek().t == tokIdent {
								nm := e.advance().v
								if e.peek().t == tokColon { e.advance(); if e.peek().t == tokIdent { nm = e.advance().v } }
								if e.peek().t == tokAssign { e.advance(); e.expr() }
								names = append(names, nm)
							} else { e.advance() }
							if e.peek().t == tokComma { e.advance() }
						}
						if e.peek().t == tokRBrace { e.advance() }
						fparams = append(fparams, "__obj_destructure__:"+strings.Join(names, ","))
					} else if e.peek().t == tokLBrack {
						// Array destructuring: function f([a, b]) { ... }
						e.advance()
						var names []string
						for e.peek().t != tokRBrack && e.peek().t != tokEOF {
							if e.peek().t == tokIdent { names = append(names, e.advance().v) }
							if e.peek().t == tokComma { e.advance() }
						}
						if e.peek().t == tokRBrack { e.advance() }
						fparams = append(fparams, "__destructure__:"+strings.Join(names, ","))
					} else if e.peek().t == tokIdent {
						pname := e.advance().v
						fparams = append(fparams, pname)
						// Default value: param = expr
						if e.peek().t == tokAssign {
							e.advance() // skip =
							var defToks []tok
							depth := 0
							for e.peek().t != tokEOF {
								if e.peek().t == tokComma && depth == 0 { break }
								if e.peek().t == tokRParen && depth == 0 { break }
								if e.peek().t == tokLParen || e.peek().t == tokLBrack || e.peek().t == tokLBrace { depth++ }
								if e.peek().t == tokRParen || e.peek().t == tokRBrack || e.peek().t == tokRBrace { depth-- }
								defToks = append(defToks, e.advance())
							}
							defaults = append(defaults, defaultDef{pname, defToks})
						}
					} else {
						e.advance() // skip unexpected tokens
					}
					if e.peek().t == tokComma { e.advance() }
				}
				e.expect(tokRParen)
				if e.peek().t == tokLBrace {
					start := e.pos
					e.skipBalanced(tokLBrace, tokRBrace)
					bodyToks := make([]tok, e.pos-start-2)
					copy(bodyToks, e.tokens[start+1:e.pos-1])
					// Prepend default value assignments: if (param === undefined) param = defaultExpr;
					var prefix []tok
					for _, d := range defaults {
						// if (name === undefined) name = expr;
						prefix = append(prefix,
							tok{t: tokIdent, v: "if"},
							tok{t: tokLParen},
							tok{t: tokIdent, v: d.name},
							tok{t: tokEqEqEq},
							tok{t: tokIdent, v: "undefined"},
							tok{t: tokRParen},
							tok{t: tokIdent, v: d.name},
							tok{t: tokAssign, v: "="},
						)
						prefix = append(prefix, d.toks...)
						prefix = append(prefix, tok{t: tokSemi})
					}
					bodyToks = append(prefix, bodyToks...)
					bodyToks = append(bodyToks, tok{t: tokEOF})
					allParams := fparams
					if fRestParam != "" {
						allParams = append(allParams, "__rest__:"+fRestParam)
					}
					af := &arrowFunc{params: allParams, tokens: bodyToks, isBlock: true, scope: e.scope}
					fnVal := &Value{typ: TypeFunc, str: "__arrow", num: float64(registerArrow(af))}
					if funcName != "" {
						e.scope[funcName] = fnVal
					}
				}
			} else {
				if e.peek().t == tokLBrace { e.skipBalanced(tokLBrace, tokRBrace) }
			}
			continue
		}

		// class declaration
		if t.t == tokIdent && t.v == "class" {
			e.advance()
			className, ctorFn := e.parseClass()
			if className != "" {
				e.scope[className] = ctorFn
			}
			continue
		}

		// if/else statement — must be handled before the expression-fallback
		// below, otherwise `if (...) console.log(...); else console.log(...);`
		// gets parsed as the identifier `if` followed by separate expressions
		// and BOTH branches end up running.
		if t.t == tokIdent && t.v == "if" {
			e.advance()
			e.expect(tokLParen)
			cond := e.expr()
			e.expect(tokRParen)
			if cond.truthy() {
				if e.peek().t == tokLBrace {
					e.advance()
					if r := e.evalBlock(); r != nil {
						return r
					}
				} else if e.peek().t == tokIdent && e.peek().v == "return" {
					if r := e.evalSingleStatement(); r != nil {
						return r
					}
				} else {
					if r := e.evalSingleStatement(); r != nil {
						return r
					}
				}
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
						continue
					}
					if e.peek().t == tokLBrace {
						e.advance()
						if r := e.evalBlock(); r != nil {
							return r
						}
					} else {
						if r := e.evalSingleStatement(); r != nil {
							return r
						}
					}
				}
			}
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

		// throw statement
		if t.t == tokIdent && t.v == "throw" {
			e.advance()
			val := e.expr()
			if e.peek().t == tokSemi { e.advance() }
			// Skip to closing brace
			for e.peek().t != tokRBrace && e.peek().t != tokEOF {
				e.advance()
			}
			if e.peek().t == tokRBrace { e.advance() }
			return newThrow(val)
		}

		// try/catch/finally
		if t.t == tokIdent && t.v == "try" {
			e.advance()
			var tryResult *Value
			if e.peek().t == tokLBrace {
				e.advance()
				tryResult = e.evalBlock()
				if e.peek().t == tokRBrace { e.advance() }
			}
			if isThrow(tryResult) {
				if e.peek().t == tokIdent && e.peek().v == "catch" {
					e.advance()
					var catchVar string
					if e.peek().t == tokLParen {
						e.advance()
						if e.peek().t == tokIdent { catchVar = e.advance().v }
						e.expect(tokRParen)
					}
					if e.peek().t == tokLBrace {
						e.advance()
						if catchVar != "" { e.scope[catchVar] = thrownValue(tryResult) }
						catchResult := e.evalBlock()
						if e.peek().t == tokIdent && e.peek().v == "finally" {
							e.advance()
							if e.peek().t == tokLBrace { e.advance(); e.evalBlock() }
						}
						if catchResult != nil { return catchResult }
						continue
					}
				}
				if e.peek().t == tokIdent && e.peek().v == "finally" {
					e.advance()
					if e.peek().t == tokLBrace { e.advance(); e.evalBlock() }
				}
				return tryResult
			} else if tryResult != nil {
				if e.peek().t == tokIdent && e.peek().v == "catch" {
					e.advance()
					if e.peek().t == tokLParen { e.skipBalanced(tokLParen, tokRParen) }
					if e.peek().t == tokLBrace { e.skipBalanced(tokLBrace, tokRBrace) }
				}
				if e.peek().t == tokIdent && e.peek().v == "finally" {
					e.advance()
					if e.peek().t == tokLBrace { e.advance(); e.evalBlock() }
				}
				return tryResult
			} else {
				if e.peek().t == tokIdent && e.peek().v == "catch" {
					e.advance()
					if e.peek().t == tokLParen { e.skipBalanced(tokLParen, tokRParen) }
					if e.peek().t == tokLBrace { e.skipBalanced(tokLBrace, tokRBrace) }
				}
				if e.peek().t == tokIdent && e.peek().v == "finally" {
					e.advance()
					if e.peek().t == tokLBrace { e.advance(); e.evalBlock() }
				}
				continue
			}
		}

		// Handle const/let/var declarations
		if t.t == tokIdent && (t.v == "const" || t.v == "let" || t.v == "var") {
			e.advance()

			// Array destructuring: const [a, b, ...rest] = expr
			if e.peek().t == tokLBrack {
				e.advance() // skip [
				var names []string
				restName := ""
				for e.peek().t != tokRBrack && e.peek().t != tokEOF {
					if e.peek().t == tokSpread {
						e.advance() // skip ...
						if e.peek().t == tokIdent {
							restName = e.advance().v
						}
					} else if e.peek().t == tokIdent {
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
					if restName != "" {
						restIdx := len(names)
						if restIdx < len(val.array) {
							e.scope[restName] = newArr(val.array[restIdx:])
						} else {
							e.scope[restName] = newArr(nil)
						}
					}
				} else {
					for _, name := range names {
						e.scope[name] = Undefined
					}
					if restName != "" {
						e.scope[restName] = newArr(nil)
					}
				}
				if e.peek().t == tokSemi {
					e.advance()
				}
				continue
			}

			// Object destructuring: const { a, b, x: y, z = 10, ...rest } = expr
			if e.peek().t == tokLBrace {
				e.advance()
				type de struct { key, local, def string; isRest bool }
				var entries []de
				for e.peek().t != tokRBrace && e.peek().t != tokEOF {
					if e.peek().t == tokSpread {
						e.advance()
						if e.peek().t == tokIdent { entries = append(entries, de{local: e.advance().v, isRest: true}) }
						if e.peek().t == tokComma { e.advance() }
						continue
					}
					if e.peek().t == tokIdent {
						nm := e.advance().v
						entry := de{key: nm, local: nm}
						if e.peek().t == tokColon {
							e.advance()
							if e.peek().t == tokIdent { entry.local = e.advance().v } else {
								for e.peek().t != tokComma && e.peek().t != tokRBrace && e.peek().t != tokEOF { e.advance() }
							}
						}
						if e.peek().t == tokAssign {
							e.advance()
							start := e.pos; depth := 0
							for e.peek().t != tokEOF {
								if e.peek().t == tokLBrace || e.peek().t == tokLParen { depth++ }
								if e.peek().t == tokRBrace || e.peek().t == tokRParen { if depth == 0 { break }; depth-- }
								if e.peek().t == tokComma && depth == 0 { break }
								e.advance()
							}
							var ds strings.Builder
							for ti := start; ti < e.pos; ti++ {
								if e.tokens[ti].t == tokStr { ds.WriteString("\"" + e.tokens[ti].v + "\"") } else if e.tokens[ti].t == tokNum { ds.WriteString(e.tokens[ti].v) } else { ds.WriteString(e.tokens[ti].v) }
								ds.WriteByte(' ')
							}
							entry.def = ds.String()
						}
						entries = append(entries, entry)
					} else { e.advance() }
					if e.peek().t == tokComma { e.advance() }
				}
				e.expect(tokRBrace)
				e.expect(tokAssign)
				val := e.expr()
				used := map[string]bool{}
				for _, en := range entries {
					if en.isRest {
						rest := make(map[string]*Value)
						if val.typ == TypeObject && val.object != nil { for k, v := range val.object { if !used[k] { rest[k] = v } } }
						e.scope[en.local] = newObj(rest)
						continue
					}
					used[en.key] = true
					if val.typ == TypeObject && val.object != nil {
						if v, ok := val.object[en.key]; ok { e.scope[en.local] = v } else if en.def != "" { e.scope[en.local] = evalExpr(en.def, e.scope) } else { e.scope[en.local] = Undefined }
					} else if en.def != "" { e.scope[en.local] = evalExpr(en.def, e.scope) } else { e.scope[en.local] = Undefined }
				}
				if e.peek().t == tokSemi { e.advance() }
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
				} else if e.peek().t == tokIdent && e.peek().v == "throw" {
					e.advance()
					val := e.expr()
					if e.peek().t == tokSemi { e.advance() }
					for e.peek().t != tokRBrace && e.peek().t != tokEOF { e.advance() }
					if e.peek().t == tokRBrace { e.advance() }
					return newThrow(val)
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
					// for...in inside block
					if e.peek().t == tokIdent && e.peek().v == "in" {
						e.advance() // skip "in"
						obj := e.expr()
						e.expect(tokRParen)
						// Capture body tokens — braced or single statement.
						var bodyTokens []tok
						if e.peek().t == tokLBrace {
							bs := e.pos
							e.skipBalanced(tokLBrace, tokRBrace)
							be := e.pos
							rawBody := e.tokens[bs:be]
							if len(rawBody) >= 2 && rawBody[0].t == tokLBrace {
								bodyTokens = make([]tok, len(rawBody)-2)
								copy(bodyTokens, rawBody[1:len(rawBody)-1])
							} else {
								bodyTokens = make([]tok, len(rawBody))
								copy(bodyTokens, rawBody)
							}
						} else {
							// Single-statement body: `for (k in obj) stmt;`
							stmtStart := e.pos
							depth := 0
							for e.pos < len(e.tokens) && e.tokens[e.pos].t != tokEOF {
								tt := e.tokens[e.pos].t
								if tt == tokLParen || tt == tokLBrack || tt == tokLBrace { depth++ }
								if tt == tokRParen || tt == tokRBrack || tt == tokRBrace {
									if depth == 0 { break }
									depth--
								}
								if tt == tokSemi && depth == 0 { e.pos++; break }
								e.pos++
							}
							bodyTokens = make([]tok, e.pos-stmtStart)
							copy(bodyTokens, e.tokens[stmtStart:e.pos])
						}
						bodyTokens = append(bodyTokens, tok{t: tokEOF})
						if obj.typ == TypeObject && obj.object != nil {
							var allKeys []string
							for key := range obj.object {
								allKeys = append(allKeys, key)
							}
							for _, key := range allKeys {
								e.scope[vn] = newStr(key)
								bev := &evaluator{tokens: bodyTokens, pos: 0, scope: e.scope}
								result := bev.evalStatements()
								if result == breakSentinel { break }
								if result != nil && result != continueSentinel { return result }
							}
						} else if obj.typ == TypeArray {
							for i := range obj.array {
								e.scope[vn] = newStr(strconv.Itoa(i))
								bev := &evaluator{tokens: bodyTokens, pos: 0, scope: e.scope}
								result := bev.evalStatements()
								if result == breakSentinel { break }
								if result != nil && result != continueSentinel { return result }
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
				// Skip condition tokens without evaluating
				{
					depth := 0
					for e.pos < len(e.tokens) {
						tk := e.tokens[e.pos]
						if tk.t == tokLParen { depth++ } else if tk.t == tokRParen { depth-- }
						if tk.t == tokSemi && depth <= 0 { break }
						e.pos++
					}
				}
				ce := e.pos
				e.expect(tokSemi)
				us := e.pos
				// Skip update tokens without evaluating
				{
					depth := 0
					for e.pos < len(e.tokens) {
						tk := e.tokens[e.pos]
						if tk.t == tokLParen { depth++ } else if tk.t == tokRParen { if depth == 0 { break }; depth-- }
						e.pos++
					}
				}
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

		// General expression statement (method calls, function calls, etc.)
		// e.g. process.stdout.write("hello"), myFunc(), obj.method()
		e.expr()
		if e.peek().t == tokSemi { e.advance() }
		continue
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
		if e.peek().t == tokSpread {
			e.advance() // skip ...
			val := e.expr()
			if val.typ == TypeArray && val.array != nil {
				items = append(items, val.array...)
			} else if val.typ == TypeObject && val.object != nil {
				// Iterable objects (Set, Map) — call values() if available
				if valsFn, ok := val.object["values"]; ok && valsFn.typ == TypeFunc {
					result := callFuncValue(valsFn, nil, e.scope)
					if result.typ == TypeArray {
						items = append(items, result.array...)
					}
				}
			}
		} else {
			items = append(items, e.expr())
		}
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
	var getset map[string]*PropDescriptor
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

		// key: identifier, string, number, or computed [expr]
		var key string
		if e.peek().t == tokLBrack {
			// Computed property: { [expr]: value }
			e.advance() // skip [
			keyVal := e.expr()
			key = keyVal.toStr()
			e.expect(tokRBrack)
		} else if e.peek().t == tokStr {
			key = e.advance().v
		} else if e.peek().t == tokNum {
			key = e.advance().v
		} else if e.peek().t == tokIdent {
			key = e.advance().v
		} else {
			e.advance()
			continue
		}

		// get/set property syntax: { get name() { ... }, set name(v) { ... } }
		if (key == "get" || key == "set") && e.peek().t == tokIdent {
			propName := e.advance().v
			e.expect(tokLParen)
			var params []string
			for e.peek().t != tokRParen && e.peek().t != tokEOF {
				if e.peek().t == tokIdent {
					params = append(params, e.advance().v)
				}
				if e.peek().t == tokComma { e.advance() }
			}
			e.expect(tokRParen)
			// Parse body as arrow function
			if e.peek().t == tokLBrace {
				start := e.pos
				e.skipBalanced(tokLBrace, tokRBrace)
				bodyToks := make([]tok, e.pos-start-2)
				copy(bodyToks, e.tokens[start+1:e.pos-1])
				bodyToks = append(bodyToks, tok{t: tokEOF})
				af := &arrowFunc{params: params, tokens: bodyToks, isBlock: true, scope: e.scope}
				fnVal := &Value{typ: TypeFunc, str: "__arrow", num: float64(registerArrow(af))}
				if getset == nil {
					getset = make(map[string]*PropDescriptor)
				}
				desc, ok := getset[propName]
				if !ok {
					desc = &PropDescriptor{}
					getset[propName] = desc
				}
				if key == "get" {
					desc.Get = fnVal
				} else {
					desc.Set = fnVal
				}
			}
			if e.peek().t == tokComma { e.advance() }
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

		// Method shorthand: { method() { ... } } or { method({ destructured }) { ... } }
		if e.peek().t == tokLParen {
			var params []string
			e.advance() // skip (
			for e.peek().t != tokRParen && e.peek().t != tokEOF {
				if e.peek().t == tokLBrace {
					// Object destructuring param: { key1, key2 }
					e.advance()
					var names []string
					for e.peek().t != tokRBrace && e.peek().t != tokEOF {
						if e.peek().t == tokIdent {
							nm := e.advance().v
							if e.peek().t == tokColon { e.advance(); if e.peek().t == tokIdent { nm = e.advance().v } }
							if e.peek().t == tokAssign { e.advance(); e.skipExpr() }
							names = append(names, nm)
						} else { e.advance() }
						if e.peek().t == tokComma { e.advance() }
					}
					if e.peek().t == tokRBrace { e.advance() }
					params = append(params, "__obj_destructure__:"+strings.Join(names, ","))
				} else if e.peek().t == tokLBrack {
					// Array destructuring param: [a, b]
					e.advance()
					var names []string
					for e.peek().t != tokRBrack && e.peek().t != tokEOF {
						if e.peek().t == tokIdent { names = append(names, e.advance().v) }
						if e.peek().t == tokComma { e.advance() }
					}
					if e.peek().t == tokRBrack { e.advance() }
					params = append(params, "__destructure__:"+strings.Join(names, ","))
				} else if e.peek().t == tokIdent {
					params = append(params, e.advance().v)
				} else {
					e.advance() // skip unknown tokens
				}
				if e.peek().t == tokAssign { e.advance(); e.skipExpr() }
				if e.peek().t == tokComma { e.advance() }
			}
			e.expect(tokRParen)
			if e.peek().t == tokLBrace {
				start := e.pos
				e.skipBalanced(tokLBrace, tokRBrace)
				bodyToks := make([]tok, e.pos-start-2)
				copy(bodyToks, e.tokens[start+1:e.pos-1])
				bodyToks = append(bodyToks, tok{t: tokEOF})
				af := &arrowFunc{params: params, tokens: bodyToks, isBlock: true, scope: e.scope}
				obj[key] = &Value{typ: TypeFunc, str: "__arrow", num: float64(registerArrow(af))}
			}
			if e.peek().t == tokComma { e.advance() }
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
	result := newObj(obj)
	if getset != nil {
		result.getset = getset
	}
	return result
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

// parseClass parses a class declaration and returns (className, constructorValue).
// It expects the evaluator to be positioned right after the "class" keyword.
func (e *evaluator) parseClass() (string, *Value) {
	className := ""
	if e.peek().t == tokIdent && e.peek().v != "extends" {
		className = e.advance().v
	}

	// Check for extends
	var parentProto *Value
	parentClassName := ""
	if e.peek().t == tokIdent && e.peek().v == "extends" {
		e.advance() // skip "extends"
		parentClassName = e.advance().v
		// Resolve parent: could be simple name or dotted (e.g. mod.ClassName)
		var parentCtor *Value
		if v, ok := e.scope[parentClassName]; ok {
			parentCtor = v
		}
		for parentCtor != nil && e.peek().t == tokDot {
			e.advance() // skip .
			prop := e.advance().v
			parentClassName = parentClassName + "." + prop
			parentCtor = parentCtor.getProp(prop)
		}
		if parentCtor != nil && parentCtor.typ == TypeFunc {
			if parentCtor.object != nil {
				if pp, ok := parentCtor.object["__prototype__"]; ok {
					parentProto = pp
				}
			}
		}
	}

	e.expect(tokLBrace)

	prototype := newObj(make(map[string]*Value))
	var constructorParams []string
	var constructorBodyToks []tok
	var gsMap map[string]*PropDescriptor
	staticMethods := make(map[string]*Value)
	privateFieldDefaults := make(map[string]*Value) // #field → default value

	for e.peek().t != tokRBrace && e.peek().t != tokEOF {
		isStatic := false
		isGetter := false
		isSetter := false

		// Check for "static" keyword
		if e.peek().t == tokIdent && e.peek().v == "static" {
			if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokIdent {
				isStatic = true
				e.advance()
			}
		}

		// Skip "async" keyword on methods (async connect() { ... })
		if e.peek().t == tokIdent && e.peek().v == "async" {
			if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokIdent {
				e.advance() // skip "async", method name follows
			}
		}

		if e.peek().t == tokIdent && e.peek().v == "get" {
			if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokIdent {
				isGetter = true
				e.advance()
			}
		}
		if !isGetter && e.peek().t == tokIdent && e.peek().v == "set" {
			if e.pos+1 < len(e.tokens) && e.tokens[e.pos+1].t == tokIdent {
				isSetter = true
				e.advance()
			}
		}

		if e.peek().t != tokIdent {
			e.advance()
			continue
		}

		memberName := e.advance().v

		// Private field declaration: #field = value; or #field;
		if strings.HasPrefix(memberName, "#") && e.peek().t != tokLParen {
			if e.peek().t == tokAssign {
				e.advance() // skip =
				val := e.expr()
				privateFieldDefaults[memberName] = val
			} else {
				privateFieldDefaults[memberName] = Undefined
			}
			if e.peek().t == tokSemi { e.advance() }
			continue
		}

		if e.peek().t != tokLParen {
			continue
		}

		// Parse params (supports defaults, rest, destructuring)
		e.advance() // skip (
		var params []string
		for e.peek().t != tokRParen && e.peek().t != tokEOF {
			if e.peek().t == tokSpread {
				e.advance()
				if e.peek().t == tokIdent {
					params = append(params, "__rest__:"+e.advance().v)
				}
			} else if e.peek().t == tokLBrace {
				e.advance()
				var names []string
				for e.peek().t != tokRBrace && e.peek().t != tokEOF {
					if e.peek().t == tokIdent {
						nm := e.advance().v
						if e.peek().t == tokColon { e.advance(); if e.peek().t == tokIdent { nm = e.advance().v } }
						if e.peek().t == tokAssign { e.advance(); e.skipExpr() }
						names = append(names, nm)
					} else { e.advance() }
					if e.peek().t == tokComma { e.advance() }
				}
				if e.peek().t == tokRBrace { e.advance() }
				params = append(params, "__obj_destructure__:"+strings.Join(names, ","))
			} else if e.peek().t == tokIdent {
				params = append(params, e.advance().v)
			}
			// Skip default value: = expr
			if e.peek().t == tokAssign {
				e.advance() // skip =
				e.skipExpr()
			}
			if e.peek().t == tokComma {
				e.advance()
			}
		}
		e.expect(tokRParen)

		if e.peek().t != tokLBrace {
			continue
		}
		start := e.pos
		e.skipBalanced(tokLBrace, tokRBrace)
		bodyToks := make([]tok, e.pos-start-2)
		copy(bodyToks, e.tokens[start+1:e.pos-1])
		bodyToks = append(bodyToks, tok{t: tokEOF})

		if memberName == "constructor" && !isGetter && !isSetter {
			constructorParams = params
			constructorBodyToks = bodyToks
		} else if isGetter || isSetter {
			if gsMap == nil {
				gsMap = make(map[string]*PropDescriptor)
			}
			desc, ok := gsMap[memberName]
			if !ok {
				desc = &PropDescriptor{}
				gsMap[memberName] = desc
			}
			af := &arrowFunc{params: params, tokens: bodyToks, isBlock: true, scope: e.scope}
			fnVal := &Value{typ: TypeFunc, str: "__arrow", num: float64(registerArrow(af))}
			if isGetter {
				desc.Get = fnVal
			} else {
				desc.Set = fnVal
			}
		} else if isStatic {
			af := &arrowFunc{params: params, tokens: bodyToks, isBlock: true, scope: e.scope}
			staticMethods[memberName] = &Value{typ: TypeFunc, str: "__arrow", num: float64(registerArrow(af))}
		} else {
			af := &arrowFunc{params: params, tokens: bodyToks, isBlock: true, scope: e.scope}
			prototype.object[memberName] = &Value{typ: TypeFunc, str: "__arrow", num: float64(registerArrow(af))}
		}

		if e.peek().t == tokSemi {
			e.advance()
		}
	}
	e.expect(tokRBrace)

	if parentProto != nil {
		prototype.proto = parentProto
	}
	if gsMap != nil {
		prototype.getset = gsMap
	}

	protoRef := prototype
	ctorParams := constructorParams
	ctorBody := constructorBodyToks
	scopeSnapshot := make(map[string]*Value, len(e.scope))
	for k, v := range e.scope {
		scopeSnapshot[k] = v
	}
	cName := className
	pClassName := parentClassName
	privDefaults := privateFieldDefaults
	// Collect parent private field defaults
	if pClassName != "" {
		if parentCtor, ok := e.scope[pClassName]; ok && parentCtor.object != nil {
			if ppd, ok := parentCtor.object["__private_defaults__"]; ok && ppd.typ == TypeObject {
				for k, v := range ppd.object {
					if _, exists := privDefaults[k]; !exists {
						privDefaults[k] = v
					}
				}
			}
		}
	}

	ctorFn := NewNativeFunc(func(args []*Value) *Value {
		obj := newObj(make(map[string]*Value))
		obj.proto = protoRef
		obj.object["__constructor__"] = newStr(cName)
		// Apply private field defaults (own + inherited)
		for k, v := range privDefaults {
			obj.object[k] = v
		}
		// Build constructor chain for instanceof checks
		chain := []*Value{newStr(cName)}
		if pClassName != "" {
			chain = append(chain, newStr(pClassName))
			// Walk parent's chain if available
			if parentCtor, ok := scopeSnapshot[pClassName]; ok && parentCtor.object != nil {
				if parentChain, ok := parentCtor.object["__constructors__"]; ok && parentChain.typ == TypeArray {
					for _, pc := range parentChain.array {
						chain = append(chain, pc)
					}
				}
			}
		}
		obj.object["__constructors__"] = newArr(chain)

		if ctorBody != nil {
			childScope := make(map[string]*Value, len(scopeSnapshot)+len(ctorParams)+4)
			for k, v := range scopeSnapshot {
				childScope[k] = v
			}
			childScope["this"] = obj
			if pClassName != "" {
				parentCtorRef := scopeSnapshot[pClassName]
				pcn := pClassName
				childScope["super"] = NewNativeFunc(func(superArgs []*Value) *Value {
					// Handle built-in Error types
					if pcn == "Error" || pcn == "TypeError" || pcn == "RangeError" || pcn == "SyntaxError" {
						msg := ""
						if len(superArgs) > 0 {
							msg = superArgs[0].toStr()
						}
						obj.object["message"] = newStr(msg)
						if _, hasName := obj.object["name"]; !hasName {
							obj.object["name"] = newStr(cName)
						}
						obj.object["stack"] = newStr(cName + ": " + msg)
						return Undefined
					}
					if parentCtorRef != nil && parentCtorRef.object != nil {
						if parentCtorBody, ok := parentCtorRef.object["__ctor_body__"]; ok && parentCtorBody != nil {
							parentCtorParams := parentCtorRef.object["__ctor_params__"]
							superScope := make(map[string]*Value, len(scopeSnapshot)+4)
							for k, v := range scopeSnapshot {
								superScope[k] = v
							}
							superScope["this"] = obj
							if parentCtorParams != nil && parentCtorParams.typ == TypeArray {
								for i, pv := range parentCtorParams.array {
									if i < len(superArgs) {
										superScope[pv.str] = superArgs[i]
									}
								}
							}
							bodyToksCopy := make([]tok, len(parentCtorBody.array))
							for i, tv := range parentCtorBody.array {
								bodyToksCopy[i] = tok{t: tokType(int(tv.num)), v: tv.str}
							}
							ev := &evaluator{tokens: bodyToksCopy, pos: 0, scope: superScope}
							ev.evalStatements()
						}
					}
					return Undefined
				})
			}
			for i, p := range ctorParams {
				if i < len(args) {
					childScope[p] = args[i]
				} else {
					childScope[p] = Undefined
				}
			}
			bodyToksCopy := make([]tok, len(ctorBody))
			copy(bodyToksCopy, ctorBody)
			ev := &evaluator{tokens: bodyToksCopy, pos: 0, scope: childScope}
			ev.evalStatements()
		}
		return obj
	})

	ctorFn.object = make(map[string]*Value)
	ctorFn.object["__prototype__"] = protoRef
	ctorFn.object["__class__"] = newStr(cName)
	// Store private field defaults so child classes can inherit them
	if len(privDefaults) > 0 {
		pd := NewObj(make(map[string]*Value, len(privDefaults)))
		for k, v := range privDefaults {
			pd.object[k] = v
		}
		ctorFn.object["__private_defaults__"] = pd
	}
	if ctorBody != nil {
		bodyArr := make([]*Value, len(ctorBody))
		for i, t := range ctorBody {
			bodyArr[i] = &Value{typ: TypeString, num: float64(t.t), str: t.v}
		}
		ctorFn.object["__ctor_body__"] = newArr(bodyArr)
		paramArr := make([]*Value, len(ctorParams))
		for i, p := range ctorParams {
			paramArr[i] = newStr(p)
		}
		ctorFn.object["__ctor_params__"] = newArr(paramArr)
	}

	// Add static methods to the constructor function
	for name, fn := range staticMethods {
		ctorFn.object[name] = fn
	}

	return className, ctorFn
}

// lookupMethod searches for a method on an object and its prototype chain.
func lookupMethod(v *Value, name string) *Value {
	for cur := v; cur != nil; cur = cur.proto {
		if cur.object != nil {
			if fn, ok := cur.object[name]; ok {
				return fn
			}
		}
	}
	return nil
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
				if obj.typ == TypeObject {
					// Check for setter
					if obj.getset != nil {
						if desc, ok := obj.getset[prop]; ok && desc.Set != nil {
							if desc.Set.native != nil {
								desc.Set.native([]*Value{val})
							} else if desc.Set.str == "__arrow" {
								setScope := make(map[string]*Value, len(e.scope)+1)
								for k, sv := range e.scope { setScope[k] = sv }
								setScope["this"] = obj
								callArrow(int(desc.Set.num), []*Value{val}, setScope)
								// Write back mutations
								for k, sv := range setScope {
									if k != "this" {
										e.scope[k] = sv
									}
								}
							}
							return
						}
					}
					if obj.object != nil {
						obj.object[prop] = val
					}
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
			if obj.typ == TypeArray {
				idx := int(key.toNum())
				if idx >= 0 && idx < len(obj.array) {
					obj = obj.array[idx]
					continue
				}
			}
			return
		} else {
			return
		}
	}
}
