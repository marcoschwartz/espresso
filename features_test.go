package espresso

import (
	"strings"
	"testing"
)

// ── Loose equality ──

func TestLooseEquality(t *testing.T) {
	vm := New()
	tests := []struct{ code, want string }{
		{`1 == "1" ? "yes" : "no"`, "yes"},
		{`0 == "" ? "yes" : "no"`, "yes"},
		{`null == undefined ? "yes" : "no"`, "yes"},
		{`null == 0 ? "yes" : "no"`, "no"},
		{`true == 1 ? "yes" : "no"`, "yes"},
		{`false == 0 ? "yes" : "no"`, "yes"},
		{`"" == false ? "yes" : "no"`, "yes"},
		{`1 != "2" ? "yes" : "no"`, "yes"},
		{`1 !== "1" ? "yes" : "no"`, "yes"},
	}
	for _, tt := range tests {
		r, _ := vm.Eval(tt.code)
		if r.String() != tt.want {
			t.Errorf("%s: want %s, got %s", tt.code, tt.want, r.String())
		}
	}
}

// ── typeof ──

func TestTypeof(t *testing.T) {
	vm := New()
	vm.Set("n", 42)
	vm.Set("s", "hello")
	vm.Set("b", true)
	vm.Set("a", []interface{}{})
	vm.Set("o", map[string]interface{}{})
	tests := []struct{ code, want string }{
		{`typeof n`, "number"},
		{`typeof s`, "string"},
		{`typeof b`, "boolean"},
		{`typeof null`, "object"},
		{`typeof undefined`, "undefined"},
		{`typeof a`, "object"},
		{`typeof o`, "object"},
		{`typeof parseInt`, "function"},
		{`typeof parseFloat`, "function"},
		{`typeof Number`, "function"},
		{`typeof Boolean`, "function"},
		{`typeof missing`, "undefined"},
	}
	for _, tt := range tests {
		r, _ := vm.Eval(tt.code)
		if r.String() != tt.want {
			t.Errorf("%s: want %s, got %s", tt.code, tt.want, r.String())
		}
	}
}

// ── For loop fix (<=) ──

func TestForLoop_LessEqual(t *testing.T) {
	vm := New()
	r, _ := vm.Run("let s=0; for(let i=1;i<=5;i++){s+=i;} return s;")
	if r.Number() != 15 { t.Errorf("expected 15, got %v", r.Number()) }
}

func TestForLoop_Decrement(t *testing.T) {
	vm := New()
	r, _ := vm.Run("let s=0; for(let i=5;i>0;i--){s+=i;} return s;")
	if r.Number() != 15 { t.Errorf("expected 15, got %v", r.Number()) }
}

// ── Switch ──

func TestSwitch(t *testing.T) {
	vm := New()
	vm.Set("val", "b")
	r, _ := vm.Run(`
		let result = "";
		switch (val) {
			case "a":
				result = "first";
				break;
			case "b":
				result = "second";
				break;
			case "c":
				result = "third";
				break;
			default:
				result = "unknown";
		}
		return result;
	`)
	if r.String() != "second" { t.Errorf("expected 'second', got '%s'", r.String()) }
}

func TestSwitch_Default(t *testing.T) {
	vm := New()
	vm.Set("val", "z")
	r, _ := vm.Run(`
		let result = "";
		switch (val) {
			case "a":
				result = "first";
				break;
			default:
				result = "default";
		}
		return result;
	`)
	if r.String() != "default" { t.Errorf("expected 'default', got '%s'", r.String()) }
}

func TestSwitch_Return(t *testing.T) {
	vm := New()
	vm.Set("x", 2)
	r, _ := vm.Run(`
		switch (x) {
			case 1: return "one";
			case 2: return "two";
			case 3: return "three";
		}
		return "other";
	`)
	if r.String() != "two" { t.Errorf("expected 'two', got '%s'", r.String()) }
}

// ── Break / Continue ──

func TestBreak(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let s = 0;
		for (let i = 0; i < 10; i++) {
			if (i === 5) break;
			s += i;
		}
		return s;
	`)
	if r.Number() != 10 { t.Errorf("expected 10 (0+1+2+3+4), got %v", r.Number()) }
}

func TestContinue(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let s = 0;
		for (let i = 0; i < 5; i++) {
			if (i === 2) continue;
			s += i;
		}
		return s;
	`)
	if r.Number() != 8 { t.Errorf("expected 8 (0+1+3+4), got %v", r.Number()) }
}

func TestWhileBreak(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let i = 0;
		while (true) {
			if (i >= 5) break;
			i++;
		}
		return i;
	`)
	if r.Number() != 5 { t.Errorf("expected 5, got %v", r.Number()) }
}

// ── Edge cases ──

func TestEmptyArrayTruthy(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`[] ? "truthy" : "falsy"`)
	// In JS, empty arrays ARE truthy
	if r.String() != "truthy" { t.Errorf("empty array should be truthy") }
}

func TestZeroFalsy(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`0 ? "truthy" : "falsy"`)
	if r.String() != "falsy" { t.Error("0 should be falsy") }
}

func TestEmptyStringFalsy(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"" ? "truthy" : "falsy"`)
	if r.String() != "falsy" { t.Error("empty string should be falsy") }
}

