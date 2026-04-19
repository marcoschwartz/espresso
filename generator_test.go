package espresso

import "testing"

// ─── Generator Tests ────────────────────────────────────

func TestGenerator_Basic(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		function* gen() {
			yield 1;
			yield 2;
			yield 3;
		}
		const it = gen();
		const a = it.next();
		const b = it.next();
		const c = it.next();
		const d = it.next();
		return a.value + "," + b.value + "," + c.value + "," + String(d.done);
	`)
	if r.String() != "1,2,3,true" {
		t.Errorf("expected '1,2,3,true', got '%s'", r.String())
	}
}

func TestGenerator_Done(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		function* gen() {
			yield "a";
		}
		const it = gen();
		const first = it.next();
		const second = it.next();
		return String(first.done) + "," + String(second.done);
	`)
	if r.String() != "false,true" {
		t.Errorf("expected 'false,true', got '%s'", r.String())
	}
}

func TestGenerator_WithParams(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		function* range(start, end) {
			var i = start;
			yield i;
			i = i + 1;
			yield i;
			i = i + 1;
			yield i;
		}
		const it = range(10, 12);
		return it.next().value + "," + it.next().value + "," + it.next().value;
	`)
	if r.String() != "10,11,12" {
		t.Errorf("expected '10,11,12', got '%s'", r.String())
	}
}

func TestGenerator_YieldExpressions(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		function* gen() {
			yield 1 + 2;
			yield "hello" + " world";
		}
		const it = gen();
		return it.next().value + "," + it.next().value;
	`)
	if r.String() != "3,hello world" {
		t.Errorf("expected '3,hello world', got '%s'", r.String())
	}
}

func TestGenerator_Return(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		function* gen() {
			yield 1;
			yield 2;
			yield 3;
		}
		const it = gen();
		it.next();
		const ret = it.return("early");
		return ret.value + "," + String(ret.done);
	`)
	if r.String() != "early,true" {
		t.Errorf("expected 'early,true', got '%s'", r.String())
	}
}

func TestGenerator_ReturnStopsIteration(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		function* gen() {
			yield 1;
			yield 2;
		}
		const it = gen();
		it.next();
		it.return();
		const after = it.next();
		return String(after.done);
	`)
	if r.String() != "true" {
		t.Errorf("expected 'true', got '%s'", r.String())
	}
}

func TestGenerator_EmptyGenerator(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		function* gen() {}
		const it = gen();
		const result = it.next();
		return String(result.done);
	`)
	if r.String() != "true" {
		t.Errorf("expected 'true', got '%s'", r.String())
	}
}

func TestGenerator_SingleYield(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		function* gen() {
			yield 42;
		}
		const it = gen();
		const first = it.next();
		const second = it.next();
		return first.value + "," + String(first.done) + "," + String(second.done);
	`)
	if r.String() != "42,false,true" {
		t.Errorf("expected '42,false,true', got '%s'", r.String())
	}
}

func TestGenerator_Expression(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const gen = function*() {
			yield "a";
			yield "b";
		};
		const it = gen();
		return it.next().value + it.next().value;
	`)
	if r.String() != "ab" {
		t.Errorf("expected 'ab', got '%s'", r.String())
	}
}

func TestGenerator_StringYields(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		function* greetings() {
			yield "hello";
			yield "world";
			yield "bye";
		}
		const it = greetings();
		var result = "";
		var step = it.next();
		result = result + step.value;
		step = it.next();
		result = result + "," + step.value;
		step = it.next();
		result = result + "," + step.value;
		return result;
	`)
	if r.String() != "hello,world,bye" {
		t.Errorf("expected 'hello,world,bye', got '%s'", r.String())
	}
}

func TestGenerator_Throw(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		function* gen() {
			yield 1;
			yield 2;
		}
		const it = gen();
		it.next();
		const thrown = it.throw("error");
		return String(thrown.done);
	`)
	if r.String() != "true" {
		t.Errorf("expected 'true', got '%s'", r.String())
	}
}
