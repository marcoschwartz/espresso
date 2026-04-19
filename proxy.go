package espresso

// ─── Proxy ──────────────────────────────────────────────
// Implements JS Proxy with get/set/has/deleteProperty traps.
// new Proxy(target, handler)

type jsProxy struct {
	target  *Value
	handler *Value
}

func newProxyValue(target, handler *Value, scope map[string]*Value) *Value {
	p := &jsProxy{target: target, handler: handler}
	v := &Value{typ: TypeObject, object: make(map[string]*Value), Custom: p}
	v.object["__constructor__"] = newStr("Proxy")
	v.object["__proxy__"] = True

	// Copy target properties so they're visible
	if target.typ == TypeObject && target.object != nil {
		for k, val := range target.object {
			v.object[k] = val
		}
	}

	// Install getter trap: intercepts all property reads
	getTrap := getProxyTrap(handler, "get")
	setTrap := getProxyTrap(handler, "set")

	if getTrap != nil {
		// Store trap for universal property interception (checked in property access)
		tgt := target
		proxyVal := v
		s := scope
		gt := getTrap
		v.object["__proxy_get__"] = NewNativeFunc(func(args []*Value) *Value {
			if len(args) == 0 { return Undefined }
			return callFuncValue(gt, []*Value{tgt, args[0], proxyVal}, s)
		})
	}

	if setTrap != nil {
		if v.getset == nil {
			v.getset = make(map[string]*PropDescriptor)
		}
		if target.typ == TypeObject && target.object != nil {
			for propName := range target.object {
				pn := propName
				desc, ok := v.getset[pn]
				if !ok {
					desc = &PropDescriptor{}
					v.getset[pn] = desc
				}
				desc.Set = NewNativeFunc(func(args []*Value) *Value {
					val := Undefined
					if len(args) > 0 {
						val = args[0]
					}
					callFuncValue(setTrap, []*Value{target, NewStr(pn), val, v}, scope)
					return Undefined
				})
			}
		}
	}

	// has trap — used by "in" operator (handled at evaluator level)
	hasTrap := getProxyTrap(handler, "has")
	if hasTrap != nil {
		v.object["__has__"] = NewNativeFunc(func(args []*Value) *Value {
			if len(args) == 0 {
				return False
			}
			return callFuncValue(hasTrap, []*Value{target, args[0]}, scope)
		})
	}

	// deleteProperty trap
	deleteTrap := getProxyTrap(handler, "deleteProperty")
	if deleteTrap != nil {
		v.object["__delete__"] = NewNativeFunc(func(args []*Value) *Value {
			if len(args) == 0 {
				return False
			}
			return callFuncValue(deleteTrap, []*Value{target, args[0]}, scope)
		})
	}

	// apply trap (for function proxies)
	applyTrap := getProxyTrap(handler, "apply")
	if applyTrap != nil {
		v.object["__apply__"] = NewNativeFunc(func(args []*Value) *Value {
			argsArr := NewArr(args)
			return callFuncValue(applyTrap, []*Value{target, Undefined, argsArr}, scope)
		})
	}

	return v
}

func getProxyTrap(handler *Value, name string) *Value {
	if handler == nil || handler.typ != TypeObject || handler.object == nil {
		return nil
	}
	trap, ok := handler.object[name]
	if !ok || trap.typ != TypeFunc {
		return nil
	}
	return trap
}