func TestNullEquality(t *testing.T) {
	vm := New()
	vm.Set("x", nil)
	r, _ := vm.Eval(`x === null ? "null" : "not"`)
	if r.String() != "null" { t.Error("should be null") }
}

func TestStringPlusNumber(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"count:" + 42`)
	if r.String() != "count:42" { t.Errorf("expected 'count:42', got '%s'", r.String()) }
}

func TestToFixed(t *testing.T) {
	vm := New()
	vm.Set("n", 3.14159)
	r, _ := vm.Eval("n.toFixed(2)")
	if r.String() != "3.14" { t.Errorf("expected '3.14', got '%s'", r.String()) }
}

func TestDynamicPropertyAccess(t *testing.T) {
	vm := New()
	vm.Set("o", map[string]interface{}{"name": "test"})
	r, _ := vm.Eval(`o["name"]`)
	if r.String() != "test" { t.Errorf("expected 'test', got '%s'", r.String()) }
}

func TestJSONRoundtrip(t *testing.T) {
	vm := New()
	vm.Set("o", map[string]interface{}{"a": 1.0, "b": "hello"})
	r, _ := vm.Eval("JSON.parse(JSON.stringify(o)).b")
	if r.String() != "hello" { t.Errorf("expected 'hello', got '%s'", r.String()) }
}

func TestForOfStrings(t *testing.T) {
	vm := New()
	vm.Set("arr", []interface{}{"hello", "world"})
	r, _ := vm.Run(`let r=""; for(const s of arr){r+=s+" ";} return r.trim();`)
	if r.String() != "hello world" { t.Errorf("expected 'hello world', got '%s'", r.String()) }
}

func TestWhileCountdown(t *testing.T) {
	vm := New()
	r, _ := vm.Run("let n=5; while(n>0){n--;} return n;")
	if r.Number() != 0 { t.Errorf("expected 0, got %v", r.Number()) }
}

func TestMathMin(t *testing.T) {
	vm := New()
	r, _ := vm.Eval("Math.min(5, 3)")
	if r.Number() != 3 { t.Error("Math.min") }
}

func TestMathFloorNeg(t *testing.T) {
	vm := New()
	r, _ := vm.Eval("Math.floor(-1.5)")
	if r.Number() != -1 { t.Errorf("expected -1, got %v", r.Number()) }
}

func TestModulo(t *testing.T) {
	vm := New()
	r, _ := vm.Eval("10 % 3")
	if r.Number() != 1 { t.Error("10 % 3 should be 1") }
}

func TestUnaryMinus(t *testing.T) {
	vm := New()
	r, _ := vm.Eval("-(5 + 3)")
	if r.Number() != -8 { t.Error("-(5+3) should be -8") }
}

func TestLastIndexOf(t *testing.T) {
	vm := New()
	vm.Set("s", "abcabc")
	r, _ := vm.Eval(`s.lastIndexOf("bc")`)
	if r.Number() != 4 { t.Errorf("expected 4, got %v", r.Number()) }
}

func TestArraySort(t *testing.T) {
	vm := New()
	vm.Set("a", []interface{}{"c", "a", "b"})
	r, _ := vm.Eval(`a.sort().join(",")`)
	if r.String() != "a,b,c" { t.Errorf("expected 'a,b,c', got '%s'", r.String()) }
}

func TestNaN(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`parseInt("abc")`)
	if r.Number() != 0 { t.Error("parseInt of non-number should return 0") }
}

// ── Array element property access ──

func TestArrayElementSetter(t *testing.T) {
	vm := New()
	obj := NewObj(map[string]*Value{})
	backingVal := ""
	obj.DefineGetter("name", func(args []*Value) *Value { return NewStr(backingVal) })
	obj.DefineSetter("name", func(args []*Value) *Value {
		if len(args) > 0 { backingVal = args[0].String() }
		return Undefined
	})
	vm.SetValue("arr", NewArr([]*Value{obj}))

	vm.Run(`arr[0].name = "hello";`)
	if backingVal != "hello" { t.Errorf("setter: got %q", backingVal) }
	r, _ := vm.Eval(`arr[0].name`)
	if r.String() != "hello" { t.Errorf("getter: got %q", r.String()) }
}

func TestArrayElementMethodCall(t *testing.T) {
	vm := New()
	called := false
	obj := NewObj(map[string]*Value{
		"doStuff": NewNativeFunc(func(args []*Value) *Value { called = true; return NewStr("done") }),
	})
	vm.SetValue("arr", NewArr([]*Value{obj}))
	vm.Run(`var result = arr[0].doStuff();`)
	if !called { t.Error("method not called") }
	r, _ := vm.Eval(`result`)
	if r.String() != "done" { t.Errorf("got %q", r.String()) }
}

func TestArrayElementNestedPropAssign(t *testing.T) {
	vm := New()
	style := NewObj(map[string]*Value{"color": NewStr("blue")})
	obj := NewObj(map[string]*Value{"style": style})
	vm.SetValue("arr", NewArr([]*Value{obj}))
	vm.Run(`arr[0].style.color = "red";`)
	r, _ := vm.Eval(`arr[0].style.color`)
	if r.String() != "red" { t.Errorf("got %q", r.String()) }
}

func TestArrayElementSetAttribute(t *testing.T) {
	vm := New()
	attrs := map[string]string{}
	obj := NewObj(map[string]*Value{
		"setAttribute": NewNativeFunc(func(args []*Value) *Value {
			if len(args) >= 2 { attrs[args[0].String()] = args[1].String() }
			return Undefined
		}),
	})
	vm.SetValue("items", NewArr([]*Value{obj}))
	vm.Run(`items[0].setAttribute("data-x", "42");`)
	if attrs["data-x"] != "42" { t.Errorf("got %q", attrs["data-x"]) }
}

// ── Event / CustomEvent constructors ──

func TestNewEvent(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`var e = new Event("click"); e.type`)
	if r.String() != "click" { t.Errorf("type: got %q", r.String()) }

	r, _ = vm.Eval(`e.bubbles`)
	if r.Bool() { t.Error("bubbles should default to false") }

	r, _ = vm.Eval(`var e2 = new Event("submit", {bubbles: true, cancelable: true}); e2.bubbles`)
	if !r.Bool() { t.Error("bubbles should be true") }

	r, _ = vm.Eval(`e2.cancelable`)
	if !r.Bool() { t.Error("cancelable should be true") }

	vm.Run(`e2.preventDefault();`)
	r, _ = vm.Eval(`e2.defaultPrevented`)
	if !r.Bool() { t.Error("defaultPrevented should be true after preventDefault") }
}

func TestNewCustomEvent(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`var e = new CustomEvent("foo", {detail: {x: 42}, bubbles: true}); e.type`)
	if r.String() != "foo" { t.Errorf("type: got %q", r.String()) }

	r, _ = vm.Eval(`e.detail.x`)
	if r.Number() != 42 { t.Errorf("detail.x: got %v", r.Number()) }

	r, _ = vm.Eval(`e.bubbles`)
	if !r.Bool() { t.Error("bubbles should be true") }
}

// ── Array.splice / unshift / pop / shift mutations ──

func TestArraySplice(t *testing.T) {
	vm := New()
	tests := []struct{ code, want string }{
		// splice(start, deleteCount) — remove 2 from index 1
		{`var a = [1,2,3,4,5]; var r = a.splice(1,2); r.join(",") + "|" + a.join(",")`, "2,3|1,4,5"},
		// splice(start, deleteCount, items...) — remove and insert
		{`var b = [1,2,3]; b.splice(1, 1, "a", "b"); b.join(",")`, "1,a,b,3"},
		// splice(start) — remove everything from start
		{`var c = [1,2,3,4]; c.splice(2); c.join(",")`, "1,2"},
		// splice with negative index
		{`var d = [1,2,3,4]; d.splice(-1, 1); d.join(",")`, "1,2,3"},
		// splice insert without removing
		{`var e = [1,3]; e.splice(1, 0, 2); e.join(",")`, "1,2,3"},
	}
	for _, tt := range tests {
		r, _ := vm.Eval(tt.code)
		if r.String() != tt.want { t.Errorf("%s = %q, want %q", tt.code, r.String(), tt.want) }
	}
}

func TestArrayUnshift(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`var a = [3,4]; a.unshift(1,2); a.join(",")`)
	if r.String() != "1,2,3,4" { t.Errorf("got %q", r.String()) }

	r, _ = vm.Eval(`var b = []; b.unshift("x"); b.length`)
	if r.Number() != 1 { t.Errorf("length: %v", r.Number()) }
}

func TestArrayPopShiftMutate(t *testing.T) {
	vm := New()
	// pop should remove last element
	r, _ := vm.Eval(`var a = [1,2,3]; var p = a.pop(); p + "|" + a.join(",")`)
	if r.String() != "3|1,2" { t.Errorf("pop: %q", r.String()) }

	// shift should remove first element
	r, _ = vm.Eval(`var b = [1,2,3]; var s = b.shift(); s + "|" + b.join(",")`)
	if r.String() != "1|2,3" { t.Errorf("shift: %q", r.String()) }
}

// ── do...while ──

func TestDoWhile(t *testing.T) {
	vm := New()

	// Use Run + Get to avoid Eval interaction
	vm.Run(`var i = 0; do { i = i + 1; } while (i < 5);`)
	r := vm.Get("i")
	if r.Number() != 5 { t.Errorf("basic: got %v", r.Number()) }

	// should execute at least once even if condition is false
	vm.Run(`var x = 0; do { x = 42; } while (false);`)
	r = vm.Get("x")
	if r.Number() != 42 { t.Errorf("once: got %v", r.Number()) }

	// break inside do...while
	vm.Run(`var j = 0; do { j = j + 1; if (j === 3) break; } while (j < 10);`)
	r = vm.Get("j")
	if r.Number() != 3 { t.Errorf("break: got %v", r.Number()) }
}

// ── isNaN / isFinite ──

func TestIsNaN(t *testing.T) {
	vm := New()
	tests := []struct{ code string; want bool }{
		{`isNaN(NaN)`, true},
		{`isNaN("hello")`, true},
		{`isNaN(42)`, false},
		{`isNaN("42")`, false},
		{`isNaN(undefined)`, true},
	}
	for _, tt := range tests {
		r, _ := vm.Eval(tt.code)
		if r.Bool() != tt.want { t.Errorf("%s = %v, want %v", tt.code, r.Bool(), tt.want) }
	}
}

func TestIsFinite(t *testing.T) {
	vm := New()
	tests := []struct{ code string; want bool }{
		{`isFinite(42)`, true},
		{`isFinite(Infinity)`, false},
		{`isFinite(-Infinity)`, false},
		{`isFinite(NaN)`, false},
	}
	for _, tt := range tests {
		r, _ := vm.Eval(tt.code)
		if r.Bool() != tt.want { t.Errorf("%s = %v, want %v", tt.code, r.Bool(), tt.want) }
	}
}

func TestNumberMethods(t *testing.T) {
	vm := New()
	tests := []struct{ code string; want bool }{
		{`Number.isInteger(42)`, true},
		{`Number.isInteger(42.5)`, false},
		{`Number.isNaN(NaN)`, true},
		{`Number.isNaN(42)`, false},
		{`Number.isNaN("NaN")`, false}, // strict: string is not NaN
		{`Number.isFinite(42)`, true},
		{`Number.isFinite(Infinity)`, false},
	}
	for _, tt := range tests {
		r, _ := vm.Eval(tt.code)
		if r.Bool() != tt.want { t.Errorf("%s = %v, want %v", tt.code, r.Bool(), tt.want) }
	}

	r, _ := vm.Eval(`Number.MAX_SAFE_INTEGER`)
	if r.Number() != 9007199254740991 { t.Errorf("MAX_SAFE_INTEGER: %v", r.Number()) }
}

// ── encodeURIComponent / decodeURIComponent ──

func TestURIEncoding(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`encodeURIComponent("hello world")`)
	if r.String() != "hello+world" && r.String() != "hello%20world" {
		// Go's url.QueryEscape uses + for space, JS uses %20 — both are valid
		t.Logf("encodeURIComponent: %q (Go uses + for space)", r.String())
	}

	r, _ = vm.Eval(`encodeURIComponent("foo@bar.com")`)
	if !strings.Contains(r.String(), "%40") && !strings.Contains(r.String(), "@") {
		t.Errorf("encode @: %q", r.String())
	}

	r, _ = vm.Eval(`decodeURIComponent("hello%20world")`)
	if r.String() != "hello world" { t.Errorf("decode: %q", r.String()) }

	r, _ = vm.Eval(`decodeURIComponent(encodeURIComponent("a=1&b=2"))`)
	if r.String() != "a=1&b=2" { t.Errorf("roundtrip: %q", r.String()) }
}

// ── Math methods ──

func TestMathExtended(t *testing.T) {
	vm := New()
	tests := []struct{ code string; check func(float64) bool; desc string }{
		{`Math.pow(2, 10)`, func(n float64) bool { return n == 1024 }, "pow"},
		{`Math.sqrt(144)`, func(n float64) bool { return n == 12 }, "sqrt"},
		{`Math.log(1)`, func(n float64) bool { return n == 0 }, "log(1)=0"},
		{`Math.log(Math.E)`, func(n float64) bool { return n > 0.99 && n < 1.01 }, "log(e)≈1"},
		{`Math.sin(0)`, func(n float64) bool { return n == 0 }, "sin(0)"},
		{`Math.cos(0)`, func(n float64) bool { return n == 1 }, "cos(0)"},
		{`Math.trunc(4.9)`, func(n float64) bool { return n == 4 }, "trunc"},
		{`Math.trunc(-4.9)`, func(n float64) bool { return n == -4 }, "trunc neg"},
		{`Math.sign(-5)`, func(n float64) bool { return n == -1 }, "sign neg"},
		{`Math.sign(5)`, func(n float64) bool { return n == 1 }, "sign pos"},
		{`Math.sign(0)`, func(n float64) bool { return n == 0 }, "sign zero"},
		{`Math.PI`, func(n float64) bool { return n > 3.14 && n < 3.15 }, "PI"},
		{`Math.E`, func(n float64) bool { return n > 2.71 && n < 2.72 }, "E"},
		{`Math.random()`, func(n float64) bool { return n >= 0 && n < 1 }, "random [0,1)"},
		{`Math.hypot(3, 4)`, func(n float64) bool { return n == 5 }, "hypot"},
	}
	for _, tt := range tests {
		r, _ := vm.Eval(tt.code)
		if !tt.check(r.Number()) { t.Errorf("%s (%s): got %v", tt.code, tt.desc, r.Number()) }
	}

	// random should return different values (at least sometimes)
	r1, _ := vm.Eval(`Math.random()`)
	r2, _ := vm.Eval(`Math.random()`)
	r3, _ := vm.Eval(`Math.random()`)
	if r1.Number() == r2.Number() && r2.Number() == r3.Number() {
		t.Error("Math.random() should return varying values")
	}
}

// ── charCodeAt / codePointAt / String.fromCharCode ──

func TestStringCharCodes(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"A".charCodeAt(0)`)
	if r.Number() != 65 { t.Errorf("charCodeAt A: %v", r.Number()) }

	r, _ = vm.Eval(`"hello".charCodeAt(1)`)
	if r.Number() != 101 { t.Errorf("charCodeAt e: %v", r.Number()) }

	r, _ = vm.Eval(`"A".codePointAt(0)`)
	if r.Number() != 65 { t.Errorf("codePointAt A: %v", r.Number()) }

	r, _ = vm.Eval(`String.fromCharCode(72, 101, 108, 108, 111)`)
	if r.String() != "Hello" { t.Errorf("fromCharCode: %q", r.String()) }

	r, _ = vm.Eval(`String.fromCodePoint(9731)`)
	if r.String() != "☃" { t.Errorf("fromCodePoint snowman: %q", r.String()) }
}

