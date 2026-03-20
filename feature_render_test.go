package espresso

import (
	"encoding/json"
	"testing"
)

// TestFeatureRendering tests the exact JS patterns used in pricing page feature rendering
func TestFeatureRendering(t *testing.T) {
	// Simulate the API data structure
	featureJSON := `[
		{"feature_name": "Device Limit", "feature_type": "numeric", "feature_unit": "devices", "active": true, "value": {"value": 5}},
		{"feature_name": "Webhook", "feature_type": "boolean", "active": true, "value": {"value": true}},
		{"feature_name": "Analytics", "feature_type": "boolean", "active": true, "value": {"value": false}},
		{"feature_name": "Support", "feature_type": "string", "active": true, "value": {"value": "Community"}}
	]`

	tests := []struct {
		name     string
		code     string
		expected string
	}{
		{
			name:     "typeof nested object",
			code:     `var f = {value: {value: 5}}; typeof f.value === "object" ? "obj" : "other"`,
			expected: "obj",
		},
		{
			name:     "nested value access",
			code:     `var f = {value: {value: 5}}; typeof f.value === "object" && f.value !== null ? f.value.value : f.value`,
			expected: "5",
		},
		{
			name:     "boolean check on extracted value",
			code:     `var val = true; typeof val === "boolean" ? val === true : false`,
			expected: "true",
		},
		{
			name:     "ternary class selection",
			code:     `var isIncluded = true; isIncluded ? "text-foreground" : "text-muted line-through"`,
			expected: "text-foreground",
		},
		{
			name:     "ternary SVG path",
			code:     `var isIncluded = true; isIncluded ? "M5 13l4 4L19 7" : "M6 18L18 6M6 6l12 12"`,
			expected: "M5 13l4 4L19 7",
		},
		{
			name:     "style object with ternary",
			code:     `var isIncluded = true; var color = "#b45309"; var s = {color: isIncluded ? color : "gray"}; s.color`,
			expected: "#b45309",
		},
		{
			name:     "triple && with typeof",
			code:     `var ft = "numeric"; var val = 5; ft === "numeric" && typeof val === "number" && "show"`,
			expected: "show",
		},
		{
			name:     "triple && false",
			code:     `var ft = "boolean"; var val = 5; ft === "numeric" && typeof val === "number" && "show"`,
			expected: "false",
		},
		{
			name:     "toLocaleString with unit",
			code:     `var val = 5000; var unit = "devices"; val.toLocaleString() + " " + unit`,
			expected: "5,000 devices",
		},
		{
			name:     "ternary unit suffix",
			code:     `var unit = "devices"; unit ? " " + unit : ""`,
			expected: " devices",
		},
		{
			name:     "ternary unit suffix null",
			code:     `var unit = null; unit ? " " + unit : ""`,
			expected: "",
		},
		{
			name:     "sort with property access",
			code:     `var items = [{order: 30}, {order: 10}, {order: 20}]; items.sort((a, b) => (a.order || 0) - (b.order || 0)); items.map((x) => x.order)`,
			expected: "10,20,30",
		},
		{
			name: "map with const and typeof inside block body",
			code: `var features = [{feature_name: "Limit", feature_type: "numeric", value: {value: 5}}, {feature_name: "Hook", feature_type: "boolean", value: {value: true}}];
features.map((feature) => {
  const actualValue = typeof feature.value === "object" && feature.value !== null ? feature.value.value : feature.value;
  const isIncluded = feature.feature_type === "boolean" ? actualValue === true : true;
  return feature.feature_name + ":" + isIncluded + ":" + actualValue;
})`,
			expected: "Limit:true:5,Hook:true:true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := New()
			result, err := vm.Eval(tt.code)
			if err != nil {
				t.Fatalf("Eval error: %v", err)
			}
			if result.String() != tt.expected {
				t.Errorf("got %q, want %q", result.String(), tt.expected)
			}
		})
	}

	// Test with real JSON data via EvalStatements (SSR path)
	t.Run("full feature map from JSON data", func(t *testing.T) {
		scope := map[string]*Value{}
		scope["features"] = JsonToValue(json.RawMessage(featureJSON))

		code := `const result = features.map((feature) => {
  const actualValue = typeof feature.value === "object" && feature.value !== null ? feature.value.value : feature.value;
  const isIncluded = feature.feature_type === "boolean" ? actualValue === true : true;
  return feature.feature_name + ":" + actualValue;
});
return result;`

		tokens := Tokenize(code)
		ev := NewEval(tokens, scope)
		result := ev.EvalStatements()
		if result == nil {
			t.Fatal("EvalStatements returned nil")
		}
		expected := "Device Limit:5,Webhook:true,Analytics:false,Support:Community"
		if result.String() != expected {
			t.Errorf("got %q, want %q", result.String(), expected)
		}
	})
}
