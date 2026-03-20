package espresso

import (
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