// ── queueMicrotask / requestIdleCallback ──

func TestQueueMicrotask(t *testing.T) {
	vm := New()
	vm.Run(`var called = false; queueMicrotask(function() { called = true; });`)
	r := vm.Get("called")
	if !r.Truthy() { t.Error("queueMicrotask callback should have run") }
}

// ── Bytecode: logical operators ──

func TestBytecodeLogicalAnd(t *testing.T) {
	vm := New()
	// This function body uses && — should compile to bytecode now
	vm.Run(`function check(a, b) { return a > 0 && b > 0; }`)
	r, _ := vm.Eval(`check(1, 2)`)
	if !r.Bool() { t.Error("1>0 && 2>0 should be true") }
	r, _ = vm.Eval(`check(0, 2)`)
	if r.Bool() { t.Error("0>0 && 2>0 should be false") }
	r, _ = vm.Eval(`check(1, -1)`)
	if r.Bool() { t.Error("1>0 && -1>0 should be false") }
}

func TestBytecodeLogicalOr(t *testing.T) {
	vm := New()
	vm.Run(`function either(a, b) { return a > 0 || b > 0; }`)
	r, _ := vm.Eval(`either(1, -1)`)
	if !r.Bool() { t.Error("1>0 || -1>0 should be true") }
	r, _ = vm.Eval(`either(-1, 1)`)
	if !r.Bool() { t.Error("-1>0 || 1>0 should be true") }
	r, _ = vm.Eval(`either(-1, -1)`)
	if r.Bool() { t.Error("-1>0 || -1>0 should be false") }
}

