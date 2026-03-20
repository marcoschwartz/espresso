package espresso

import (
	"testing"
)

func TestEval_Arithmetic(t *testing.T) {
	vm := New()
	r, _ := vm.Eval("2 + 3 * 4")
	if r.Number() != 14 { t.Errorf("expected 14, got %v", r.Number()) }
}

func TestEval_StringConcat(t *testing.T) {
	vm := New()
	vm.Set("name", "World")
	r, _ := vm.Eval(`"Hello " + name + "!"`)
	if r.String() != "Hello World!" { t.Errorf("expected 'Hello World!', got '%s'", r.String()) }
}

func TestEval_Ternary(t *testing.T) {
	vm := New()
	vm.Set("x", 10)
	r, _ := vm.Eval("x > 5 ? \"big\" : \"small\"")
	if r.String() != "big" { t.Errorf("expected 'big', got '%s'", r.String()) }
}

func TestEval_NullishCoalescing(t *testing.T) {
	vm := New()
	vm.Set("a", nil)
	vm.Set("b", 0)
	r1, _ := vm.Eval(`a ?? "fallback"`)
	if r1.String() != "fallback" { t.Error("null ?? should use fallback") }
	r2, _ := vm.Eval(`b ?? "fallback"`)
	if r2.Number() != 0 { t.Error("0 ?? should keep 0") }
}

func TestEval_ArrayMap(t *testing.T) {
	vm := New()
	vm.Set("nums", []interface{}{1.0, 2.0, 3.0})
	r, _ := vm.Eval("nums.map(n => n * 2)")
	if r.Len() != 3 { t.Errorf("expected 3 elements, got %d", r.Len()) }
	if r.Array()[0].Number() != 2 { t.Error("first should be 2") }
	if r.Array()[2].Number() != 6 { t.Error("last should be 6") }
}

func TestEval_ArrayFilter(t *testing.T) {
	vm := New()
	vm.Set("nums", []interface{}{1.0, 2.0, 3.0, 4.0, 5.0})
	r, _ := vm.Eval("nums.filter(n => n > 3)")
	if r.Len() != 2 { t.Errorf("expected 2, got %d", r.Len()) }
}

func TestEval_ArrayFind(t *testing.T) {
	vm := New()
	vm.Set("items", []interface{}{
		map[string]interface{}{"id": 1.0, "name": "Alice"},
		map[string]interface{}{"id": 2.0, "name": "Bob"},
	})
	r, _ := vm.Eval("items.find(i => i.id === 2)")
	if r.Get("name").String() != "Bob" { t.Error("should find Bob") }
}

func TestEval_ArrayReduce(t *testing.T) {
	vm := New()
	vm.Set("nums", []interface{}{1.0, 2.0, 3.0, 4.0, 5.0})
	r, _ := vm.Eval("nums.reduce((a, b) => a + b, 0)")
	if r.Number() != 15 { t.Errorf("expected 15, got %v", r.Number()) }
}

func TestEval_StringMethods(t *testing.T) {
	vm := New()
	vm.Set("s", "Hello World")

	tests := []struct{ expr, want string }{
		{`s.toLowerCase()`, "hello world"},
		{`s.toUpperCase()`, "HELLO WORLD"},
		{`s.startsWith("Hello") ? "yes" : "no"`, "yes"},
		{`s.endsWith("World") ? "yes" : "no"`, "yes"},
		{`s.includes("lo") ? "yes" : "no"`, "yes"},
		{`s.replace("World", "Go")`, "Hello Go"},
		{`s.slice(6, 11)`, "World"},
		{`s.indexOf("World")`, "6"},
		{`s.charAt(0)`, "H"},
		{`s.substring(0, 5)`, "Hello"},
		{`s.trim()`, "Hello World"},
	}
	for _, tt := range tests {
		r, _ := vm.Eval(tt.expr)
		if r.String() != tt.want {
			t.Errorf("%s: expected '%s', got '%s'", tt.expr, tt.want, r.String())
		}
	}
}

