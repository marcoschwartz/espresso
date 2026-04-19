package espresso

import "sync"

// ─── Promise Support ─────────────────────────────────────────
// Implements JS Promises with .then/.catch/.finally chaining,
// Promise.resolve/reject/all, and a microtask queue.

// PromiseState represents the state of a Promise.
type PromiseState int

const (
	PromisePending  PromiseState = iota
	PromiseFulfilled
	PromiseRejected
)

// promise is the internal state of a JS Promise.
type promise struct {
	state    PromiseState
	value    *Value  // resolved value or rejection reason
	then     []*thenHandler
	mu       sync.Mutex
}

type thenHandler struct {
	onFulfilled *Value // callback function or nil
	onRejected  *Value // callback function or nil
	next        *promise // the promise returned by .then()
}

// newPromiseValue creates a Promise value wrapping internal state.
func newPromiseValue(p *promise) *Value {
	v := &Value{typ: TypeObject, object: make(map[string]*Value)}
	v.object["__promise__"] = &Value{typ: TypeCustom, Custom: p}
	v.object["__constructor__"] = newStr("Promise")
	return v
}

// getPromise extracts the promise from a Value, or nil if not a promise.
func getPromise(v *Value) *promise {
	if v == nil || v.typ != TypeObject || v.object == nil {
		return nil
	}
	pv, ok := v.object["__promise__"]
	if !ok || pv.typ != TypeCustom {
		return nil
	}
	p, ok := pv.Custom.(*promise)
	if !ok {
		return nil
	}
	return p
}

// resolvePromise fulfills a promise and runs callbacks.
func resolvePromise(p *promise, val *Value, scope map[string]*Value) {
	p.mu.Lock()
	if p.state != PromisePending {
		p.mu.Unlock()
		return
	}
	p.state = PromiseFulfilled
	p.value = val
	handlers := make([]*thenHandler, len(p.then))
	copy(handlers, p.then)
	p.mu.Unlock()

	for _, h := range handlers {
		runThenHandler(h, val, true, scope)
	}
}

// rejectPromise rejects a promise and runs callbacks.
func rejectPromise(p *promise, reason *Value, scope map[string]*Value) {
	p.mu.Lock()
	if p.state != PromisePending {
		p.mu.Unlock()
		return
	}
	p.state = PromiseRejected
	p.value = reason
	handlers := make([]*thenHandler, len(p.then))
	copy(handlers, p.then)
	p.mu.Unlock()

	for _, h := range handlers {
		runThenHandler(h, reason, false, scope)
	}
}

// runThenHandler executes a .then handler and resolves/rejects the chained promise.
func runThenHandler(h *thenHandler, val *Value, fulfilled bool, scope map[string]*Value) {
	var callback *Value
	if fulfilled {
		callback = h.onFulfilled
	} else {
		callback = h.onRejected
	}

	if callback != nil && callback.typ == TypeFunc {
		result := callFuncValue(callback, []*Value{val}, scope)
		if h.next != nil {
			// If result is itself a promise, chain it
			if rp := getPromise(result); rp != nil {
				rp.mu.Lock()
				if rp.state == PromiseFulfilled {
					rp.mu.Unlock()
					resolvePromise(h.next, rp.value, scope)
				} else if rp.state == PromiseRejected {
					rp.mu.Unlock()
					rejectPromise(h.next, rp.value, scope)
				} else {
					rp.then = append(rp.then, &thenHandler{
						onFulfilled: NewNativeFunc(func(args []*Value) *Value {
							v := Undefined
							if len(args) > 0 { v = args[0] }
							resolvePromise(h.next, v, scope)
							return Undefined
						}),
						onRejected: NewNativeFunc(func(args []*Value) *Value {
							v := Undefined
							if len(args) > 0 { v = args[0] }
							rejectPromise(h.next, v, scope)
							return Undefined
						}),
					})
					rp.mu.Unlock()
				}
			} else {
				resolvePromise(h.next, result, scope)
			}
		}
	} else {
		// No handler — pass through
		if h.next != nil {
			if fulfilled {
				resolvePromise(h.next, val, scope)
			} else {
				rejectPromise(h.next, val, scope)
			}
		}
	}
}

// callFuncValue calls a Value function with args.
// CallFuncValue calls a Value function with args (exported for embedders).
func CallFuncValue(fn *Value, args []*Value, scope map[string]*Value) *Value {
	return callFuncValueInternal(fn, args, scope)
}

func callFuncValue(fn *Value, args []*Value, scope map[string]*Value) *Value {
	return callFuncValueInternal(fn, args, scope)
}