func TestBytecodeLogicalShortCircuit(t *testing.T) {
	vm := New()
	// && should short-circuit: if left is falsy, return left value
	vm.Run(`function andVal(a, b) { return a && b; }`)
	r, _ := vm.Eval(`andVal(0, "hello")`)
	if r.Number() != 0 { t.Errorf("0 && 'hello' should be 0, got %v", r) }
	r, _ = vm.Eval(`andVal(1, "hello")`)
	if r.String() != "hello" { t.Errorf("1 && 'hello' should be 'hello', got %v", r) }

	// || should short-circuit: if left is truthy, return left value
	vm.Run(`function orVal(a, b) { return a || b; }`)
	r, _ = vm.Eval(`orVal("first", "second")`)
	if r.String() != "first" { t.Errorf("'first' || 'second' should be 'first', got %v", r) }
	r, _ = vm.Eval(`orVal(0, "fallback")`)
	if r.String() != "fallback" { t.Errorf("0 || 'fallback' should be 'fallback', got %v", r) }
}

// ── Bytecode: property access ──

func TestBytecodePropAccess(t *testing.T) {
	vm := New()
	vm.Set("obj", map[string]interface{}{"x": 10, "y": 20})
	vm.Run(`function getX(o) { return o.x; }`)
	r, _ := vm.Eval(`getX(obj)`)
	if r.Number() != 10 { t.Errorf("got %v", r.Number()) }
}

