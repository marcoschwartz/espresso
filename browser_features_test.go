package espresso

import (
	"testing"
)

// ══════════════════════════════════════════════════════════════
// Feature 1: throw + Error objects
// ══════════════════════════════════════════════════════════════

func TestThrow_BasicCatch(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let result = "";
		try {
			throw new Error("boom");
		} catch (e) {
			result = e.message;
		}
		return result;
	`)
	if r.String() != "boom" { t.Errorf("expected 'boom', got '%s'", r.String()) }
}

func TestThrow_TypeError(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let result = "";
		try {
			throw new TypeError("not a function");
		} catch (e) {
			result = e.name + ": " + e.message;
		}
		return result;
	`)
	if r.String() != "TypeError: not a function" {
		t.Errorf("expected 'TypeError: not a function', got '%s'", r.String())
	}
}

func TestThrow_StringValue(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let result = "";
		try {
			throw "simple error";
		} catch (e) {
			result = e;
		}
		return result;
	`)
	if r.String() != "simple error" { t.Errorf("expected 'simple error', got '%s'", r.String()) }
}

func TestThrow_Finally(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let result = "";
		try {
			throw new Error("fail");
		} catch (e) {
			result = "caught";
		} finally {
			result = result + "+finally";
		}
		return result;
	`)
	if r.String() != "caught+finally" { t.Errorf("expected 'caught+finally', got '%s'", r.String()) }
}

func TestThrow_PropagatesThroughLoop(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let result = "";
		try {
			for (let i = 0; i < 5; i++) {
				if (i === 3) throw new Error("at 3");
			}
		} catch (e) {
			result = e.message;
		}
		return result;
	`)
	if r.String() != "at 3" { t.Errorf("expected 'at 3', got '%s'", r.String()) }
}

func TestThrow_ErrorStack(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const e = new Error("test");
		return e.stack;
	`)
	if r.String() != "Error: test" { t.Errorf("expected 'Error: test', got '%s'", r.String()) }
}

func TestThrow_Rethrow(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let result = "";
		try {
			try {
				throw new Error("inner");
			} catch (e) {
				throw new Error("rethrown: " + e.message);
			}
		} catch (e) {
			result = e.message;
		}
		return result;
	`)
	if r.String() != "rethrown: inner" { t.Errorf("expected 'rethrown: inner', got '%s'", r.String()) }
}

// ══════════════════════════════════════════════════════════════
// Feature 2: for...in
// ══════════════════════════════════════════════════════════════

func TestForIn_Object(t *testing.T) {
	vm := New()
	vm.Set("obj", map[string]interface{}{"a": 1.0, "b": 2.0, "c": 3.0})
	r, _ := vm.Run(`
		let keys = [];
		for (const key in obj) {
			keys.push(key);
		}
		return keys.sort().join(",");
	`)
	if r.String() != "a,b,c" { t.Errorf("expected 'a,b,c', got '%s'", r.String()) }
}

func TestForIn_ObjectValues(t *testing.T) {
	vm := New()
	vm.Set("obj", map[string]interface{}{"x": 10.0, "y": 20.0})
	r, _ := vm.Run(`
		let sum = 0;
		for (const key in obj) {
			sum += obj[key];
		}
		return sum;
	`)
	if r.Number() != 30 { t.Errorf("expected 30, got %v", r.Number()) }
}

func TestForIn_Array(t *testing.T) {
	vm := New()
	vm.Set("arr", []interface{}{"a", "b", "c"})
	r, _ := vm.Run(`
		let indices = [];
		for (const i in arr) {
			indices.push(i);
		}
		return indices.join(",");
	`)
	if r.String() != "0,1,2" { t.Errorf("expected '0,1,2', got '%s'", r.String()) }
}

func TestForIn_BreakContinue(t *testing.T) {
	vm := New()
	vm.Set("obj", map[string]interface{}{"a": 1.0, "b": 2.0, "c": 3.0, "d": 4.0})
	r, _ := vm.Run(`
		let count = 0;
		for (const key in obj) {
			count++;
			if (count >= 2) break;
		}
		return count;
	`)
	if r.Number() != 2 { t.Errorf("expected 2, got %v", r.Number()) }
}

func TestForIn_Let(t *testing.T) {
	vm := New()
	vm.Set("obj", map[string]interface{}{"name": "test"})
	r, _ := vm.Run(`
		let result = "";
		for (let k in obj) {
			result = k;
		}
		return result;
	`)
	if r.String() != "name" { t.Errorf("expected 'name', got '%s'", r.String()) }
}

func TestForIn_BracelessBody(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const obj = {a: 1, b: 2, c: 3};
		const dst = {};
		for (var p in obj) if (p !== "b") dst[p] = obj[p];
		return Object.keys(dst).length;
	`)
	if r.Number() != 2 { t.Errorf("expected 2, got %v", r.Number()) }
}

