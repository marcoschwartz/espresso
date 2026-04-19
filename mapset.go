package espresso

import "unsafe"

// ─── Map ────────────────────────────────────────────────────

type jsMap struct {
	keys   []string
	values map[string]*Value
}

func newMapValue(initArr *Value) *Value {
	m := &jsMap{values: make(map[string]*Value)}
	v := &Value{typ: TypeObject, object: make(map[string]*Value), Custom: m}
	v.object["__constructor__"] = newStr("Map")

	// Initialize from array of [key, value] pairs
	if initArr != nil && initArr.typ == TypeArray {
		for _, entry := range initArr.array {
			if entry.typ == TypeArray && len(entry.array) >= 2 {
				key := entry.array[0].toStr()
				if _, exists := m.values[key]; !exists {
					m.keys = append(m.keys, key)
				}
				m.values[key] = entry.array[1]
			}
		}
	}

	v.object["set"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) < 2 { return v }
		key := args[0].toStr()
		if _, exists := m.values[key]; !exists {
			m.keys = append(m.keys, key)
		}
		m.values[key] = args[1]
		return v // return map for chaining
	})

	v.object["get"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return Undefined }
		key := args[0].toStr()
		if val, ok := m.values[key]; ok { return val }
		return Undefined
	})

	v.object["has"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return False }
		_, ok := m.values[args[0].toStr()]
		return newBool(ok)
	})

	v.object["delete"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return False }
		key := args[0].toStr()
		if _, ok := m.values[key]; ok {
			delete(m.values, key)
			for i, k := range m.keys {
				if k == key { m.keys = append(m.keys[:i], m.keys[i+1:]...); break }
			}
			return True
		}
		return False
	})

	v.object["clear"] = NewNativeFunc(func(args []*Value) *Value {
		m.keys = nil
		m.values = make(map[string]*Value)
		return Undefined
	})

	v.object["forEach"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return Undefined }
		cb := args[0]
		for _, key := range m.keys {
			val := m.values[key]
			callFuncValue(cb, []*Value{val, newStr(key)}, nil)
		}
		return Undefined
	})

	v.object["entries"] = NewNativeFunc(func(args []*Value) *Value {
		arr := make([]*Value, len(m.keys))
		for i, key := range m.keys {
			arr[i] = newArr([]*Value{newStr(key), m.values[key]})
		}
		return newArr(arr)
	})

	v.object["keys"] = NewNativeFunc(func(args []*Value) *Value {
		arr := make([]*Value, len(m.keys))
		for i, key := range m.keys { arr[i] = newStr(key) }
		return newArr(arr)
	})

	v.object["values"] = NewNativeFunc(func(args []*Value) *Value {
		arr := make([]*Value, len(m.keys))
		for i, key := range m.keys { arr[i] = m.values[key] }
		return newArr(arr)
	})

	// size as a getter
	v.DefineGetter("size", func(args []*Value) *Value {
		return newNum(float64(len(m.values)))
	})

	return v
}

// ─── Set ────────────────────────────────────────────────────

type jsSet struct {
	items  []string    // insertion order
	values map[string]*Value
}

func newSetValue(initArr *Value) *Value {
	s := &jsSet{values: make(map[string]*Value)}
	v := &Value{typ: TypeObject, object: make(map[string]*Value), Custom: s}
	v.object["__constructor__"] = newStr("Set")

	addFn := func(val *Value) {
		key := val.toStr()
		if _, exists := s.values[key]; !exists {
			s.items = append(s.items, key)
			s.values[key] = val
		}
	}

	// Initialize from array if provided
	if initArr != nil && initArr.typ == TypeArray {
		for _, item := range initArr.array {
			addFn(item)
		}
	}

	v.object["add"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) > 0 { addFn(args[0]) }
		return v
	})

	v.object["has"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return False }
		_, ok := s.values[args[0].toStr()]
		return newBool(ok)
	})

	v.object["delete"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return False }
		key := args[0].toStr()
		if _, ok := s.values[key]; ok {
			delete(s.values, key)
			for i, k := range s.items {
				if k == key { s.items = append(s.items[:i], s.items[i+1:]...); break }
			}
			return True
		}
		return False
	})

	v.object["clear"] = NewNativeFunc(func(args []*Value) *Value {
		s.items = nil
		s.values = make(map[string]*Value)
		return Undefined
	})

	v.object["forEach"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return Undefined }
		cb := args[0]
		for _, key := range s.items {
			val := s.values[key]
			callFuncValue(cb, []*Value{val, val}, nil)
		}
		return Undefined
	})

	v.object["values"] = NewNativeFunc(func(args []*Value) *Value {
		arr := make([]*Value, len(s.items))
		for i, key := range s.items { arr[i] = s.values[key] }
		return newArr(arr)
	})

	v.DefineGetter("size", func(args []*Value) *Value {
		return newNum(float64(len(s.values)))
	})

	return v
}

// ─── WeakMap ─────────────────────────────────────────────
// Uses pointer identity for keys (objects/arrays only, not primitives).

func ptrKey(v *Value) uintptr {
	return uintptr(unsafe.Pointer(v))
}

func newWeakMapValue() *Value {
	store := make(map[uintptr]*Value)
	v := &Value{typ: TypeObject, object: make(map[string]*Value)}
	v.object["__constructor__"] = newStr("WeakMap")

	v.object["set"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) < 2 { return v }
		key := args[0]
		if key.typ != TypeObject && key.typ != TypeArray && key.typ != TypeFunc {
			return v // WeakMap keys must be objects
		}
		store[ptrKey(key)] = args[1]
		return v
	})

	v.object["get"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return Undefined }
		if val, ok := store[ptrKey(args[0])]; ok {
			return val
		}
		return Undefined
	})

	v.object["has"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return False }
		_, ok := store[ptrKey(args[0])]
		return newBool(ok)
	})

	v.object["delete"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return False }
		k := ptrKey(args[0])
		if _, ok := store[k]; ok {
			delete(store, k)
			return True
		}
		return False
	})

	return v
}

// ─── WeakSet ─────────────────────────────────────────────

func newWeakSetValue() *Value {
	store := make(map[uintptr]bool)
	v := &Value{typ: TypeObject, object: make(map[string]*Value)}
	v.object["__constructor__"] = newStr("WeakSet")

	v.object["add"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return v }
		key := args[0]
		if key.typ != TypeObject && key.typ != TypeArray && key.typ != TypeFunc {
			return v
		}
		store[ptrKey(key)] = true
		return v
	})

	v.object["has"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return False }
		return newBool(store[ptrKey(args[0])])
	})

	v.object["delete"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return False }
		k := ptrKey(args[0])
		if store[k] {
			delete(store, k)
			return True
		}
		return False
	})

	return v
}
