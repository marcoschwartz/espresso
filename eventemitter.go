package espresso

// ─── EventEmitter ────────────────────────────────────────
// Node.js-compatible EventEmitter available via require('events').

// newEventEmitterInstance creates an EventEmitter object with on/off/once/emit/etc.
func newEventEmitterInstance() *Value {
	listeners := make(map[string][]*Value)    // event → [callback, ...]
	onceFlags := make(map[string][]bool)       // parallel to listeners — true means one-shot

	obj := NewObj(make(map[string]*Value))
	obj.object["__constructor__"] = newStr("EventEmitter")

	obj.object["on"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) < 2 { return obj }
		event := args[0].toStr()
		listeners[event] = append(listeners[event], args[1])
		onceFlags[event] = append(onceFlags[event], false)
		return obj
	})

	obj.object["addListener"] = obj.object["on"]

	obj.object["once"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) < 2 { return obj }
		event := args[0].toStr()
		listeners[event] = append(listeners[event], args[1])
		onceFlags[event] = append(onceFlags[event], true)
		return obj
	})

	obj.object["off"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) < 2 { return obj }
		event := args[0].toStr()
		cb := args[1]
		cbs := listeners[event]
		flags := onceFlags[event]
		for i, c := range cbs {
			if c == cb {
				listeners[event] = append(cbs[:i], cbs[i+1:]...)
				onceFlags[event] = append(flags[:i], flags[i+1:]...)
				break
			}
		}
		return obj
	})

	obj.object["removeListener"] = obj.object["off"]

	obj.object["removeAllListeners"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) > 0 {
			event := args[0].toStr()
			delete(listeners, event)
			delete(onceFlags, event)
		} else {
			for k := range listeners { delete(listeners, k) }
			for k := range onceFlags { delete(onceFlags, k) }
		}
		return obj
	})

	obj.object["emit"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return False }
		event := args[0].toStr()
		cbs := listeners[event]
		if len(cbs) == 0 { return False }
		cbArgs := args[1:]

		// Collect indices to remove (once listeners)
		var toRemove []int
		for i, cb := range cbs {
			callFuncValue(cb, cbArgs, nil)
			if onceFlags[event][i] {
				toRemove = append(toRemove, i)
			}
		}
		// Remove once listeners in reverse order
		for i := len(toRemove) - 1; i >= 0; i-- {
			idx := toRemove[i]
			listeners[event] = append(listeners[event][:idx], listeners[event][idx+1:]...)
			onceFlags[event] = append(onceFlags[event][:idx], onceFlags[event][idx+1:]...)
		}
		return True
	})

	obj.object["listenerCount"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return newNum(0) }
		return newNum(float64(len(listeners[args[0].toStr()])))
	})

	obj.object["listeners"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return newArr(nil) }
		cbs := listeners[args[0].toStr()]
		arr := make([]*Value, len(cbs))
		copy(arr, cbs)
		return newArr(arr)
	})

	obj.object["eventNames"] = NewNativeFunc(func(args []*Value) *Value {
		names := make([]*Value, 0, len(listeners))
		for k, v := range listeners {
			if len(v) > 0 {
				names = append(names, newStr(k))
			}
		}
		return newArr(names)
	})

	obj.object["setMaxListeners"] = NewNativeFunc(func(args []*Value) *Value {
		return obj // no-op, no limit
	})

	return obj
}

// builtinEvents returns the 'events' module: { EventEmitter, default: EventEmitter }
func (ms *ModuleSystem) builtinEvents() *Value {
	// EventEmitter constructor
	ctor := NewNativeFunc(func(args []*Value) *Value {
		return newEventEmitterInstance()
	})
	ctor.object = make(map[string]*Value)
	ctor.object["__class__"] = newStr("EventEmitter")

	m := NewObj(make(map[string]*Value))
	m.object["EventEmitter"] = ctor
	m.object["default"] = ctor
	return m
}
