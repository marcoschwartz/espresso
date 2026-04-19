package espresso

import (
	"testing"
)

// ── RegExp literal parsing ──────────────────────────────────────

func TestRegExp_LiteralTest(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`/hello/.test("hello world")`)
	if !r.Bool() {
		t.Error("expected /hello/.test('hello world') to be true")
	}
}

func TestRegExp_LiteralTestFalse(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`/xyz/.test("hello world")`)
	if r.Truthy() {
		t.Error("expected /xyz/.test('hello world') to be false")
	}
}

func TestRegExp_LiteralWithFlags(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`/hello/i.test("Hello World")`)
	if !r.Bool() {
		t.Error("expected case-insensitive match to be true")
	}
}

func TestRegExp_Exec(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`/(\d+)-(\d+)/.exec("date: 2026-03")`)
	if r.IsNull() {
		t.Fatal("expected exec to return an array, got null")
	}
	arr := r.Array()
	if len(arr) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(arr))
	}
	if arr[0].String() != "2026-03" {
		t.Errorf("expected full match '2026-03', got '%s'", arr[0].String())
	}
	if arr[1].String() != "2026" {
		t.Errorf("expected group 1 '2026', got '%s'", arr[1].String())
	}
	if arr[2].String() != "03" {
		t.Errorf("expected group 2 '03', got '%s'", arr[2].String())
	}
}

func TestRegExp_ExecNoMatch(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`/xyz/.exec("hello")`)
	if !r.IsNull() {
		t.Error("expected exec with no match to return null")
	}
}

// ── new RegExp() constructor ────────────────────────────────────

func TestRegExp_Constructor(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`
		const re = new RegExp("\\d+", "g");
		re.test("abc123")
	`)
	if !r.Bool() {
		t.Error("expected new RegExp('\\d+', 'g').test('abc123') to be true")
	}
}

func TestRegExp_ConstructorNoFlags(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`
		const re = new RegExp("hello");
		re.test("hello world")
	`)
	if !r.Bool() {
		t.Error("expected new RegExp('hello').test('hello world') to be true")
	}
}

// ── RegExp properties ───────────────────────────────────────────

func TestRegExp_Source(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`/abc/gi.source`)
	if r.String() != "abc" {
		t.Errorf("expected source 'abc', got '%s'", r.String())
	}
}

func TestRegExp_Flags(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`/abc/gi.flags`)
	if r.String() != "gi" {
		t.Errorf("expected flags 'gi', got '%s'", r.String())
	}
}

// ── String.match() ──────────────────────────────────────────────

func TestString_MatchGlobal(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"test 123 and 456".match(/\d+/g)`)
	if r.IsNull() {
		t.Fatal("expected match to return array, got null")
	}
	arr := r.Array()
	if len(arr) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(arr))
	}
	if arr[0].String() != "123" {
		t.Errorf("expected first match '123', got '%s'", arr[0].String())
	}
	if arr[1].String() != "456" {
		t.Errorf("expected second match '456', got '%s'", arr[1].String())
	}
}

func TestString_MatchNonGlobal(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"test 123 and 456".match(/(\d+)/)`)
	if r.IsNull() {
		t.Fatal("expected match to return array, got null")
	}
	arr := r.Array()
	if len(arr) < 2 {
		t.Fatalf("expected at least 2 elements (full match + group), got %d", len(arr))
	}
	if arr[0].String() != "123" {
		t.Errorf("expected full match '123', got '%s'", arr[0].String())
	}
	if arr[1].String() != "123" {
		t.Errorf("expected group 1 '123', got '%s'", arr[1].String())
	}
}

func TestString_MatchNoResult(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"hello".match(/\d+/)`)
	if !r.IsNull() {
		t.Error("expected no match to return null")
	}
}

// ── String.replace() with RegExp ────────────────────────────────

func TestString_ReplaceRegexp(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"hello world".replace(/world/, "Go")`)
	if r.String() != "hello Go" {
		t.Errorf("expected 'hello Go', got '%s'", r.String())
	}
}

func TestString_ReplaceRegexpGlobal(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"aaa bbb aaa".replace(/aaa/g, "ccc")`)
	if r.String() != "ccc bbb ccc" {
		t.Errorf("expected 'ccc bbb ccc', got '%s'", r.String())
	}
}

func TestString_ReplaceRegexpFirstOnly(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"aaa bbb aaa".replace(/aaa/, "ccc")`)
	if r.String() != "ccc bbb aaa" {
		t.Errorf("expected 'ccc bbb aaa', got '%s'", r.String())
	}
}