// ══════════════════════════════════════════════════════════════
// Feature 3: this binding
// ══════════════════════════════════════════════════════════════

func TestThis_ObjectMethod(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const obj = {
			name: "test",
			greet() {
				return this.name;
			}
		};
		return obj.greet();
	`)
	if r.String() != "test" { t.Errorf("expected 'test', got '%s'", r.String()) }
}

func TestThis_NativeFunc(t *testing.T) {
	vm := New()
	obj := NewObj(map[string]*Value{
		"value": NewNum(42),
	})
	obj.object["getValue"] = NewNativeFunc(func(args []*Value) *Value {
		// Native functions don't auto-bind this, but can access the object
		return NewNum(42)
	})
	vm.SetValue("obj", obj)
	r, _ := vm.Eval("obj.getValue()")
	if r.Number() != 42 { t.Errorf("expected 42, got %v", r.Number()) }
}

func TestThis_SetFromScope(t *testing.T) {
	vm := New()
	vm.Set("this", map[string]interface{}{"x": 99.0})
	r, _ := vm.Eval("this.x")
	if r.Number() != 99 { t.Errorf("expected 99, got %v", r.Number()) }
}

func TestThis_MethodWithArgs(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const calc = {
			base: 10,
			add(n) {
				return this.base + n;
			}
		};
		return calc.add(5);
	`)
	if r.Number() != 15 { t.Errorf("expected 15, got %v", r.Number()) }
}

func TestThis_NestedObject(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const outer = {
			inner: {
				value: 100,
				get() { return this.value; }
			}
		};
		return outer.inner.get();
	`)
	if r.Number() != 100 { t.Errorf("expected 100, got %v", r.Number()) }
}

// ══════════════════════════════════════════════════════════════
// Feature 4: Getters/Setters
// ══════════════════════════════════════════════════════════════

func TestGetterSetter_ObjectLiteral(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const obj = {
			_name: "world",
			get name() { return "Hello " + this._name; },
			set name(v) { this._name = v; }
		};
		return obj.name;
	`)
	if r.String() != "Hello world" { t.Errorf("expected 'Hello world', got '%s'", r.String()) }
}

func TestGetterSetter_NativeGetter(t *testing.T) {
	vm := New()
	obj := NewObj(map[string]*Value{})
	counter := 0
	obj.DefineGetter("count", func(args []*Value) *Value {
		counter++
		return NewNum(float64(counter))
	})
	vm.SetValue("obj", obj)

	r1, _ := vm.Eval("obj.count")
	r2, _ := vm.Eval("obj.count")
	if r1.Number() != 1 { t.Errorf("first call expected 1, got %v", r1.Number()) }
	if r2.Number() != 2 { t.Errorf("second call expected 2, got %v", r2.Number()) }
}

