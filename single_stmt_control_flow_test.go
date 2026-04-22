package espresso

import "testing"

// When an if/else/while/for body is a single statement (no braces) that is
// itself a control-flow statement (e.g. `if (cond) for (k in obj) { … }`),
// the body must dispatch through the statement handlers, not e.expr() — which
// silently swallows keywords like `for` as no-ops.

func TestIf_WithoutBraces_WrapsForIn(t *testing.T) {
	vm := New()
	vm.Set("obj", map[string]interface{}{"a": 1.0, "b": 2.0, "c": 3.0})
	r, _ := vm.Run(`
		let c = 0;
		let flag = true;
		if (flag) for (const k in obj) { c++; }
		return c;
	`)
	if r.Number() != 3 {
		t.Errorf("expected 3, got %v — for..in inside brace-less if was not dispatched", r.Number())
	}
}

func TestIf_WithoutBraces_WrapsWhile(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		let c = 0;
		let flag = true;
		if (flag) while (c < 3) c++;
		return c;
	`)
	if r.Number() != 3 {
		t.Errorf("expected 3, got %v", r.Number())
	}
}

// Regression: the exact shape that broke per-tenant theming in content-site.
// `if (t.palette) for (const k in t.palette) palette[k] = t.palette[k];`
// Was silently iterating zero times because the for was parsed as an
// expression and discarded.
func TestIf_WithoutBraces_ForInNestedJSON(t *testing.T) {
	vm := New()
	vm.Set("site", map[string]interface{}{
		"theme": map[string]interface{}{
			"palette": map[string]interface{}{
				"primary": "#6366f1",
				"accent":  "#7c3aed",
				"surface": "#09090b",
			},
		},
	})
	r, _ := vm.Run(`
		const t = site.theme;
		const p = {};
		if (t.palette) for (const k in t.palette) p[k] = t.palette[k];
		return p.accent + "|" + p.primary + "|" + p.surface;
	`)
	got := r.String()
	want := "#7c3aed|#6366f1|#09090b"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}