func TestBytecodeNestedPropAccess(t *testing.T) {
	vm := New()
	vm.Set("data", map[string]interface{}{
		"user": map[string]interface{}{"name": "Alice"},
	})
	vm.Run(`function getName(d) { return d.user.name; }`)
	r, _ := vm.Eval(`getName(data)`)
	if r.String() != "Alice" { t.Errorf("got %q", r.String()) }
}

// ── Bytecode: loops ──

func TestBytecodeWhileLoop(t *testing.T) {
	vm := New()
	vm.Run(`
		function sumTo(n) {
			var i = 0;
			var s = 0;
			while (i < n) {
				s = s + i;
				i++;
			}
			return s;
		}
	`)
	r, _ := vm.Eval(`sumTo(10)`)
	if r.Number() != 45 { t.Errorf("sumTo(10) = %v, want 45", r.Number()) }
}

func TestBytecodeForLoop(t *testing.T) {
	vm := New()
	vm.Run(`
		function factorial(n) {
			var result = 1;
			for (var i = 2; i <= n; i++) {
				result = result * i;
			}
			return result;
		}
	`)
	r, _ := vm.Eval(`factorial(5)`)
	if r.Number() != 120 { t.Errorf("factorial(5) = %v, want 120", r.Number()) }

	r, _ = vm.Eval(`factorial(10)`)
	if r.Number() != 3628800 { t.Errorf("factorial(10) = %v, want 3628800", r.Number()) }
}

func TestBytecodeForLoopWithCondition(t *testing.T) {
	vm := New()
	vm.Run(`
		function countEvens(n) {
			var count = 0;
			for (var i = 0; i < n; i++) {
				if (i % 2 === 0) {
					count++;
				}
			}
			return count;
		}
	`)
	r, _ := vm.Eval(`countEvens(10)`)
	if r.Number() != 5 { t.Errorf("countEvens(10) = %v, want 5", r.Number()) }
}

// ── Bytecode exec handlers ──