func TestGetterSetter_NativeSetter(t *testing.T) {
	vm := New()
	obj := NewObj(map[string]*Value{})
	var captured string
	obj.DefineSetter("html", func(args []*Value) *Value {
		if len(args) > 0 {
			captured = args[0].String()
		}
		return Undefined
	})
	obj.DefineGetter("html", func(args []*Value) *Value {
		return NewStr(captured)
	})
	vm.SetValue("el", obj)

	vm.Run(`el.html = "<p>hello</p>";`)
	if captured != "<p>hello</p>" { t.Errorf("setter not called, captured: '%s'", captured) }

	r, _ := vm.Eval("el.html")
	if r.String() != "<p>hello</p>" { t.Errorf("getter expected '<p>hello</p>', got '%s'", r.String()) }
}

func TestGetterSetter_DefineProperty(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const obj = {};
		let stored = 0;
		Object.defineProperty(obj, "value", {
			get: () => stored * 2,
			set: (v) => { stored = v; }
		});
		obj.value = 21;
		return obj.value;
	`)
	if r.Number() != 42 { t.Errorf("expected 42, got %v", r.Number()) }
}

// ══════════════════════════════════════════════════════════════
// Feature 5: Prototype chain
// ══════════════════════════════════════════════════════════════

func TestPrototype_ObjectCreate(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const proto = { greet: () => "hello" };
		const obj = Object.create(proto);
		return obj.greet();
	`)
	if r.String() != "hello" { t.Errorf("expected 'hello', got '%s'", r.String()) }
}

func TestPrototype_PropertyLookup(t *testing.T) {
	vm := New()
	parent := NewObj(map[string]*Value{
		"inherited": NewStr("from parent"),
	})
	child := NewObj(map[string]*Value{
		"own": NewStr("from child"),
	})
	child.proto = parent
	vm.SetValue("obj", child)

	r1, _ := vm.Eval("obj.own")
	if r1.String() != "from child" { t.Errorf("expected 'from child', got '%s'", r1.String()) }

	r2, _ := vm.Eval("obj.inherited")
	if r2.String() != "from parent" { t.Errorf("expected 'from parent', got '%s'", r2.String()) }
}

func TestPrototype_OverrideProperty(t *testing.T) {
	vm := New()
	parent := NewObj(map[string]*Value{
		"name": NewStr("parent"),
	})
	child := NewObj(map[string]*Value{
		"name": NewStr("child"),
	})
	child.proto = parent
	vm.SetValue("obj", child)

	r, _ := vm.Eval("obj.name")
	if r.String() != "child" { t.Errorf("expected 'child', got '%s'", r.String()) }
}

func TestPrototype_MethodInheritance(t *testing.T) {
	vm := New()
	proto := NewObj(map[string]*Value{
		"type": NewStr("node"),
	})
	proto.object["getType"] = NewNativeFunc(func(args []*Value) *Value {
		return NewStr("node")
	})
	child := NewObj(map[string]*Value{
		"id": NewStr("div1"),
	})
	child.proto = proto
	vm.SetValue("el", child)

	r, _ := vm.Eval("el.getType()")
	if r.String() != "node" { t.Errorf("expected 'node', got '%s'", r.String()) }

	r2, _ := vm.Eval("el.id")
	if r2.String() != "div1" { t.Errorf("expected 'div1', got '%s'", r2.String()) }
}

func TestPrototype_GetPrototypeOf(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const proto = { x: 1 };
		const obj = Object.create(proto);
		const p = Object.getPrototypeOf(obj);
		return p.x;
	`)
	if r.Number() != 1 { t.Errorf("expected 1, got %v", r.Number()) }
}

func TestPrototype_ThreeLevels(t *testing.T) {
	vm := New()
	grandparent := NewObj(map[string]*Value{"level": NewStr("grandparent")})
	parent := NewObj(map[string]*Value{})
	parent.proto = grandparent
	child := NewObj(map[string]*Value{})
	child.proto = parent
	vm.SetValue("obj", child)

	r, _ := vm.Eval("obj.level")
	if r.String() != "grandparent" { t.Errorf("expected 'grandparent', got '%s'", r.String()) }
}

func TestPrototype_Instanceof(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const e = new Error("test");
		return e instanceof Error ? "yes" : "no";
	`)
	if r.String() != "yes" { t.Errorf("expected 'yes', got '%s'", r.String()) }
}

