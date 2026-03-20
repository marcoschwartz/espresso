package espresso

import (
	"testing"
)

func TestIntlNumberFormatCurrency(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected string
	}{
		{
			name:     "basic USD format",
			code:     `new Intl.NumberFormat('en-US', {style: 'currency', currency: 'USD', minimumFractionDigits: 0, maximumFractionDigits: 0}).format(1900)`,
			expected: "$1,900",
		},
		{
			name:     "zero dollars",
			code:     `new Intl.NumberFormat('en-US', {style: 'currency', currency: 'USD', minimumFractionDigits: 0, maximumFractionDigits: 0}).format(0)`,
			expected: "$0",
		},
		{
			name:     "cents to dollars",
			code:     `new Intl.NumberFormat('en-US', {style: 'currency', currency: 'USD', minimumFractionDigits: 0, maximumFractionDigits: 0}).format(7900)`,
			expected: "$7,900",
		},
		{
			name:     "formatPrice function",
			code:     `function formatPrice(amount, currency) { return new Intl.NumberFormat('en-US', {style: 'currency', currency: currency.toUpperCase(), minimumFractionDigits: 0, maximumFractionDigits: 0}).format(amount / 100); } formatPrice(1900, "usd")`,
			expected: "$19",
		},
		{
			name:     "formatPrice zero",
			code:     `function formatPrice(amount, currency) { return new Intl.NumberFormat('en-US', {style: 'currency', currency: currency.toUpperCase(), minimumFractionDigits: 0, maximumFractionDigits: 0}).format(amount / 100); } formatPrice(0, "usd")`,
			expected: "$0",
		},
		{
			name:     "formatPrice 7900",
			code:     `function formatPrice(amount, currency) { return new Intl.NumberFormat('en-US', {style: 'currency', currency: currency.toUpperCase(), minimumFractionDigits: 0, maximumFractionDigits: 0}).format(amount / 100); } formatPrice(7900, "usd")`,
			expected: "$79",
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
}

func TestAndChainInExpressions(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected string
	}{
		{
			name:     "simple && chain true",
			code:     `"hours" && "hours" !== "seconds" && true && "show"`,
			expected: "show",
		},
		{
			name:     "&& chain false in middle",
			code:     `"seconds" && "seconds" !== "seconds" && true && "show"`,
			expected: "false",
		},
		{
			name:     "&& chain with null",
			code:     `null && "nope"`,
			expected: "null",
		},
		{
			name:     "&& chain with falsy first",
			code:     `"" && "nope"`,
			expected: "",
		},
		{
			name:     "triple && all truthy",
			code:     `"a" && "b" && "c"`,
			expected: "c",
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
}