func TestEval_ObjectMethods(t *testing.T) {
	vm := New()
	vm.Set("obj", map[string]interface{}{"a": 1.0, "b": 2.0, "c": 3.0})

	r1, _ := vm.Eval("Object.keys(obj).length")
	if r1.Number() != 3 { t.Error("Object.keys should return 3 keys") }

	r2, _ := vm.Eval("Object.values(obj).length")
	if r2.Number() != 3 { t.Error("Object.values should return 3 values") }

	r3, _ := vm.Eval("Object.entries(obj).length")
	if r3.Number() != 3 { t.Error("Object.entries should return 3 entries") }
}

func TestRun_ForLoop(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let sum = 0;
		for (let i = 0; i < 5; i++) {
			sum += i;
		}
		return sum;
	`)
	if r.Number() != 10 { t.Errorf("expected 10, got %v", r.Number()) }
}

func TestRun_ForOfLoop(t *testing.T) {
	vm := New()
	vm.Set("items", []interface{}{"a", "b", "c"})
	r, _ := vm.Run(`
		let result = "";
		for (const item of items) {
			result += item;
		}
		return result;
	`)
	if r.String() != "abc" { t.Errorf("expected 'abc', got '%s'", r.String()) }
}

func TestRun_WhileLoop(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let count = 0;
		while (count < 10) {
			count++;
		}
		return count;
	`)
	if r.Number() != 10 { t.Errorf("expected 10, got %v", r.Number()) }
}

func TestRun_IfElse(t *testing.T) {
	vm := New()
	vm.Set("x", 42)
	r, _ := vm.Run(`
		if (x > 100) {
			return "big";
		} else if (x > 20) {
			return "medium";
		} else {
			return "small";
		}
	`)
	if r.String() != "medium" { t.Errorf("expected 'medium', got '%s'", r.String()) }
}

func TestRun_TryCatch(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let result = "initial";
		try {
			result = "from try";
		} catch (e) {
			result = "from catch";
		}
		return result;
	`)
	if r.String() != "from try" { t.Error("expected 'from try'") }
}

func TestRun_TemplateLiteral(t *testing.T) {
	vm := New()
	vm.Set("name", "World")
	vm.Set("x", 3)
	r, _ := vm.Run("return `Hello ${name}, ${x * 2}!`;")
	if r.String() != "Hello World, 6!" { t.Errorf("expected 'Hello World, 6!', got '%s'", r.String()) }
}

func TestEval_ArrowFunction(t *testing.T) {
	vm := New()
	vm.Set("items", []interface{}{1.0, 2.0, 3.0})
	r, _ := vm.Eval("items.map(x => x * x).join(\",\")")
	if r.String() != "1,4,9" { t.Errorf("expected '1,4,9', got '%s'", r.String()) }
}

func TestEval_OptionalChaining(t *testing.T) {
	vm := New()
	vm.Set("obj", map[string]interface{}{"a": map[string]interface{}{"b": "deep"}})
	r1, _ := vm.Eval("obj?.a?.b")
	if r1.String() != "deep" { t.Error("should access deep value") }
	r2, _ := vm.Eval("obj?.x?.y")
	if !r2.IsUndefined() { t.Error("should be undefined for missing path") }
}

func TestEval_Typeof(t *testing.T) {
	vm := New()
	vm.Set("n", 42)
	vm.Set("s", "hello")
	r1, _ := vm.Eval(`typeof n`)
	if r1.String() != "number" { t.Error("typeof number") }
	r2, _ := vm.Eval(`typeof s`)
	if r2.String() != "string" { t.Error("typeof string") }
}

func TestEval_JSONParse(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`JSON.parse('{"name":"test","count":5}')`)
	if r.Get("name").String() != "test" { t.Error("should parse name") }
	if r.Get("count").Number() != 5 { t.Error("should parse count") }
}

func TestEval_JSONStringify(t *testing.T) {
	vm := New()
	vm.Set("obj", map[string]interface{}{"x": 1.0})
	r, _ := vm.Eval("JSON.stringify(obj)")
	if r.String() != `{"x":1}` { t.Errorf("expected '{\"x\":1}', got '%s'", r.String()) }
}

func TestRun_Reassignment(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let x = 1;
		x = 2;
		x += 3;
		x++;
		return x;
	`)
	if r.Number() != 6 { t.Errorf("expected 6, got %v", r.Number()) }
}