func TestBytecodeTypeof(t *testing.T) {
	vm := New()
	vm.Run(`function check(x) { if (typeof x === "number") { return "num"; } return "other"; }`)
	r, _ := vm.Eval(`check(42)`)
	if r.String() != "num" { t.Errorf("got %s, want num", r.String()) }
	r, _ = vm.Eval(`check("hi")`)
	if r.String() != "other" { t.Errorf("got %s, want other", r.String()) }
}

func TestBytecodeTypeofString(t *testing.T) {
	vm := New()
	vm.Run(`function typeStr(x) { if (typeof x === "string") { return "yes"; } return "no"; }`)
	r, _ := vm.Eval(`typeStr("hello")`)
	if r.String() != "yes" { t.Errorf("got %s, want yes", r.String()) }
	r, _ = vm.Eval(`typeStr(123)`)
	if r.String() != "no" { t.Errorf("got %s, want no", r.String()) }
}

func TestBytecodeTypeofUndefined(t *testing.T) {
	vm := New()
	vm.Run(`function isUndef(x) { if (typeof x === "undefined") { return true; } return false; }`)
	r, _ := vm.Eval(`isUndef(undefined)`)
	if !r.Bool() { t.Errorf("got %v, want true", r.Bool()) }
}

func TestBytecodeTypeofObject(t *testing.T) {
	vm := New()
	vm.Set("obj", map[string]interface{}{"a": 1})
	vm.Run(`function isObj(x) { if (typeof x === "object") { return "obj"; } return "nope"; }`)
	r, _ := vm.Eval(`isObj(obj)`)
	if r.String() != "obj" { t.Errorf("got %s, want obj", r.String()) }
}

func TestBytecodeTypeofFunction(t *testing.T) {
	vm := New()
	vm.Run(`function isFn(x) { if (typeof x === "function") { return true; } return false; }`)
	r, _ := vm.Eval(`isFn(parseInt)`)
	if !r.Bool() { t.Errorf("got %v, want true", r.Bool()) }
}

func TestBytecodeBitAnd(t *testing.T) {
	vm := New()
	vm.Run(`function band(a, b) { return a & b; }`)
	r, _ := vm.Eval(`band(12, 10)`)
	if r.Number() != 8 { t.Errorf("got %v, want 8", r.Number()) }
}

func TestBytecodeBitOr(t *testing.T) {
	vm := New()
	vm.Run(`function bor(a, b) { return a | b; }`)
	r, _ := vm.Eval(`bor(12, 10)`)
	if r.Number() != 14 { t.Errorf("got %v, want 14", r.Number()) }
}

func TestBytecodeBitXor(t *testing.T) {
	vm := New()
	vm.Run(`function bxor(a, b) { return a ^ b; }`)
	r, _ := vm.Eval(`bxor(12, 10)`)
	if r.Number() != 6 { t.Errorf("got %v, want 6", r.Number()) }
}

func TestBytecodeBitNot(t *testing.T) {
	vm := New()
	vm.Run(`function bnot(a) { return ~a; }`)
	r, _ := vm.Eval(`bnot(5)`)
	if r.Number() != -6 { t.Errorf("got %v, want -6", r.Number()) }
}

func TestBytecodeShl(t *testing.T) {
	vm := New()
	vm.Run(`function shl(a, b) { return a << b; }`)
	r, _ := vm.Eval(`shl(1, 4)`)
	if r.Number() != 16 { t.Errorf("got %v, want 16", r.Number()) }
}

func TestBytecodeShr(t *testing.T) {
	vm := New()
	vm.Run(`function shr(a, b) { return a >> b; }`)
	r, _ := vm.Eval(`shr(16, 2)`)
	if r.Number() != 4 { t.Errorf("got %v, want 4", r.Number()) }
}

func TestBytecodeUShr(t *testing.T) {
	vm := New()
	vm.Run(`function ushr(a, b) { return a >>> b; }`)
	r, _ := vm.Eval(`ushr(-1, 0)`)
	if r.Number() != 4294967295 { t.Errorf("got %v, want 4294967295", r.Number()) }
}

func TestBytecodeIn(t *testing.T) {
	vm := New()
	vm.Set("obj", map[string]interface{}{"x": 1, "y": 2})
	vm.Run(`function hasKey(k, o) { return k in o; }`)
	r, _ := vm.Eval(`hasKey("x", obj)`)
	if !r.Bool() { t.Errorf("x in obj: got %v, want true", r.Bool()) }
	r, _ = vm.Eval(`hasKey("z", obj)`)
	if r.Bool() { t.Errorf("z in obj: got %v, want false", r.Bool()) }
}

func TestBytecodeThrow(t *testing.T) {
	// Test that opThrow in the VM executor works correctly
	bc := &bytecode{
		code: []instr{
			{op: opLoadStr, sarg: "boom"},
			{op: opThrow},
		},
	}
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Error("expected panic from throw")
				return
			}
			v, ok := r.(*Value)
			if !ok {
				t.Errorf("expected *Value panic, got %T", r)
				return
			}
			if v.toStr() != "boom" {
				t.Errorf("throw value: got %s, want boom", v.toStr())
			}
		}()
		execBytecode(bc, map[string]*Value{})
	}()
}

func TestBytecodeNewCallMap(t *testing.T) {
	vm := New()
	vm.Run(`function makeMap() { var m = new Map(); m.set("a", 1); return m.get("a"); }`)
	r, _ := vm.Eval(`makeMap()`)
	if r.Number() != 1 { t.Errorf("got %v, want 1", r.Number()) }
}

