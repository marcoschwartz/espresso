package espresso

import (
	"fmt"
	"strings"
)

// ── Exported types for embedders (like Lungo SSR) ───────

// Tok is a token from the JS tokenizer.
type Tok = tok

// TokType is the type of a token.
type TokType = tokType

// Token type constants (exported).
const (
	TokEOF       = tokEOF
	TokIdent     = tokIdent
	TokNum       = tokNum
	TokStr       = tokStr
	TokLParen    = tokLParen
	TokRParen    = tokRParen
	TokLBrace    = tokLBrace
	TokRBrace    = tokRBrace
	TokLBrack    = tokLBrack
	TokRBrack    = tokRBrack
	TokComma     = tokComma
	TokColon     = tokColon
	TokSemi      = tokSemi
	TokDot       = tokDot
	TokAssign    = tokAssign
	TokArrow     = tokArrow
	TokSpread    = tokSpread
)

// Tokenize tokenizes JS source code into tokens (exported, cached).
func Tokenize(code string) []Tok {
	return tokenizeCached(code)
}

// TokenizeRaw tokenizes JS source code without caching.
func TokenizeRaw(code string) []Tok {
	return tokenize(code)
}

// ── Exported evaluator for embedders ────────────────────

// Eval is an exported evaluator that provides low-level access
// for embedders that need to control evaluation directly.
type Eval struct {
	ev *evaluator
}

// NewEval creates a new evaluator with the given tokens and scope.
func NewEval(tokens []Tok, scope map[string]*Value) *Eval {
	return &Eval{ev: &evaluator{tokens: tokens, pos: 0, scope: scope}}
}

// Expr evaluates a single expression.
func (e *Eval) Expr() *Value {
	return e.ev.expr()
}

// EvalStatements evaluates multiple statements and returns the result.
func (e *Eval) EvalStatements() *Value {
	return e.ev.evalStatements()
}

// Scope returns the evaluator's scope.
func (e *Eval) Scope() map[string]*Value {
	return e.ev.scope
}

// ── Page parsing utilities (used by Lungo SSR) ──────────

// ExtractDefaultExport finds the default export function in JS source
// and returns its body and parameter string.
func ExtractDefaultExport(source string) (body string, params string, err error) {
	idx := indexOutsideStrings(source, "export default function")
	if idx < 0 {
		return "", "", fmt.Errorf("no default export function found")
	}

	rest := source[idx+len("export default function"):]
	rest = strings.TrimSpace(rest)
	if len(rest) > 0 && rest[0] != '(' {
		nameEnd := strings.IndexAny(rest, "( ")
		if nameEnd < 0 {
			return "", "", fmt.Errorf("malformed function declaration")
		}
		rest = strings.TrimSpace(rest[nameEnd:])
	}

	if len(rest) == 0 || rest[0] != '(' {
		return "", "", fmt.Errorf("expected ( after function name")
	}
	parenEnd := findMatchingParen(rest, 0)
	if parenEnd < 0 {
		return "", "", fmt.Errorf("unmatched ( in function params")
	}
	params = strings.TrimSpace(rest[1:parenEnd])
	rest = strings.TrimSpace(rest[parenEnd+1:])

	if len(rest) == 0 || rest[0] != '{' {
		return "", "", fmt.Errorf("expected { after function params")
	}
	braceEnd := findMatchingBrace(rest, 0)
	if braceEnd < 0 {
		return "", "", fmt.Errorf("unmatched { in function body")
	}
	body = rest[1:braceEnd]
	return body, params, nil
}

// ExtractFunctions finds all non-exported function definitions and
// returns them as Value objects with fnParams and fnBody.
func ExtractFunctions(source string) map[string]*Value {
	funcs := make(map[string]*Value)
	i := 0
	for i < len(source) {
		idx := strings.Index(source[i:], "function ")
		if idx < 0 {
			break
		}
		absIdx := i + idx
		prefix := source[max(0, absIdx-30):absIdx]
		if strings.Contains(prefix, "export default") {
			i = absIdx + 9
			continue
		}
		rest := source[absIdx+9:]
		rest = strings.TrimSpace(rest)
		nameEnd := strings.IndexAny(rest, "( \t\n")
		if nameEnd < 0 || nameEnd == 0 {
			i = absIdx + 9
			continue
		}
		name := strings.TrimSpace(rest[:nameEnd])
		if name == "" {
			i = absIdx + 9
			continue
		}
		rest = strings.TrimSpace(rest[nameEnd:])
		if len(rest) == 0 || rest[0] != '(' {
			i = absIdx + 9
			continue
		}
		parenEnd := findMatchingParen(rest, 0)
		if parenEnd < 0 {
			i = absIdx + 9
			continue
		}
		paramStr := strings.TrimSpace(rest[1:parenEnd])
		rest = strings.TrimSpace(rest[parenEnd+1:])
		if len(rest) == 0 || rest[0] != '{' {
			i = absIdx + 9
			continue
		}
		braceEnd := findMatchingBrace(rest, 0)
		if braceEnd < 0 {
			i = absIdx + 9
			continue
		}
		bodyStr := rest[1:braceEnd]
		var fnParams []string
		if paramStr != "" {
			fnParams = []string{paramStr}
		}
		funcs[name] = &Value{
			typ:      TypeFunc,
			fnParams: fnParams,
			fnBody:   bodyStr,
		}
		i = absIdx + 9 + len(rest[:braceEnd+1])
	}
	return funcs
}