func TestEval_ArrayConcat(t *testing.T) {
	vm := New()
	r, _ := vm.Eval("[1,2].concat([3,4]).length")
	// Can't eval array literals directly, use scope
	vm.Set("a", []interface{}{1.0, 2.0})
	vm.Set("b", []interface{}{3.0, 4.0})
	r, _ = vm.Eval("a.concat(b).length")
	if r.Number() != 4 { t.Errorf("expected 4, got %v", r.Number()) }
}

func TestEval_ArraySomeEvery(t *testing.T) {
	vm := New()
	vm.Set("nums", []interface{}{2.0, 4.0, 6.0})
	r1, _ := vm.Eval("nums.every(n => n % 2 === 0)")
	if !r1.Truthy() { t.Error("all even, every should be true") }
	r2, _ := vm.Eval("nums.some(n => n > 5)")
	if !r2.Truthy() { t.Error("6 > 5, some should be true") }
}

func TestEval_ParseIntFloat(t *testing.T) {
	vm := New()
	r1, _ := vm.Eval(`parseInt("42")`)
	if r1.Number() != 42 { t.Error("parseInt") }
	r2, _ := vm.Eval(`parseFloat("3.14")`)
	if r2.Number() != 3.14 { t.Error("parseFloat") }
}

func TestEval_MathMethods(t *testing.T) {
	vm := New()
	r1, _ := vm.Eval("Math.floor(3.7)")
	if r1.Number() != 3 { t.Error("Math.floor") }
	r2, _ := vm.Eval("Math.ceil(3.2)")
	if r2.Number() != 4 { t.Error("Math.ceil") }
	r3, _ := vm.Eval("Math.abs(-5)")
	if r3.Number() != 5 { t.Error("Math.abs") }
	r4, _ := vm.Eval("Math.max(3, 7)")
	if r4.Number() != 7 { t.Error("Math.max") }
}

func TestSet_And_Get(t *testing.T) {
	vm := New()
	vm.Set("count", 42)
	vm.Set("name", "test")
	vm.Set("active", true)
	vm.Set("items", []interface{}{1.0, 2.0})
	vm.Set("config", map[string]interface{}{"debug": true})

	if vm.Get("count").Number() != 42 { t.Error("count") }
	if vm.Get("name").String() != "test" { t.Error("name") }
	if !vm.Get("active").Truthy() { t.Error("active") }
	if vm.Get("items").Len() != 2 { t.Error("items length") }
	if !vm.Get("config").Get("debug").Truthy() { t.Error("config.debug") }
	if !vm.Get("missing").IsUndefined() { t.Error("missing should be undefined") }
}

func TestRun_ConsoleLog(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		console.log("test");
		console.warn("warning");
		return "ok";
	`)
	if r.String() != "ok" { t.Error("console should be no-op") }
}

func TestEval_Destructuring(t *testing.T) {
	vm := New()
	vm.Set("pair", []interface{}{10.0, 20.0})
	vm.Run("const [a, b] = pair;")
	if vm.Get("a").Number() != 10 { t.Error("destructured a") }
	if vm.Get("b").Number() != 20 { t.Error("destructured b") }
}

func TestEval_SpreadObject(t *testing.T) {
	vm := New()
	vm.Set("base", map[string]interface{}{"x": 1.0})
	r, _ := vm.Eval("{...base, y: 2}")
	if r.Get("x").Number() != 1 { t.Error("spread x") }
	if r.Get("y").Number() != 2 { t.Error("new y") }
}
