package espresso

import "testing"

func TestDestructure_ObjRename(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`const {x: renamed} = {x: 42}; return renamed;`)
	if r.Number() != 42 { t.Errorf("expected 42, got %v", r.Number()) }
}

func TestDestructure_ObjDefault(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`const {x = 10} = {}; return x;`)
	if r.Number() != 10 { t.Errorf("expected 10, got %v", r.Number()) }
}

func TestDestructure_ObjDefaultWithValue(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`const {x = 10} = {x: 99}; return x;`)
	if r.Number() != 99 { t.Errorf("expected 99, got %v", r.Number()) }
}

func TestDestructure_ObjRest(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`const {a, ...rest} = {a: 1, b: 2, c: 3}; return a;`)
	if r.Number() != 1 { t.Errorf("expected 1, got %v", r.Number()) }
}

func TestDestructure_ObjRestKeys(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`const {a, ...rest} = {a: 1, b: 2, c: 3}; return Object.keys(rest).length;`)
	if r.Number() != 2 { t.Errorf("expected 2, got %v", r.Number()) }
}

func TestDestructure_ObjMultipleDefaults(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`const {x = 1, y = 2, z = 3} = {y: 20}; return x + y + z;`)
	if r.Number() != 24 { t.Errorf("expected 24 (1+20+3), got %v", r.Number()) }
}

// ── Assignment operators ──

func TestAssign_NullishNull(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`let x = null; x ??= 42; return x;`)
	if r.Number() != 42 { t.Errorf("expected 42, got %v", r.Number()) }
}

func TestAssign_NullishDefined(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`let x = 5; x ??= 42; return x;`)
	if r.Number() != 5 { t.Errorf("expected 5, got %v", r.Number()) }
}

func TestAssign_OrFalsy(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`let x = 0; x ||= 42; return x;`)
	if r.Number() != 42 { t.Errorf("expected 42, got %v", r.Number()) }
}

func TestAssign_OrTruthy(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`let x = 5; x ||= 42; return x;`)
	if r.Number() != 5 { t.Errorf("expected 5, got %v", r.Number()) }
}

func TestAssign_AndTruthy(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`let x = 1; x &&= 42; return x;`)
	if r.Number() != 42 { t.Errorf("expected 42, got %v", r.Number()) }
}

func TestAssign_AndFalsy(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`let x = 0; x &&= 42; return x;`)
	if r.Number() != 0 { t.Errorf("expected 0, got %v", r.Number()) }
}

// ── in operator ──

func TestIn_ObjectTrue(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"a" in {a: 1, b: 2}`)
	if !r.Bool() { t.Error("expected true") }
}

func TestIn_ObjectFalse(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"c" in {a: 1}`)
	if r.Bool() { t.Error("expected false") }
}

func TestIn_Array(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`0 in [10, 20]`)
	if !r.Bool() { t.Error("expected true") }
}

// ── Computed properties ──

func TestComputed_ObjectKey(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`const k = "x"; const o = {[k]: 42}; return o.x;`)
	if r.Number() != 42 { t.Errorf("expected 42, got %v", r.Number()) }
}

func TestComputed_DynamicKey(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`const o = {["a" + "b"]: 99}; return o.ab;`)
	if r.Number() != 99 { t.Errorf("expected 99, got %v", r.Number()) }
}

// ── Map ──

func TestMap_SetGet(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`const m = new Map(); m.set("a", 1); return m.get("a");`)
	if r.Number() != 1 { t.Errorf("expected 1, got %v", r.Number()) }
}

func TestMap_Has(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`const m = new Map(); m.set("a", 1); return m.has("a") ? "y" : "n";`)
	if r.String() != "y" { t.Errorf("expected y, got %s", r.String()) }
}

func TestMap_Size(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`const m = new Map(); m.set("a", 1); m.set("b", 2); return m.size;`)
	if r.Number() != 2 { t.Errorf("expected 2, got %v", r.Number()) }
}

func TestMap_Delete(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`const m = new Map(); m.set("a", 1); m.set("b", 2); m.delete("a"); return m.size;`)
	if r.Number() != 1 { t.Errorf("expected 1, got %v", r.Number()) }
}