func TestString_ReplaceRegexpWithGroups(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"2026-03-21".replace(/(\d{4})-(\d{2})-(\d{2})/, "$2/$3/$1")`)
	if r.String() != "03/21/2026" {
		t.Errorf("expected '03/21/2026', got '%s'", r.String())
	}
}

func TestString_ReplaceRegexpCaseInsensitive(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"Hello HELLO hello".replace(/hello/gi, "hi")`)
	if r.String() != "hi hi hi" {
		t.Errorf("expected 'hi hi hi', got '%s'", r.String())
	}
}

// ── String.search() ─────────────────────────────────────────────

func TestString_Search(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"hello 123 world".search(/\d+/)`)
	if r.Number() != 6 {
		t.Errorf("expected index 6, got %v", r.Number())
	}
}

func TestString_SearchNotFound(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"hello world".search(/\d+/)`)
	if r.Number() != -1 {
		t.Errorf("expected -1, got %v", r.Number())
	}
}

// ── String.split() with RegExp ──────────────────────────────────

func TestString_SplitRegexp(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"one1two2three".split(/\d/)`)
	arr := r.Array()
	if len(arr) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(arr))
	}
	if arr[0].String() != "one" {
		t.Errorf("expected 'one', got '%s'", arr[0].String())
	}
	if arr[1].String() != "two" {
		t.Errorf("expected 'two', got '%s'", arr[1].String())
	}
	if arr[2].String() != "three" {
		t.Errorf("expected 'three', got '%s'", arr[2].String())
	}
}

func TestString_SplitRegexpMultiChar(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`"a, b,  c ,d".split(/\s*,\s*/)`)
	arr := r.Array()
	if len(arr) != 4 {
		t.Fatalf("expected 4 parts, got %d", len(arr))
	}
	if arr[0].String() != "a" || arr[1].String() != "b" || arr[2].String() != "c" || arr[3].String() != "d" {
		t.Errorf("unexpected split result: %v", r.String())
	}
}

// ── RegExp not confused with division ───────────────────────────

func TestRegExp_DivisionNotConfused(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`10 / 2`)
	if r.Number() != 5 {
		t.Errorf("expected 5, got %v", r.Number())
	}
}

func TestRegExp_DivisionAfterVariable(t *testing.T) {
	vm := New()
	vm.Set("x", 10)
	r, _ := vm.Eval(`x / 2`)
	if r.Number() != 5 {
		t.Errorf("expected 5, got %v", r.Number())
	}
}

func TestRegExp_DivisionAfterParen(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`(10 + 2) / 3`)
	if r.Number() != 4 {
		t.Errorf("expected 4, got %v", r.Number())
	}
}

// ── RegExp with special patterns ────────────────────────────────

func TestRegExp_WordBoundary(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`/\bworld\b/.test("hello world foo")`)
	if !r.Bool() {
		t.Error("expected word boundary match to be true")
	}
}

func TestRegExp_DigitPattern(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`/^\d{3}-\d{4}$/.test("555-1234")`)
	if !r.Bool() {
		t.Error("expected phone pattern to match")
	}
}

func TestRegExp_EscapedSlash(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`/a\/b/.test("a/b")`)
	if !r.Bool() {
		t.Error("expected escaped slash pattern to match")
	}
}

// ── RegExp in variable assignment ───────────────────────────────

func TestRegExp_AssignToVariable(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`
		const pattern = /\d+/g;
		"abc 123 def 456".match(pattern)
	`)
	if r.IsNull() {
		t.Fatal("expected match result, got null")
	}
	arr := r.Array()
	if len(arr) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(arr))
	}
	if arr[0].String() != "123" || arr[1].String() != "456" {
		t.Error("unexpected match values")
	}
}

// ── Multiline flag ──────────────────────────────────────────────

func TestRegExp_MultilineFlag(t *testing.T) {
	vm := New()
	vm.Set("text", "line1\nline2\nline3")
	r, _ := vm.Eval(`text.match(/^line\d/gm)`)
	if r.IsNull() {
		t.Fatal("expected matches, got null")
	}
	arr := r.Array()
	if len(arr) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(arr))
	}
}

// ── Constructor with variables ──────────────────────────────────

func TestRegExp_ConstructorWithVariable(t *testing.T) {
	vm := New()
	r, _ := vm.Eval(`
		const word = "hello";
		const re = new RegExp(word, "i");
		re.test("Hello World")
	`)
	if !r.Bool() {
		t.Error("expected constructor with variable pattern to work")
	}
}