// ExtractTopLevelVars finds top-level const/let/var declarations (not inside functions)
// and evaluates them, returning name→Value pairs.
func ExtractTopLevelVars(source string, scope map[string]*Value) {
	// Find const/let/var at the start of a line (top level)
	lines := strings.Split(source, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		// Skip exports, functions, comments
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "/*") {
			continue
		}
		if strings.HasPrefix(line, "export default") || strings.HasPrefix(line, "export const metadata") {
			continue
		}
		if strings.HasPrefix(line, "function ") || strings.HasPrefix(line, "export function") {
			// Skip function body
			depth := strings.Count(line, "{") - strings.Count(line, "}")
			for depth > 0 && i+1 < len(lines) {
				i++
				depth += strings.Count(lines[i], "{") - strings.Count(lines[i], "}")
			}
			continue
		}

		isDecl := false
		for _, kw := range []string{"const ", "let ", "var "} {
			if strings.HasPrefix(line, kw) {
				isDecl = true
				break
			}
		}
		if !isDecl {
			continue
		}

		// Collect the full statement (may span multiple lines)
		stmt := line
		depth := strings.Count(stmt, "[") - strings.Count(stmt, "]") +
			strings.Count(stmt, "{") - strings.Count(stmt, "}") +
			strings.Count(stmt, "(") - strings.Count(stmt, ")")
		for depth > 0 && i+1 < len(lines) {
			i++
			stmt += "\n" + lines[i]
			depth += strings.Count(lines[i], "[") - strings.Count(lines[i], "]") +
				strings.Count(lines[i], "{") - strings.Count(lines[i], "}") +
				strings.Count(lines[i], "(") - strings.Count(lines[i], ")")
		}

		// Evaluate the declaration in the given scope
		tokens := tokenizeCached(stmt)
		ev := &evaluator{tokens: tokens, pos: 0, scope: scope}
		ev.evalStatements()
	}
}

// CallFunc calls a function value with the given props.
func CallFunc(scope map[string]*Value, fn *Value, props map[string]*Value) *Value {
	if fn == nil {
		return Undefined
	}
	// Handle native Go functions — pass props as single object arg
	if fn.native != nil {
		propsObj := NewObj(props)
		return fn.native([]*Value{propsObj})
	}
	// Handle arrow functions
	if fn.str == "__arrow" {
		var args []*Value
		for _, v := range props {
			args = append(args, v)
		}
		return callArrow(int(fn.num), args, scope)
	}
	// Handle regular functions with body
	ev := &evaluator{scope: scope}
	return ev.callFunc(fn, props)
}

// ── Helper functions ────────────────────────────────────

func indexOutsideStrings(s, needle string) int {
	inStr := byte(0)
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inStr != 0 {
			if ch == inStr && (i == 0 || s[i-1] != '\\') {
				inStr = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' || ch == '`' {
			inStr = ch
			continue
		}
		if i+len(needle) <= len(s) && s[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func findMatchingParen(s string, start int) int {
	depth := 0
	inStr := byte(0)
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inStr != 0 {
			if ch == inStr && (i == 0 || s[i-1] != '\\') {
				inStr = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' || ch == '`' {
			inStr = ch
			continue
		}
		if ch == '(' {
			depth++
		} else if ch == ')' {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func findMatchingBrace(s string, start int) int {
	depth := 0
	inStr := byte(0)
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inStr != 0 {
			if ch == inStr && (i == 0 || s[i-1] != '\\') {
				inStr = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' || ch == '`' {
			inStr = ch
			continue
		}
		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