func callFuncValueInternal(fn *Value, args []*Value, scope map[string]*Value) *Value {
	if fn == nil {
		return Undefined
	}
	if fn.native != nil {
		return fn.native(args)
	}
	if fn.str == "__arrow" {
		// For arrow functions, use their captured scope so mutations are visible
		// to the code that defined the arrow (closure semantics).
		arrowRegistryMu.Lock()
		af, ok := arrowRegistry[int(fn.num)]
		arrowRegistryMu.Unlock()
		if ok && af.scope != nil {
			return callArrow(int(fn.num), args, af.scope)
		}
		if scope == nil {
			scope = make(map[string]*Value)
		}
		return callArrow(int(fn.num), args, scope)
	}
	// Regular function with body — try bytecode
	if fn.fnBody != "" {
		if scope == nil {
			scope = make(map[string]*Value)
		}
		props := make(map[string]*Value, len(args))
		if len(fn.fnParams) > 0 {
			params := splitParams(fn.fnParams[0])
			for i, p := range params {
				if i < len(args) {
					props[p] = args[i]
				}
			}
		}
		ev := &evaluator{scope: scope}
		return ev.callFunc(fn, props)
	}
	return Undefined
}

// promiseThen implements .then(onFulfilled, onRejected) and returns a new promise.
func promiseThen(p *promise, onFulfilled, onRejected *Value, scope map[string]*Value) *Value {
	next := &promise{state: PromisePending}
	h := &thenHandler{
		onFulfilled: onFulfilled,
		onRejected:  onRejected,
		next:        next,
	}

	p.mu.Lock()
	if p.state == PromisePending {
		p.then = append(p.then, h)
		p.mu.Unlock()
	} else {
		p.mu.Unlock()
		// Already settled — run immediately
		runThenHandler(h, p.value, p.state == PromiseFulfilled, scope)
	}

	pv := newPromiseValue(next)
	registerPromiseMethods(pv, next, scope)
	return pv
}

// registerPromiseMethods adds .then/.catch/.finally as native methods on a promise Value.
func registerPromiseMethods(v *Value, p *promise, scope map[string]*Value) {
	v.object["then"] = NewNativeFunc(func(args []*Value) *Value {
		var onF, onR *Value
		if len(args) > 0 && args[0].typ == TypeFunc { onF = args[0] }
		if len(args) > 1 && args[1].typ == TypeFunc { onR = args[1] }
		return promiseThen(p, onF, onR, scope)
	})
	v.object["catch"] = NewNativeFunc(func(args []*Value) *Value {
		var onR *Value
		if len(args) > 0 && args[0].typ == TypeFunc { onR = args[0] }
		return promiseThen(p, nil, onR, scope)
	})
	v.object["finally"] = NewNativeFunc(func(args []*Value) *Value {
		var onF *Value
		if len(args) > 0 && args[0].typ == TypeFunc { onF = args[0] }
		next := &promise{state: PromisePending}
		h := &thenHandler{
			onFulfilled: NewNativeFunc(func(fargs []*Value) *Value {
				if onF != nil { callFuncValue(onF, nil, scope) }
				v := Undefined
				if len(fargs) > 0 { v = fargs[0] }
				return v
			}),
			onRejected: NewNativeFunc(func(rargs []*Value) *Value {
				if onF != nil { callFuncValue(onF, nil, scope) }
				// Re-reject with original reason
				v := Undefined
				if len(rargs) > 0 { v = rargs[0] }
				return newThrow(v)
			}),
			next: next,
		}
		p.mu.Lock()
		if p.state == PromisePending {
			p.then = append(p.then, h)
			p.mu.Unlock()
		} else {
			p.mu.Unlock()
			runThenHandler(h, p.value, p.state == PromiseFulfilled, scope)
		}
		return newPromiseValue(next)
	})
}

// MakeResolvedPromise creates an already-resolved promise Value (exported for embedders).
func MakeResolvedPromise(val *Value) *Value {
	p := &promise{state: PromiseFulfilled, value: val}
	v := newPromiseValue(p)
	registerPromiseMethods(v, p, nil)
	return v
}

// MakeRejectedPromise creates an already-rejected promise Value (exported for embedders).
func MakeRejectedPromise(reason *Value) *Value {
	p := &promise{state: PromiseRejected, value: reason}
	v := newPromiseValue(p)
	registerPromiseMethods(v, p, nil)
	return v
}

// MakePromise creates a pending promise and returns it along with resolve/reject functions.
func MakePromise() (promiseVal *Value, resolve func(*Value), reject func(*Value)) {
	p := &promise{state: PromisePending}
	v := newPromiseValue(p)
	registerPromiseMethods(v, p, nil)
	return v, func(val *Value) {
		resolvePromise(p, val, nil)
	}, func(reason *Value) {
		rejectPromise(p, reason, nil)
	}
}
