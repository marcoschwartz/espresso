package espresso

import (
	"strings"
	"sync"
)

// ── Token Cache ─────────────────────────────────────────
// Caches tokenized code so repeated Eval/Run calls skip the lexer.

var (
	tokenCache   = make(map[string][]tok)
	tokenCacheMu sync.RWMutex
)

func tokenizeCached(code string) []tok {
	tokenCacheMu.RLock()
	cached, ok := tokenCache[code]
	tokenCacheMu.RUnlock()
	if ok {
		// Return a copy so the evaluator can advance pos without affecting cache
		result := make([]tok, len(cached))
		copy(result, cached)
		return result
	}

	tokens := tokenize(code)
	tokenCacheMu.Lock()
	tokenCache[code] = tokens
	tokenCacheMu.Unlock()

	result := make([]tok, len(tokens))
	copy(result, tokens)
	return result
}

// ── Scope Pool ──────────────────────────────────────────
// Reuses scope maps to reduce GC pressure in callbacks and loops.

var scopePool = sync.Pool{
	New: func() interface{} {
		return make(map[string]*Value, 16)
	},
}

func getScope(parent map[string]*Value) map[string]*Value {
	s := scopePool.Get().(map[string]*Value)
	// Clear and copy parent
	for k := range s {
		delete(s, k)
	}
	for k, v := range parent {
		s[k] = v
	}
	return s
}

func putScope(s map[string]*Value) {
	scopePool.Put(s)
}

// ── String Builder Pool ─────────────────────────────────

var sbPool = sync.Pool{
	New: func() interface{} {
		return &strings.Builder{}
	},
}

func getSB() *strings.Builder {
	sb := sbPool.Get().(*strings.Builder)
	sb.Reset()
	return sb
}

func putSB(sb *strings.Builder) {
	sbPool.Put(sb)
}

// ── Value Interning ─────────────────────────────────────
// Pre-allocated values for common integers and strings.

const intCacheSize = 65537

var (
	intCache [intCacheSize]*Value // 0-16384
	negCache [129]*Value          // -1 to -128
	emptyStr = &Value{typ: TypeString, str: ""}
)

func init() {
	for i := 0; i < intCacheSize; i++ {
		intCache[i] = &Value{typ: TypeNumber, num: float64(i)}
	}
	for i := 1; i <= 128; i++ {
		negCache[i] = &Value{typ: TypeNumber, num: float64(-i)}
	}
}

// internNum returns a cached Value for small integers, or allocates a new one.
func internNum(n float64) *Value {
	if n >= 0 && n < intCacheSize {
		i := int(n)
		if float64(i) == n {
			return intCache[i]
		}
	}
	if n < 0 && n >= -128 {
		i := int(-n)
		if float64(-i) == n {
			return negCache[i]
		}
	}
	return &Value{typ: TypeNumber, num: n}
}

// internStr returns a cached Value for empty string, or allocates a new one.
func internStr(s string) *Value {
	if s == "" {
		return emptyStr
	}
	return &Value{typ: TypeString, str: s}
}
