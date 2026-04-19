package espresso

import (
	"fmt"
	"sync/atomic"
)

// Symbol support — basic implementation for library compatibility.
// Symbols are unique identifiers. We represent them as string values
// with a unique prefix that can't collide with normal strings.

var symbolCounter int64

// newSymbol creates a unique symbol value.
func newSymbol(description string) *Value {
	id := atomic.AddInt64(&symbolCounter, 1)
	// Create a unique string that looks like a symbol
	unique := fmt.Sprintf("@@symbol_%d_%s", id, description)
	v := &Value{typ: TypeString, str: unique}
	return v
}

// RegisterSymbolGlobals adds Symbol and well-known symbols to a VM scope.
func RegisterSymbolGlobals(scope map[string]*Value) {
	// Symbol function: Symbol("description") → unique value
	symbolFn := NewNativeFunc(func(args []*Value) *Value {
		desc := ""
		if len(args) > 0 { desc = args[0].toStr() }
		return newSymbol(desc)
	})

	scope["Symbol"] = symbolFn

	// Note: Symbol.iterator, Symbol.toPrimitive etc. would go on symbolFn.object
	// but NativeFunc doesn't have an object map. For now, typeof Symbol === "function"
	// which is what libraries check.
}
