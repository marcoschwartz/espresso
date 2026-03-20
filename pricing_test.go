package espresso

import (
	"encoding/json"
	"testing"
)

// TestPricingPageFeatures tests all JS features needed for the pricing page rendering:
// typeof, optional chaining, toLocaleString, arrow functions with const, nested ternaries
func TestPricingPageFeatures(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected string
	}{
		{
			name:     "typeof object",
			code:     `typeof {value: 1}`,
			expected: "object",
		},
		{
			name:     "typeof number",
			code:     `typeof 42`,
			expected: "number",
		},
		{
			name:     "typeof boolean",
			code:     `typeof true`,
			expected: "boolean",
		},
		{
			name:     "typeof string",
			code:     `typeof "hello"`,
			expected: "string",
		},
		{
			name:     "typeof in ternary",
			code:     `typeof {value: 1} === "object" ? "yes" : "no"`,
			expected: "yes",
		},
		{
			name:     "toLocaleString on number",
			code:     `(1000).toLocaleString()`,
			expected: "1,000",
		},
		{
			name:     "toLocaleString on small number",
			code:     `(42).toLocaleString()`,
			expected: "42",
		},
		{
			name:     "toLocaleString on large number",
			code:     `(1000000).toLocaleString()`,
			expected: "1,000,000",
		},
		{
			name:     "optional chaining on null",
			code:     `var x = null; x?.value`,
			expected: "undefined",
		},
		{
			name:     "optional chaining on object",
			code:     `var x = {value: 42}; x?.value`,
			expected: "42",
		},
		{
			name: "nested typeof with ternary",
			code: `var feature = {value: {value: 100}};
var actualValue = typeof feature.value === "object" && feature.value !== null ? feature.value.value : feature.value;
actualValue`,
			expected: "100",
		},
		{
			name: "typeof boolean check",
			code: `var val = true;
typeof val === "boolean" ? "is bool" : "not bool"`,
			expected: "is bool",
		},
		{
			name: "typeof number check with comparison",
			code: `var val = 500;
typeof val !== "number" || val <= 0 ? "skip" : "show"`,
			expected: "show",
		},
		{
			name: "arrow function with typeof inside map",
			code: `var items = [{value: {value: 100}}, {value: {value: true}}];
items.map((item) => {
  var v = typeof item.value === "object" ? item.value.value : item.value;
  return typeof v === "boolean" ? "bool" : "num:" + v;
})`,
			expected: "num:100,bool",
		},
		{
			name: "number division in template literal",
			code: `var val = 3600; ` + "`${val / 3600} hour`",
			expected: "1 hour",
		},
		{
			name: "val.toLocaleString in arrow",
			code: `var val = 500; val.toLocaleString()`,
			expected: "500",
		},
		{
			name: "formatValue-like function",
			code: `var formatValue = (val, unit) => {
  if (typeof val === "boolean") return null;
  if (typeof val !== "number" || val <= 0) return null;
  if (unit === "seconds") {
    if (val >= 3600) return val / 3600 + " hour";
    return val / 60 + " min";
  }
  return val.toLocaleString();
};
formatValue(100, "hours") + "|" + formatValue(3600, "seconds") + "|" + formatValue(true, "bool")`,
			expected: "100|1 hour|null",
		},
		{
			name: "find on array",
			code: `var prices = [{active: false, unit_amount: 0}, {active: true, unit_amount: 1900}];
var price = prices.find((p) => p.active);
price.unit_amount`,
			expected: "1900",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := New()
			result, err := vm.Eval(tt.code)
			if err != nil {
				t.Fatalf("Eval error: %v", err)
			}
			got := result.String()
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestPricingEvalStatements tests the same features via EvalStatements (used by SSR)
func TestPricingEvalStatements(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected string
	}{
		{
			name: "typeof + ternary in statements",
			code: `const feature = {value: {value: 500}};
const actualValue = typeof feature.value === "object" && feature.value !== null ? feature.value.value : feature.value;
const isActive = typeof actualValue === "boolean" ? actualValue : true;
const result = isActive ? "active:" + actualValue : "inactive";
return result;`,
			expected: "active:500",
		},
		{
			name: "formatValue with toLocaleString in statements",
			code: `const formatValue = (val, unit) => {
  if (typeof val === "boolean") return null;
  if (typeof val !== "number" || val <= 0) return null;
  if (unit === "seconds") {
    if (val >= 3600) return val / 3600 + " hour";
    return val / 60 + " min";
  }
  return val.toLocaleString();
};
return formatValue(1000, "hours");`,
			expected: "1,000",
		},
		{
			name: "map with const inside arrow body",
			code: `const items = [{v: 1}, {v: 2}, {v: 3}];
const result = items.map((item) => {
  const doubled = item.v * 2;
  return doubled;
});
return result;`,
			expected: "2,4,6",
		},
		{
			name: "full pricing feature rendering simulation",
			code: `const features = [
  {feature_name: "Browser Hours", feature_unit: "hours", value: {value: 100}},
  {feature_name: "Sessions", feature_unit: "seconds", value: {value: 3600}},
  {feature_name: "Screenshots", feature_unit: null, value: {value: true}}
];
const result = features.map((f) => {
  const val = typeof f.value === "object" ? f.value.value : f.value;
  if (typeof val === "boolean") return f.feature_name;
  if (f.feature_unit === "seconds" && val >= 3600) return val / 3600 + " hour " + f.feature_name;
  return val + " " + f.feature_name;
});
return result;`,
			expected: "100 Browser Hours,1 hour Sessions,Screenshots",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := Tokenize(tt.code)
			scope := map[string]*Value{}
			ev := NewEval(tokens, scope)
			result := ev.EvalStatements()
			if result == nil {
				t.Fatal("EvalStatements returned nil")
			}
			got := result.String()
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestToLocaleStringJSON tests toLocaleString works on values from JSON data
func TestToLocaleStringJSON(t *testing.T) {
	vm := New()
	data := json.RawMessage(`{"value": 1234567}`)
	vm.SetValue("data", JsonToValue(data))
	result, _ := vm.Eval(`data.value.toLocaleString()`)
	if result.String() != "1,234,567" {
		t.Errorf("got %q, want %q", result.String(), "1,234,567")
	}
}
