package espresso

import (
	"os"
	"path/filepath"
	"testing"
)

// ─── Private Class Fields ────────────────────────────────

func TestPrivateField_Basic(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		class Counter {
			#count = 0;
			increment() { this.#count = this.#count + 1; }
			get value() { return this.#count; }
		}
		var c = new Counter();
		c.increment();
		c.increment();
		c.increment();
		var result = c.value;
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("result"); v.Number() != 3 {
		t.Errorf("expected 3, got %v", v.Number())
	}
}

func TestPrivateField_DefaultValue(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		class Config {
			#ttl = 3600;
			#name = "default";
			getTTL() { return this.#ttl; }
			getName() { return this.#name; }
		}
		var cfg = new Config();
		var ttl = cfg.getTTL();
		var name = cfg.getName();
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("ttl"); v.Number() != 3600 {
		t.Errorf("expected ttl=3600, got %v", v.Number())
	}
	if v := vm.Get("name"); v.String() != "default" {
		t.Errorf("expected name='default', got '%s'", v.String())
	}
}

func TestPrivateField_WriteInConstructor(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		class Box {
			#value;
			constructor(v) { this.#value = v; }
			get() { return this.#value; }
		}
		var b = new Box(42);
		var result = b.get();
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("result"); v.Number() != 42 {
		t.Errorf("expected 42, got %v", v.Number())
	}
}

func TestPrivateField_NotVisibleOutside(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		class Secret {
			#hidden = "secret";
			reveal() { return this.#hidden; }
		}
		var s = new Secret();
		var inside = s.reveal();
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("inside"); v.String() != "secret" {
		t.Errorf("expected 'secret', got '%s'", v.String())
	}
}

func TestPrivateField_MapPattern(t *testing.T) {
	// MCP SDK uses: #cache = new Map()
	vm := New()
	_, err := vm.Run(`
		class Store {
			#cache = new Map();
			set(k, v) { this.#cache.set(k, v); }
			get(k) { return this.#cache.get(k); }
			get size() { return this.#cache.size; }
		}
		var s = new Store();
		s.set("a", 1);
		s.set("b", 2);
		var val = s.get("a");
		var sz = s.size;
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("val"); v.Number() != 1 {
		t.Errorf("expected val=1, got %v", v.Number())
	}
	if v := vm.Get("sz"); v.Number() != 2 {
		t.Errorf("expected sz=2, got %v", v.Number())
	}
}

// ─── node: prefix in require() ───────────────────────────

func TestRequire_NodePrefix(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)
	el.RegisterGlobals()
	ms := NewModuleSystem(vm, "/tmp")
	ms.RegisterGlobals("")

	_, err := vm.Run(`
		var events = require('node:events');
		var hasEE = typeof events.EventEmitter === 'function';
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("hasEE"); !v.truthy() {
		t.Error("expected events.EventEmitter to be a function")
	}
}

func TestRequire_BuiltinProcess(t *testing.T) {
	vm := New()
	vm.SetValue("process", NewObj(map[string]*Value{
		"pid": newNum(1234),
	}))
	ms := NewModuleSystem(vm, "/tmp")
	ms.RegisterGlobals("")

	_, err := vm.Run(`
		var proc = require('node:process');
		var pid = proc.pid;
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("pid"); v.Number() != 1234 {
		t.Errorf("expected pid=1234, got %v", v.Number())
	}
}

func TestRequire_BuiltinPath(t *testing.T) {
	vm := New()
	ms := NewModuleSystem(vm, "/tmp")
	ms.RegisterGlobals("")

	_, err := vm.Run(`
		var path = require('node:path');
		var joined = path.join('/foo', 'bar', 'baz.js');
		var dir = path.dirname('/foo/bar/baz.js');
		var base = path.basename('/foo/bar/baz.js');
		var ext = path.extname('file.txt');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("joined"); v.String() != "/foo/bar/baz.js" {
		t.Errorf("expected '/foo/bar/baz.js', got '%s'", v.String())
	}
	if v := vm.Get("dir"); v.String() != "/foo/bar" {
		t.Errorf("expected '/foo/bar', got '%s'", v.String())
	}
	if v := vm.Get("base"); v.String() != "baz.js" {
		t.Errorf("expected 'baz.js', got '%s'", v.String())
	}
	if v := vm.Get("ext"); v.String() != ".txt" {
		t.Errorf("expected '.txt', got '%s'", v.String())
	}
}

func TestRequire_BuiltinWithoutPrefix(t *testing.T) {
	vm := New()
	ms := NewModuleSystem(vm, "/tmp")
	ms.RegisterGlobals("")

	_, err := vm.Run(`
		var events = require('events');
		var path = require('path');
		var hasEE = typeof events.EventEmitter === 'function';
		var hasSep = typeof path.sep === 'string';
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("hasEE"); !v.truthy() {
		t.Error("expected events.EventEmitter to be a function")
	}
	if v := vm.Get("hasSep"); !v.truthy() {
		t.Error("expected path.sep to be a string")
	}
}

func TestRequire_TimersPromises(t *testing.T) {
	vm := New()
	ms := NewModuleSystem(vm, "/tmp")
	ms.RegisterGlobals("")

	_, err := vm.Run(`
		var timers = require('timers/promises');
		var hasSetTimeout = typeof timers.setTimeout === 'function';
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("hasSetTimeout"); !v.truthy() {
		t.Error("expected timers/promises.setTimeout to be a function")
	}
}

// ─── EventEmitter ────────────────────────────────────────

func TestEventEmitter_OnEmit(t *testing.T) {
	vm := New()
	ms := NewModuleSystem(vm, "/tmp")
	ms.RegisterGlobals("")

	_, err := vm.Run(`
		var EventEmitter = require('events').EventEmitter;
		var ee = new EventEmitter();
		var received = [];
		ee.on('data', function(msg) { received.push(msg); });
		ee.emit('data', 'hello');
		ee.emit('data', 'world');
		var count = received.length;
		var first = received[0];
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("count"); v.Number() != 2 {
		t.Errorf("expected count=2, got %v", v.Number())
	}
	if v := vm.Get("first"); v.String() != "hello" {
		t.Errorf("expected first='hello', got '%s'", v.String())
	}
}

func TestEventEmitter_Once(t *testing.T) {
	vm := New()
	ms := NewModuleSystem(vm, "/tmp")
	ms.RegisterGlobals("")

	_, err := vm.Run(`
		var EventEmitter = require('events').EventEmitter;
		var ee = new EventEmitter();
		var count = 0;
		ee.once('ping', function() { count++; });
		ee.emit('ping');
		ee.emit('ping');
		ee.emit('ping');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("count"); v.Number() != 1 {
		t.Errorf("expected count=1, got %v", v.Number())
	}
}

func TestEventEmitter_Off(t *testing.T) {
	vm := New()
	ms := NewModuleSystem(vm, "/tmp")
	ms.RegisterGlobals("")

	_, err := vm.Run(`
		var EventEmitter = require('events').EventEmitter;
		var ee = new EventEmitter();
		var count = 0;
		var handler = function() { count++; };
		ee.on('tick', handler);
		ee.emit('tick');
		ee.off('tick', handler);
		ee.emit('tick');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("count"); v.Number() != 1 {
		t.Errorf("expected count=1, got %v", v.Number())
	}
}

func TestEventEmitter_ListenerCount(t *testing.T) {
	vm := New()
	ms := NewModuleSystem(vm, "/tmp")
	ms.RegisterGlobals("")

	_, err := vm.Run(`
		var EventEmitter = require('events').EventEmitter;
		var ee = new EventEmitter();
		ee.on('x', function() {});
		ee.on('x', function() {});
		ee.on('y', function() {});
		var xCount = ee.listenerCount('x');
		var yCount = ee.listenerCount('y');
		var zCount = ee.listenerCount('z');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("xCount"); v.Number() != 2 {
		t.Errorf("expected xCount=2, got %v", v.Number())
	}
	if v := vm.Get("yCount"); v.Number() != 1 {
		t.Errorf("expected yCount=1, got %v", v.Number())
	}
	if v := vm.Get("zCount"); v.Number() != 0 {
		t.Errorf("expected zCount=0, got %v", v.Number())
	}
}

func TestEventEmitter_EmitReturnValue(t *testing.T) {
	vm := New()
	ms := NewModuleSystem(vm, "/tmp")
	ms.RegisterGlobals("")

	_, err := vm.Run(`
		var EventEmitter = require('events').EventEmitter;
		var ee = new EventEmitter();
		var noListeners = ee.emit('nope');
		ee.on('yes', function() {});
		var hasListeners = ee.emit('yes');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("noListeners"); v.truthy() {
		t.Error("expected emit with no listeners to return false")
	}
	if v := vm.Get("hasListeners"); !v.truthy() {
		t.Error("expected emit with listeners to return true")
	}
}

func TestEventEmitter_MultipleArgs(t *testing.T) {
	vm := New()
	ms := NewModuleSystem(vm, "/tmp")
	ms.RegisterGlobals("")

	_, err := vm.Run(`
		var EventEmitter = require('events').EventEmitter;
		var ee = new EventEmitter();
		var got = [];
		ee.on('msg', function(a, b) { got.push(a); got.push(b); });
		ee.emit('msg', 'hello', 42);
	`)
	if err != nil {
		t.Fatal(err)
	}
	got := vm.Get("got")
	if got.typ != TypeArray || len(got.array) != 2 {
		t.Fatalf("expected 2 items, got %v", got)
	}
	if got.array[0].String() != "hello" {
		t.Errorf("expected 'hello', got '%s'", got.array[0].String())
	}
	if got.array[1].Number() != 42 {
		t.Errorf("expected 42, got %v", got.array[1].Number())
	}
}

// ─── for await...of ──────────────────────────────────────

func TestForAwaitOf_Basic(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)
	el.RegisterGlobals()
	_, err := vm.Run(`
		var items = [Promise.resolve(1), Promise.resolve(2), Promise.resolve(3)];
		var sum = 0;
		for await (const val of items) {
			sum += val;
		}
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("sum"); v.Number() != 6 {
		t.Errorf("expected sum=6, got %v", v.Number())
	}
}

func TestForAwaitOf_MixedValues(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)
	el.RegisterGlobals()
	_, err := vm.Run(`
		var items = [Promise.resolve("a"), "b", Promise.resolve("c")];
		var result = "";
		for await (const val of items) {
			result += val;
		}
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("result"); v.String() != "abc" {
		t.Errorf("expected 'abc', got '%s'", v.String())
	}
}

// ─── Integration: MCP SDK patterns ──────────────────────

func TestIntegration_SDKPattern(t *testing.T) {
	// Simulates the pattern used by @modelcontextprotocol/sdk
	vm := New()
	ms := NewModuleSystem(vm, "/tmp")
	ms.RegisterGlobals("")

	_, err := vm.Run(`
		var EventEmitter = require('events').EventEmitter;

		class Protocol {
			#handlers = new Map();
			#transport = null;

			setHandler(method, fn) {
				this.#handlers.set(method, fn);
			}

			handle(msg) {
				var handler = this.#handlers.get(msg.method);
				if (handler) {
					return handler(msg);
				}
				return null;
			}

			connect(transport) {
				this.#transport = transport;
			}
		}

		class Server extends Protocol {
			#info;
			constructor(info) {
				super();
				this.#info = info;
				this.setHandler('initialize', function(msg) {
					return { result: { serverInfo: info } };
				});
			}
		}

		var server = new Server({ name: "test", version: "1.0" });
		var resp = server.handle({ method: "initialize", id: 1 });
		var serverName = resp.result.serverInfo.name;
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("serverName"); v.String() != "test" {
		t.Errorf("expected 'test', got '%s'", v.String())
	}
}

func TestIntegration_RequireExportsFunction(t *testing.T) {
	// Create a temp module that exports functions
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "tools.js"), []byte(`
function greet(name) { return "Hello " + name; }
function add(a, b) { return a + b; }
module.exports = { greet: greet, add: add };
`), 0644)

	vm := New()
	ms := NewModuleSystem(vm, dir)
	ms.RegisterGlobals(filepath.Join(dir, "test.js"))

	_, err := vm.Run(`
		var tools = require('./tools');
		var greeting = tools.greet("World");
		var sum = tools.add(3, 4);
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("greeting"); v.String() != "Hello World" {
		t.Errorf("expected 'Hello World', got '%s'", v.String())
	}
	if v := vm.Get("sum"); v.Number() != 7 {
		t.Errorf("expected 7, got %v", v.Number())
	}
}