func TestMapForEach_ArrowExpr(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const m = new Map();
		m.set("a", 1);
		m.set("b", 2);
		const keys = [];
		m.forEach((v, k) => keys.push(k));
		return keys.join(",");
	`)
	if r.String() != "a,b" { t.Errorf("expected 'a,b', got '%s'", r.String()) }
}

func TestSetSpread(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const s = new Set([1, 2, 3]);
		const arr = [...s];
		return arr.length + arr[0] + arr[1] + arr[2];
	`)
	if r.Number() != 9 { t.Errorf("expected 9 (3+1+2+3), got %v", r.Number()) }
}

func TestArrayFrom_Set(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const s = new Set([1, 2, 3]);
		const arr = Array.from(s);
		return arr.length;
	`)
	if r.Number() != 3 { t.Errorf("expected 3, got %v", r.Number()) }
}

func TestSpreadInFunctionCall(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		function add(a, b, c) { return a + b + c; }
		const args = [1, 2, 3];
		return add(...args);
	`)
	if r.Number() != 6 { t.Errorf("expected 6, got %v", r.Number()) }
}

func TestDefaultParams_Function(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		function greet(name = "world") { return "hello " + name; }
		return greet() + "|" + greet("bob");
	`)
	if r.String() != "hello world|hello bob" {
		t.Errorf("expected 'hello world|hello bob', got '%s'", r.String())
	}
}

func TestRestParams_Function(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		function test(a, b, ...rest) { return rest.length; }
		return test(1, 2, 3, 4, 5);
	`)
	if r.Number() != 3 { t.Errorf("expected 3, got %v", r.Number()) }
}

func TestRestParams_OnlyRest(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		function all(...args) { return args.join("-"); }
		return all("a", "b", "c");
	`)
	if r.String() != "a-b-c" { t.Errorf("expected 'a-b-c', got '%s'", r.String()) }
}

func TestMap_ConstructorWithEntries(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const m = new Map([["a", 1], ["b", 2], ["c", 3]]);
		return m.get("a") + m.get("b") + m.get("c") + m.size;
	`)
	if r.Number() != 9 { t.Errorf("expected 9 (1+2+3+3), got %v", r.Number()) }
}

// ── Set ──

func TestSet_AddHas(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`const s = new Set(); s.add(1); return s.has(1) ? "y" : "n";`)
	if r.String() != "y" { t.Errorf("expected y, got %s", r.String()) }
}

func TestSet_Dedup(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`const s = new Set(); s.add(1); s.add(2); s.add(1); return s.size;`)
	if r.Number() != 2 { t.Errorf("expected 2, got %v", r.Number()) }
}

func TestSet_InitFromArray(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`const s = new Set([1, 2, 3]); return s.size;`)
	if r.Number() != 3 { t.Errorf("expected 3, got %v", r.Number()) }
}

// ── Comma operator ──

func TestComma_ReturnsLast(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`(1, 2, 3)`)
	if r.Number() != 3 { t.Errorf("expected 3, got %v", r.Number()) }
}

func TestComma_TwoValues(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`("a", "b")`)
	if r.String() != "b" { t.Errorf("expected 'b', got %q", r.String()) }
}

// ── for...of string ──

func TestForOf_String(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`let r = ""; for (const c of "abc") { r += c; } return r;`)
	if r.String() != "abc" { t.Errorf("expected 'abc', got %q", r.String()) }
}

// ── Param destructuring ──

func TestParamDestructure_Function(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`function f({x, y}) { return x + y; } return f({x: 3, y: 4});`)
	if r.Number() != 7 { t.Errorf("expected 7, got %v", r.Number()) }
}

func TestParamDestructure_Arrow(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`const f = ({x}) => x * 2; return f({x: 5});`)
	if r.Number() != 10 { t.Errorf("expected 10, got %v", r.Number()) }
}

func TestParamDestructure_ArrowMultiple(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`const f = ({a, b, c}) => a + b + c; return f({a: 1, b: 2, c: 3});`)
	if r.Number() != 6 { t.Errorf("expected 6, got %v", r.Number()) }
}

func TestDestructure_ObjRenameAndDefault(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`const {x: myX} = {x: 42}; return myX;`)
	if r.Number() != 42 { t.Errorf("expected 42, got %v", r.Number()) }
}