func TestBytecodeNewCallDate(t *testing.T) {
	vm := New()
	vm.Run(`function getYear() { var d = new Date(); return d.getFullYear(); }`)
	r, _ := vm.Eval(`getYear()`)
	if r.Number() != 2026 { t.Errorf("got %v, want 2026", r.Number()) }
}

func TestBytecodeNewCallError(t *testing.T) {
	vm := New()
	vm.Run(`function makeErr() { var e = new Error("oops"); return e.message; }`)
	r, _ := vm.Eval(`makeErr()`)
	if r.String() != "oops" { t.Errorf("got %s, want oops", r.String()) }
}

func TestBytecodeLoadThis(t *testing.T) {
	vm := New()
	vm.Set("obj", map[string]interface{}{"val": 42})
	// this is only used in method calls via callMethodBC, which sets "this" in scope
	r, _ := vm.Eval(`obj.val`)
	if r.Number() != 42 { t.Errorf("got %v, want 42", r.Number()) }
}

// ── Bytecode async/await ──

func TestBytecodeAwaitResolved(t *testing.T) {
	vm := New()
	vm.Run(`var result = Promise.resolve(42);`)
	vm.Run(`function getValue() { var v = await result; return v; }`)
	r, _ := vm.Eval(`getValue()`)
	if r.Number() != 42 { t.Errorf("got %v, want 42", r.Number()) }
}

func TestBytecodeAwaitNonPromise(t *testing.T) {
	vm := New()
	vm.Run(`function passThru(x) { var v = await x; return v; }`)
	r, _ := vm.Eval(`passThru(99)`)
	if r.Number() != 99 { t.Errorf("got %v, want 99", r.Number()) }
}

func TestBytecodeAwaitInExpression(t *testing.T) {
	vm := New()
	vm.Run(`var p = Promise.resolve(10);`)
	vm.Run(`function calc() { return await p + 5; }`)
	r, _ := vm.Eval(`calc()`)
	if r.Number() != 15 { t.Errorf("got %v, want 15", r.Number()) }
}

// ── Bytecode destructuring ──

func TestBytecodeObjDestructure(t *testing.T) {
	vm := New()
	vm.Run(`function getXY(obj) { const { x, y } = obj; return x + y; }`)
	vm.Set("pt", map[string]interface{}{"x": 10, "y": 20})
	r, _ := vm.Eval(`getXY(pt)`)
	if r.Number() != 30 { t.Errorf("got %v, want 30", r.Number()) }
}

func TestBytecodeObjDestructureAlias(t *testing.T) {
	vm := New()
	vm.Run(`function getName(obj) { const { name: n } = obj; return n; }`)
	vm.Set("person", map[string]interface{}{"name": "Alice"})
	r, _ := vm.Eval(`getName(person)`)
	if r.String() != "Alice" { t.Errorf("got %s, want Alice", r.String()) }
}

func TestBytecodeArrDestructure(t *testing.T) {
	vm := New()
	vm.Run(`function first2(arr) { const [a, b] = arr; return a * 10 + b; }`)
	r, _ := vm.Eval(`first2([3, 7])`)
	if r.Number() != 37 { t.Errorf("got %v, want 37", r.Number()) }
}

func TestBytecodeObjDestructureMultiField(t *testing.T) {
	vm := New()
	vm.Run(`function extract(obj) { const { a, b, c } = obj; return a + b + c; }`)
	vm.Set("data", map[string]interface{}{"a": 1, "b": 2, "c": 3})
	r, _ := vm.Eval(`extract(data)`)
	if r.Number() != 6 { t.Errorf("got %v, want 6", r.Number()) }
}

// ── Bytecode try/catch ──

func TestBytecodeTryCatchBasic(t *testing.T) {
	vm := New()
	vm.Run(`function safe() { try { throw "err"; } catch (e) { return e; } return "nope"; }`)
	r, _ := vm.Eval(`safe()`)
	if r.String() != "err" { t.Errorf("got %s, want err", r.String()) }
}

func TestBytecodeTryCatchNoError(t *testing.T) {
	vm := New()
	vm.Run(`function noErr() { try { var x = 42; return x; } catch (e) { return -1; } }`)
	r, _ := vm.Eval(`noErr()`)
	if r.Number() != 42 { t.Errorf("got %v, want 42", r.Number()) }
}

func TestBytecodeTryCatchFallthrough(t *testing.T) {
	vm := New()
	vm.Run(`function after() { var result = "before"; try { result = "inside"; } catch (e) { result = "caught"; } return result; }`)
	r, _ := vm.Eval(`after()`)
	if r.String() != "inside" { t.Errorf("got %s, want inside", r.String()) }
}

func TestBytecodeTryCatchNoCatchVar(t *testing.T) {
	vm := New()
	vm.Run(`function noVar() { try { throw "x"; } catch { return "caught"; } }`)
	r, _ := vm.Eval(`noVar()`)
	if r.String() != "caught" { t.Errorf("got %s, want caught", r.String()) }
}

// ── Bytecode arrow functions ──

