package espresso

import (
	"encoding/json"
	"testing"
)

func BenchmarkEvalSimpleExpr(b *testing.B) {
	vm := New()
	for i := 0; i < b.N; i++ {
		vm.Eval(`1 + 2 * 3`)
	}
}

func BenchmarkEvalStringConcat(b *testing.B) {
	vm := New()
	for i := 0; i < b.N; i++ {
		vm.Eval(`"hello" + " " + "world"`)
	}
}

func BenchmarkEvalTemplateLiteral(b *testing.B) {
	vm := New()
	vm.Set("name", "World")
	for i := 0; i < b.N; i++ {
		vm.Eval("`Hello ${name}!`")
	}
}

func BenchmarkEvalArrayMap(b *testing.B) {
	vm := New()
	for i := 0; i < b.N; i++ {
		vm.Eval(`[1,2,3,4,5].map((x) => x * 2)`)
	}
}

func BenchmarkEvalArrayFilter(b *testing.B) {
	vm := New()
	for i := 0; i < b.N; i++ {
		vm.Eval(`[1,2,3,4,5,6,7,8,9,10].filter((x) => x > 5)`)
	}
}

func BenchmarkEvalArrayFind(b *testing.B) {
	vm := New()
	for i := 0; i < b.N; i++ {
		vm.Eval(`[{id:1,name:"a"},{id:2,name:"b"},{id:3,name:"c"}].find((x) => x.id === 2)`)
	}
}

func BenchmarkEvalObjectAccess(b *testing.B) {
	vm := New()
	vm.Set("data", map[string]interface{}{
		"user": map[string]interface{}{
			"name": "Alice",
			"age":  30,
		},
	})
	for i := 0; i < b.N; i++ {
		vm.Eval(`data.user.name`)
	}
}

func BenchmarkEvalTernary(b *testing.B) {
	vm := New()
	vm.Set("x", 42)
	for i := 0; i < b.N; i++ {
		vm.Eval(`x > 10 ? "big" : "small"`)
	}
}

func BenchmarkEvalTypeof(b *testing.B) {
	vm := New()
	vm.Set("val", map[string]interface{}{"value": 100})
	for i := 0; i < b.N; i++ {
		vm.Eval(`typeof val === "object" ? val.value : val`)
	}
}

func BenchmarkEvalMultiStatement(b *testing.B) {
	for i := 0; i < b.N; i++ {
		vm := New()
		vm.Eval(`const items = [{a:1},{a:2},{a:3}]; items.map((x) => x.a * 2)`)
	}
}

func BenchmarkEvalIntlNumberFormat(b *testing.B) {
	vm := New()
	for i := 0; i < b.N; i++ {
		vm.Eval(`new Intl.NumberFormat('en-US', {style: 'currency', currency: 'USD', minimumFractionDigits: 0, maximumFractionDigits: 0}).format(1900)`)
	}
}

func BenchmarkEvalToLocaleString(b *testing.B) {
	vm := New()
	for i := 0; i < b.N; i++ {
		vm.Eval(`(1234567).toLocaleString()`)
	}
}

func BenchmarkEvalFunctionCall(b *testing.B) {
	for i := 0; i < b.N; i++ {
		vm := New()
		vm.Eval(`function add(a, b) { return a + b; } add(10, 20)`)
	}
}

func BenchmarkEvalComplexPricing(b *testing.B) {
	// Simulates the pricing page feature rendering
	for i := 0; i < b.N; i++ {
		vm := New()
		vm.Eval(`function formatPrice(amount, currency) { return new Intl.NumberFormat('en-US', {style: 'currency', currency: currency.toUpperCase(), minimumFractionDigits: 0, maximumFractionDigits: 0}).format(amount / 100); } formatPrice(1900, "usd")`)
	}
}

func BenchmarkJsonToValue(b *testing.B) {
	data := json.RawMessage(`{"products":[{"name":"Basic","price":0,"features":["1 hour","1 session"]},{"name":"Standard","price":1900,"features":["100 hours","10 sessions"]},{"name":"Advanced","price":7900,"features":["500 hours","50 sessions"]}]}`)
	for i := 0; i < b.N; i++ {
		JsonToValue(data)
	}
}

func BenchmarkTokenize(b *testing.B) {
	code := `const items = [{id:1,name:"hello"},{id:2,name:"world"}]; items.filter((x) => x.id > 0).map((x) => x.name + "!")`
	for i := 0; i < b.N; i++ {
		tokenize(code)
	}
}

func BenchmarkTokenizeCached(b *testing.B) {
	code := `const items = [{id:1,name:"hello"},{id:2,name:"world"}]; items.filter((x) => x.id > 0).map((x) => x.name + "!")`
	for i := 0; i < b.N; i++ {
		tokenizeCached(code)
	}
}

func BenchmarkBytecodeLogicalOps(b *testing.B) {
	vm := New()
	vm.Run(`function check(a, b) { return a > 0 && b > 0 || a === b; }`)
	for i := 0; i < b.N; i++ {
		vm.Eval(`check(5, 10)`)
	}
}

func BenchmarkBytecodeWhileLoop(b *testing.B) {
	vm := New()
	vm.Run(`function sumTo(n) { var s = 0; var i = 0; while (i < n) { s = s + i; i++; } return s; }`)
	for i := 0; i < b.N; i++ {
		vm.Eval(`sumTo(100)`)
	}
}

func BenchmarkBytecodeForLoop(b *testing.B) {
	vm := New()
	vm.Run(`function fact(n) { var r = 1; for (var i = 2; i <= n; i++) { r = r * i; } return r; }`)
	for i := 0; i < b.N; i++ {
		vm.Eval(`fact(20)`)
	}
}

func BenchmarkBytecodePropAccess(b *testing.B) {
	vm := New()
	vm.Set("obj", map[string]interface{}{"x": 42, "nested": map[string]interface{}{"y": 100}})
	vm.Run(`function getY(o) { return o.nested.y; }`)
	for i := 0; i < b.N; i++ {
		vm.Eval(`getY(obj)`)
	}
}