// ══════════════════════════════════════════════════════════════
// Feature 6: Promises + async/await
// ══════════════════════════════════════════════════════════════

func TestPromise_Resolve(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const p = Promise.resolve(42);
		return await p;
	`)
	if r.Number() != 42 { t.Errorf("expected 42, got %v", r.Number()) }
}

func TestPromise_Then(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let result = 0;
		const p = Promise.resolve(10);
		p.then((v) => { result = v * 2; });
		return result;
	`)
	if r.Number() != 20 { t.Errorf("expected 20, got %v", r.Number()) }
}

func TestPromise_ThenChain(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let result = 0;
		Promise.resolve(5)
			.then((v) => v * 2)
			.then((v) => { result = v + 1; });
		return result;
	`)
	if r.Number() != 11 { t.Errorf("expected 11, got %v", r.Number()) }
}

func TestPromise_Catch(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let result = "";
		Promise.reject("oops")
			.catch((e) => { result = "caught: " + e; });
		return result;
	`)
	if r.String() != "caught: oops" { t.Errorf("expected 'caught: oops', got '%s'", r.String()) }
}

func TestPromise_Constructor(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const p = new Promise((resolve, reject) => {
			resolve(99);
		});
		return await p;
	`)
	if r.Number() != 99 { t.Errorf("expected 99, got %v", r.Number()) }
}

func TestPromise_ConstructorReject(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let result = "";
		const p = new Promise((resolve, reject) => {
			reject("failed");
		});
		p.catch((e) => { result = e; });
		return result;
	`)
	if r.String() != "failed" { t.Errorf("expected 'failed', got '%s'", r.String()) }
}

func TestPromise_All(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const p = Promise.all([
			Promise.resolve(1),
			Promise.resolve(2),
			Promise.resolve(3)
		]);
		const arr = await p;
		return arr[0] + arr[1] + arr[2];
	`)
	if r.Number() != 6 { t.Errorf("expected 6, got %v", r.Number()) }
}

func TestPromise_AwaitNonPromise(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`return await 42;`)
	if r.Number() != 42 { t.Errorf("expected 42, got %v", r.Number()) }
}

func TestAsync_BasicFunction(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const fn = async () => 42;
		const p = fn();
		return await p;
	`)
	if r.Number() != 42 { t.Errorf("expected 42, got %v", r.Number()) }
}

func TestAsync_WithAwait(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const fetchData = async () => {
			const val = await Promise.resolve(100);
			return val + 1;
		};
		return await fetchData();
	`)
	if r.Number() != 101 { t.Errorf("expected 101, got %v", r.Number()) }
}

func TestPromise_MakePromiseAPI(t *testing.T) {
	// Test the Go API for creating promises
	pv, resolve, _ := MakePromise()
	resolve(NewNum(42))

	p := getPromise(pv)
	if p == nil { t.Fatal("expected promise") }
	if p.state != PromiseFulfilled { t.Error("expected fulfilled") }
	if p.value.Number() != 42 { t.Errorf("expected 42, got %v", p.value.Number()) }
}

func TestPromise_Finally(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let finallyCalled = false;
		Promise.resolve(42)
			.finally(() => { finallyCalled = true; });
		return finallyCalled ? "yes" : "no";
	`)
	if r.String() != "yes" { t.Errorf("expected 'yes', got '%s'", r.String()) }
}

// ══════════════════════════════════════════════════════════════
// Feature: Method shorthand in object literals
// ══════════════════════════════════════════════════════════════

func TestMethodShorthand(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const obj = {
			x: 10,
			double(n) { return n * 2; }
		};
		return obj.double(5);
	`)
	if r.Number() != 10 { t.Errorf("expected 10, got %v", r.Number()) }
}

func TestMethodShorthand_NoArgs(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const obj = {
			hello() { return "world"; }
		};
		return obj.hello();
	`)
	if r.String() != "world" { t.Errorf("expected 'world', got '%s'", r.String()) }
}
