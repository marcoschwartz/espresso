package espresso

import (
	"testing"
)

// ── forEach ──

func TestForEach_SumWithPlusEquals(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let sum = 0;
		const arr = [1, 2, 3, 4, 5];
		arr.forEach((x) => { sum += x; });
		return sum;
	`)
	if r.Number() != 15 { t.Errorf("expected 15, got %v", r.Number()) }
}

func TestForEach_MultipleItems(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let sum = 0;
		const arr = [10, 20, 30];
		arr.forEach((val) => { sum += val; });
		return sum;
	`)
	if r.Number() != 60 { t.Errorf("expected 60, got %v", r.Number()) }
}

// ── includes ──

func TestIncludes_ArrayTrue(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`[1, 2, 3].includes(2)`)
	if !r.Truthy() { t.Error("should be true") }
}

func TestIncludes_ArrayFalse(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`[1, 2, 3].includes(5)`)
	if r.Truthy() { t.Error("should be false") }
}

func TestIncludes_StringTrue(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"hello world".includes("world")`)
	if !r.Truthy() { t.Error("should be true") }
}

func TestIncludes_StringFalse(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"hello".includes("xyz")`)
	if r.Truthy() { t.Error("should be false") }
}

// ── indexOf ──

func TestIndexOf_Found(t *testing.T) {
	vm := New()
	vm.Set("arr", []interface{}{10.0, 20.0, 30.0})
	r, _ := vm.Eval(`arr.indexOf(20)`)
	if r.Number() != 1 { t.Errorf("expected 1, got %v", r.Number()) }
}

func TestIndexOf_NotFound(t *testing.T) {
	vm := New()
	vm.Set("arr", []interface{}{10.0, 20.0, 30.0})
	r, _ := vm.Eval(`arr.indexOf(99)`)
	if r.Number() != -1 { t.Errorf("expected -1, got %v", r.Number()) }
}

// ── chained assignment ──

func TestChainedAssignment(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`var a, b; b = a = 42; return a + "|" + b;`)
	if r.String() != "42|42" { t.Errorf("got %s", r.String()) }
}

func TestChainedAssignment_Object(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`var x = {}; var y; y = x; x.foo = 1; return y.foo;`)
	if r.Number() != 1 { t.Errorf("got %v", r.Number()) }
}

// ── delete operator ──

// ── globalThis ──

// ── AbortController ──

// ── function.length ──

func TestFunctionLength(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		function f(a, b, c) { return a; }
		const g = (x, y) => x + y;
		return f.length + "|" + g.length;
	`)
	if r.String() != "3|2" { t.Errorf("got %s", r.String()) }
}

// ── AbortController ──

func TestAbortController(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const ac = new AbortController();
		const before = ac.signal.aborted;
		ac.abort();
		return before + "|" + ac.signal.aborted;
	`)
	if r.String() != "false|true" { t.Errorf("got %s", r.String()) }
}

// ── globalThis ──

func TestGlobalThis(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		globalThis.x = 99;
		return typeof globalThis + "|" + globalThis.x;
	`)
	if r.String() != "object|99" { t.Errorf("got %s", r.String()) }
}

// ── delete operator ──

func TestDelete_DotProp(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const o = {a: 1, b: 2, c: 3};
		delete o.a;
		return Object.keys(o).length;
	`)
	if r.Number() != 2 { t.Errorf("expected 2, got %v", r.Number()) }
}

func TestDelete_BracketProp(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const o = {x: 10, y: 20};
		delete o["x"];
		return Object.keys(o).join(",");
	`)
	if r.String() != "y" { t.Errorf("expected 'y', got '%s'", r.String()) }
}

// ── Object.entries ──

func TestObjectEntries(t *testing.T) {
	vm := New()
	vm.Set("obj", map[string]interface{}{"a": 1.0, "b": 2.0})
	r, _ := vm.Eval(`Object.entries(obj).length`)
	if r.Number() != 2 { t.Errorf("expected 2, got %v", r.Number()) }
}

// ── Object.assign ──

func TestObjectAssign(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const target = { a: 1 };
		const result = Object.assign(target, { b: 2, c: 3 });
		return result.b + result.c;
	`)
	if r.Number() != 5 { t.Errorf("expected 5, got %v", r.Number()) }
}

