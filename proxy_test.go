package espresso

import "testing"

// ─── Proxy Tests ────────────────────────────────────────

func TestProxy_GetTrap(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const target = { name: "world", age: 30 };
		const handler = {
			get: (obj, prop) => {
				if (prop === "name") {
					return "Hello, " + obj.name;
				}
				return obj[prop];
			}
		};
		const proxy = new Proxy(target, handler);
		return proxy.name;
	`)
	if r.String() != "Hello, world" {
		t.Errorf("expected 'Hello, world', got '%s'", r.String())
	}
}

func TestProxy_SetTrap(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const target = { value: 0 };
		const handler = {
			set: (obj, prop, val) => {
				obj[prop] = val * 2;
				return true;
			}
		};
		const proxy = new Proxy(target, handler);
		proxy.value = 5;
		return target.value;
	`)
	if r.Number() != 10 {
		t.Errorf("expected 10, got %v", r.Number())
	}
}

func TestProxy_NoTraps(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const target = { x: 42 };
		const proxy = new Proxy(target, {});
		return proxy.x;
	`)
	if r.Number() != 42 {
		t.Errorf("expected 42, got %v", r.Number())
	}
}

func TestProxy_GetTrapMultipleProps(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const target = { a: 1, b: 2 };
		const handler = {
			get: (obj, prop) => {
				return obj[prop] + 10;
			}
		};
		const proxy = new Proxy(target, handler);
		return proxy.a + proxy.b;
	`)
	if r.Number() != 23 {
		t.Errorf("expected 23, got %v", r.Number())
	}
}

func TestProxy_GetTrapDefault(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const target = { name: "test" };
		const handler = {
			get: (obj, prop) => {
				if (prop === "name") {
					return obj.name.toUpperCase();
				}
				return "default";
			}
		};
		const proxy = new Proxy(target, handler);
		return proxy.name;
	`)
	if r.String() != "TEST" {
		t.Errorf("expected 'TEST', got '%s'", r.String())
	}
}

func TestProxy_HasTrap(t *testing.T) {
	vm := New()
	vm.Run(`
		const target = { a: 1 };
		const handler = {
			has: (obj, prop) => {
				return false;
			}
		};
		const proxy = new Proxy(target, handler);
	`)

	// The has trap is stored as __has__ on the proxy
	proxy := vm.Get("proxy")
	hasFn := proxy.Get("__has__")
	if hasFn.IsUndefined() {
		t.Error("expected __has__ to be defined on proxy")
	}
}

func TestProxy_DeleteTrap(t *testing.T) {
	vm := New()
	vm.Run(`
		const target = { a: 1 };
		var deleted = false;
		const handler = {
			deleteProperty: (obj, prop) => {
				deleted = true;
				return true;
			}
		};
		const proxy = new Proxy(target, handler);
	`)

	proxy := vm.Get("proxy")
	deleteFn := proxy.Get("__delete__")
	if deleteFn.IsUndefined() {
		t.Error("expected __delete__ to be defined on proxy")
	}
}

func TestProxy_Logging(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		var log = "";
		const target = { x: 1, y: 2 };
		const handler = {
			get: (obj, prop) => {
				log = log + "get:" + prop + ";";
				return obj[prop];
			}
		};
		const proxy = new Proxy(target, handler);
		const a = proxy.x;
		const b = proxy.y;
		return log;
	`)
	if r.String() != "get:x;get:y;" {
		t.Errorf("expected 'get:x;get:y;', got '%s'", r.String())
	}
}

func TestProxy_ValidationSet(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const target = { age: 25 };
		var rejected = false;
		const handler = {
			set: (obj, prop, val) => {
				if (prop === "age" && val < 0) {
					rejected = true;
					return false;
				}
				obj[prop] = val;
				return true;
			}
		};
		const proxy = new Proxy(target, handler);
		proxy.age = -5;
		return String(rejected) + "," + target.age;
	`)
	if r.String() != "true,25" {
		t.Errorf("expected 'true,25', got '%s'", r.String())
	}
}

func TestProxy_GetTrapUnknownProp(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		const handler = {
			get: function(target, prop) {
				if (prop in target) return target[prop];
				return 42;
			}
		};
		const p = new Proxy({x: 1}, handler);
		return p.x + "," + p.y + "," + p.missing;
	`)
	if r.String() != "1,42,42" {
		t.Errorf("expected '1,42,42', got '%s'", r.String())
	}
}
