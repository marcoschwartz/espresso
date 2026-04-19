package espresso

import "testing"

// ─── WeakMap Tests ──────────────────────────────────────

func TestWeakMap_SetGetHas(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const wm = new WeakMap();
		const key1 = { id: 1 };
		const key2 = { id: 2 };
		wm.set(key1, "value1");
		wm.set(key2, "value2");
		return wm.get(key1) + "," + wm.get(key2);
	`)
	if r.String() != "value1,value2" {
		t.Errorf("expected 'value1,value2', got '%s'", r.String())
	}
}

func TestWeakMap_Has(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const wm = new WeakMap();
		const obj = { x: 1 };
		const other = { x: 2 };
		wm.set(obj, true);
		return String(wm.has(obj)) + "," + String(wm.has(other));
	`)
	if r.String() != "true,false" {
		t.Errorf("expected 'true,false', got '%s'", r.String())
	}
}

func TestWeakMap_Delete(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const wm = new WeakMap();
		const key = { a: 1 };
		wm.set(key, "hello");
		const before = wm.has(key);
		wm.delete(key);
		const after = wm.has(key);
		return String(before) + "," + String(after);
	`)
	if r.String() != "true,false" {
		t.Errorf("expected 'true,false', got '%s'", r.String())
	}
}

func TestWeakMap_ObjectKeysOnly(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const wm = new WeakMap();
		const obj = {};
		wm.set(obj, "works");
		return wm.get(obj);
	`)
	if r.String() != "works" {
		t.Errorf("expected 'works', got '%s'", r.String())
	}
}

func TestWeakMap_ArrayAsKey(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const wm = new WeakMap();
		const arr = [1, 2, 3];
		wm.set(arr, "array-value");
		return wm.get(arr);
	`)
	if r.String() != "array-value" {
		t.Errorf("expected 'array-value', got '%s'", r.String())
	}
}

func TestWeakMap_OverwriteValue(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const wm = new WeakMap();
		const key = {};
		wm.set(key, "first");
		wm.set(key, "second");
		return wm.get(key);
	`)
	if r.String() != "second" {
		t.Errorf("expected 'second', got '%s'", r.String())
	}
}

func TestWeakMap_MissingKeyReturnsUndefined(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const wm = new WeakMap();
		const key = {};
		return typeof wm.get(key);
	`)
	if r.String() != "undefined" {
		t.Errorf("expected 'undefined', got '%s'", r.String())
	}
}

// ─── WeakSet Tests ──────────────────────────────────────

func TestWeakSet_AddHas(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const ws = new WeakSet();
		const obj = { id: 1 };
		ws.add(obj);
		return ws.has(obj);
	`)
	if !r.Truthy() {
		t.Error("expected WeakSet.has to return true after add")
	}
}

func TestWeakSet_Delete(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const ws = new WeakSet();
		const obj = {};
		ws.add(obj);
		const before = ws.has(obj);
		ws.delete(obj);
		const after = ws.has(obj);
		return String(before) + "," + String(after);
	`)
	if r.String() != "true,false" {
		t.Errorf("expected 'true,false', got '%s'", r.String())
	}
}

func TestWeakSet_NoDoubleAdd(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const ws = new WeakSet();
		const obj = {};
		ws.add(obj);
		ws.add(obj); // should be idempotent
		return ws.has(obj);
	`)
	if !r.Truthy() {
		t.Error("expected has to be true after double add")
	}
}

func TestWeakSet_DifferentObjects(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const ws = new WeakSet();
		const a = { x: 1 };
		const b = { x: 1 }; // different object, same content
		ws.add(a);
		return String(ws.has(a)) + "," + String(ws.has(b));
	`)
	if r.String() != "true,false" {
		t.Errorf("expected 'true,false' (identity not equality), got '%s'", r.String())
	}
}

func TestWeakSet_Chainable(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const ws = new WeakSet();
		const obj = {};
		ws.add(obj).add({});
		return ws.has(obj);
	`)
	if !r.Truthy() {
		t.Error("expected add() to be chainable")
	}
}