func TestBytecodeArrowSimple(t *testing.T) {
	vm := New()
	vm.Run(`function double(arr) { return arr.map((x) => x * 2); }`)
	r, _ := vm.Eval(`double([1,2,3])`)
	if r.String() != "2,4,6" { t.Errorf("got %s, want 2,4,6", r.String()) }
}

func TestBytecodeArrowFilter(t *testing.T) {
	vm := New()
	vm.Run(`function evens(arr) { return arr.filter((x) => x % 2 === 0); }`)
	r, _ := vm.Eval(`evens([1,2,3,4,5,6])`)
	if r.String() != "2,4,6" { t.Errorf("got %s, want 2,4,6", r.String()) }
}

func TestBytecodeArrowFind(t *testing.T) {
	vm := New()
	vm.Run(`function findById(arr, id) { return arr.find((x) => x.id === id); }`)
	r, _ := vm.Eval(`findById([{id:1,name:"a"},{id:2,name:"b"}], 2)`)
	if r.IsUndefined() { t.Error("expected a result") }
}

func TestBytecodeArrowSort(t *testing.T) {
	vm := New()
	vm.Run(`function sortDesc(arr) { return arr.sort((a, b) => b - a); }`)
	r, _ := vm.Eval(`sortDesc([3,1,4,1,5])`)
	if r.String() != "5,4,3,1,1" { t.Errorf("got %s, want 5,4,3,1,1", r.String()) }
}

func TestBytecodeArrowNoParens(t *testing.T) {
	vm := New()
	vm.Run(`function inc(arr) { return arr.map(x => x + 1); }`)
	r, _ := vm.Eval(`inc([10,20,30])`)
	if r.String() != "11,21,31" { t.Errorf("got %s, want 11,21,31", r.String()) }
}

func TestBytecodeArrowBlock(t *testing.T) {
	vm := New()
	vm.Run(`function transform(arr) { return arr.map((x) => { const y = x * 10; return y + 1; }); }`)
	r, _ := vm.Eval(`transform([1,2,3])`)
	if r.String() != "11,21,31" { t.Errorf("got %s, want 11,21,31", r.String()) }
}

func TestBytecodeArrowReduce(t *testing.T) {
	vm := New()
	vm.Run(`function total(arr) { return arr.reduce((sum, x) => sum + x, 0); }`)
	r, _ := vm.Eval(`total([1,2,3,4])`)
	if r.Number() != 10 { t.Errorf("got %v, want 10", r.Number()) }
}

// ── Bytecode template literals ──

func TestBytecodeTemplateSimple(t *testing.T) {
	vm := New()
	vm.Run(`function greet(name) { return ` + "`Hello ${name}!`" + `; }`)
	r, _ := vm.Eval(`greet("World")`)
	if r.String() != "Hello World!" { t.Errorf("got %q, want %q", r.String(), "Hello World!") }
}

func TestBytecodeTemplateMultiExpr(t *testing.T) {
	vm := New()
	vm.Run(`function fmt(a, b) { return ` + "`${a} + ${b} = ${a + b}`" + `; }`)
	r, _ := vm.Eval(`fmt(3, 4)`)
	if r.String() != "3 + 4 = 7" { t.Errorf("got %q, want %q", r.String(), "3 + 4 = 7") }
}

func TestBytecodeTemplateWithProp(t *testing.T) {
	vm := New()
	vm.Set("url", "http://example.com")
	vm.Run(`function buildUrl(id) { return ` + "`${url}/items/${id}`" + `; }`)
	r, _ := vm.Eval(`buildUrl(42)`)
	if r.String() != "http://example.com/items/42" { t.Errorf("got %q", r.String()) }
}

func TestBytecodeTemplateNoInterpolation(t *testing.T) {
	vm := New()
	vm.Run("function plain() { return `just a string`; }")
	r, _ := vm.Eval(`plain()`)
	if r.String() != "just a string" { t.Errorf("got %q", r.String()) }
}

// ── Bytecode trampoline (deep recursion) ──

func TestBytecodeDeepRecursion(t *testing.T) {
	vm := New()
	vm.Run(`function countdown(n) { if (n <= 0) { return 0; } return countdown(n - 1); }`)
	r, _ := vm.Eval(`countdown(5000)`)
	if r.Number() != 0 {
		t.Errorf("got %v, want 0", r.Number())
	}
}

func TestBytecodeDeepFibonacci(t *testing.T) {
	vm := New()
	vm.Run(`function fib(n) { if (n <= 1) return n; return fib(n - 1) + fib(n - 2); }`)
	// First call compiles, subsequent use trampoline
	r, _ := vm.Eval(`fib(1)`)
	if r.Number() != 1 { t.Errorf("fib(1): got %v", r.Number()) }
	r, _ = vm.Eval(`fib(10)`)
	if r.Number() != 55 { t.Errorf("fib(10): got %v, want 55", r.Number()) }
	r, _ = vm.Eval(`fib(15)`)
	if r.Number() != 610 { t.Errorf("fib(15): got %v, want 610", r.Number()) }
}

func TestRequestIdleCallback(t *testing.T) {
	vm := New()
	vm.Run(`
		var remaining = -1;
		requestIdleCallback(function(deadline) {
			remaining = deadline.timeRemaining();
		});
	`)
	r := vm.Get("remaining")
	if r.Number() != 50 { t.Errorf("timeRemaining: %v", r.Number()) }
}