// ── Object.fromEntries ──

func TestObjectFromEntries(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const entries = [["a", 1], ["b", 2]];
		const obj = Object.fromEntries(entries);
		return obj.a + obj.b;
	`)
	if r.Number() != 3 { t.Errorf("expected 3, got %v", r.Number()) }
}

func TestObjectPrototypeHasOwnProperty(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const obj = {a: 1, b: 2};
		const r1 = Object.prototype.hasOwnProperty.call(obj, "a");
		const r2 = Object.prototype.hasOwnProperty.call(obj, "c");
		return r1 + "|" + r2;
	`)
	if r.String() != "true|false" { t.Errorf("got %s", r.String()) }
}

func TestObjectGetOwnPropertyDescriptor(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const d = Object.getOwnPropertyDescriptor({x: 42}, "x");
		return d.value + "|" + d.writable + "|" + d.enumerable;
	`)
	if r.String() != "42|true|true" { t.Errorf("got %s", r.String()) }
}

func TestObjectGetOwnPropertyDescriptor_Missing(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const d = Object.getOwnPropertyDescriptor({x: 1}, "y");
		return d === undefined;
	`)
	if !r.Truthy() { t.Error("expected undefined for missing prop") }
}

func TestObjectFromEntries_Empty(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const obj = Object.fromEntries([]);
		return Object.keys(obj).length;
	`)
	if r.Number() != 0 { t.Errorf("expected 0, got %v", r.Number()) }
}

// ── RegExp basics ──

func TestRegExp_Test(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`/\d+/.test("abc123")`)
	if !r.Truthy() { t.Error("should match digits") }
}

func TestRegExp_TestNoMatch(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`/\d+/.test("abcdef")`)
	if r.Truthy() { t.Error("should not match") }
}

func TestRegExp_Replace(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"hello world".replace(/world/, "espresso")`)
	if r.String() != "hello espresso" { t.Errorf("got %q", r.String()) }
}

func TestRegExp_ReplaceGlobal(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"aaa".replace(/a/g, "b")`)
	if r.String() != "bbb" { t.Errorf("got %q", r.String()) }
}

func TestRegExp_Search(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"hello world".search(/world/)`)
	if r.Number() != 6 { t.Errorf("expected 6, got %v", r.Number()) }
}

func TestRegExp_SearchNotFound(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"hello".search(/xyz/)`)
	if r.Number() != -1 { t.Errorf("expected -1, got %v", r.Number()) }
}

func TestRegExp_Split(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"a,b,c".split(/,/).join("-")`)
	if r.String() != "a-b-c" { t.Errorf("got %q", r.String()) }
}

// ── Class basics ──

func TestClass_Basic(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		class Dog {
			constructor(name) {
				this.name = name;
			}
			bark() {
				return "woof " + this.name;
			}
		}
		const d = new Dog("Rex");
		return d.bark();
	`)
	if r.String() != "woof Rex" { t.Errorf("got %q", r.String()) }
}

func TestClass_Extends(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		class Animal {
			constructor(name) {
				this.name = name;
			}
			speak() {
				return this.name + " speaks";
			}
		}
		class Cat extends Animal {
			constructor(name) {
				super(name);
				this.type = "cat";
			}
		}
		const c = new Cat("Whiskers");
		return c.speak();
	`)
	if r.String() != "Whiskers speaks" { t.Errorf("got %q", r.String()) }
}

func TestClass_Instanceof(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		class Foo {}
		const f = new Foo();
		return f instanceof Foo ? "yes" : "no";
	`)
	if r.String() != "yes" { t.Errorf("got %q", r.String()) }
}

func TestSymbol_Basic(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`typeof Symbol`)
	if r.String() != "function" { t.Errorf("expected 'function', got '%s'", r.String()) }
}

func TestSymbol_Create(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`typeof Symbol("test")`)
	if r.String() != "string" { t.Errorf("expected 'string', got '%s'", r.String()) }
}

func TestSymbol_Unique(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`const a = Symbol("x"); const b = Symbol("x"); return a !== b ? "unique" : "same";`)
	if r.String() != "unique" { t.Errorf("expected 'unique', got '%s'", r.String()) }
}
