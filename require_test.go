package espresso

import (
	"os"
	"path/filepath"
	"testing"
)

// ─── Module System Tests ────────────────────────────────

func setupModuleDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0755)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRequire_BasicExports(t *testing.T) {
	dir := setupModuleDir(t)
	writeFile(t, filepath.Join(dir, "math.js"), `
		exports.add = (a, b) => a + b;
		exports.PI = 3.14159;
	`)

	vm := New()
	ms := NewModuleSystem(vm, dir)
	ms.RegisterGlobals(filepath.Join(dir, "main.js"))

	vm.Run(`
		const math = require("./math");
		var sum = math.add(2, 3);
		var pi = math.PI;
	`)

	if vm.Get("sum").Number() != 5 {
		t.Errorf("expected sum=5, got %v", vm.Get("sum").Number())
	}
	if vm.Get("pi").Number() != 3.14159 {
		t.Errorf("expected pi=3.14159, got %v", vm.Get("pi").Number())
	}
}

func TestRequire_ModuleExports(t *testing.T) {
	dir := setupModuleDir(t)
	writeFile(t, filepath.Join(dir, "greet.js"), `
		module.exports = (name) => "Hello, " + name + "!";
	`)

	vm := New()
	ms := NewModuleSystem(vm, dir)
	ms.RegisterGlobals(filepath.Join(dir, "main.js"))

	vm.Run(`
		const greet = require("./greet");
		var msg = greet("World");
	`)

	if vm.Get("msg").String() != "Hello, World!" {
		t.Errorf("expected 'Hello, World!', got '%s'", vm.Get("msg").String())
	}
}

func TestRequire_ModuleExportsObject(t *testing.T) {
	dir := setupModuleDir(t)
	writeFile(t, filepath.Join(dir, "config.js"), `
		module.exports = {
			host: "localhost",
			port: 8080
		};
	`)

	vm := New()
	ms := NewModuleSystem(vm, dir)
	ms.RegisterGlobals(filepath.Join(dir, "main.js"))

	vm.Run(`
		const config = require("./config");
		var host = config.host;
		var port = config.port;
	`)

	if vm.Get("host").String() != "localhost" {
		t.Errorf("expected 'localhost', got '%s'", vm.Get("host").String())
	}
	if vm.Get("port").Number() != 8080 {
		t.Errorf("expected 8080, got %v", vm.Get("port").Number())
	}
}

func TestRequire_JsExtensionResolution(t *testing.T) {
	dir := setupModuleDir(t)
	writeFile(t, filepath.Join(dir, "utils.js"), `
		exports.version = "1.0";
	`)

	vm := New()
	ms := NewModuleSystem(vm, dir)
	ms.RegisterGlobals(filepath.Join(dir, "main.js"))

	// Require without .js extension
	vm.Run(`
		const utils = require("./utils");
		var ver = utils.version;
	`)

	if vm.Get("ver").String() != "1.0" {
		t.Errorf("expected '1.0', got '%s'", vm.Get("ver").String())
	}
}

func TestRequire_IndexJs(t *testing.T) {
	dir := setupModuleDir(t)
	libDir := filepath.Join(dir, "mylib")
	writeFile(t, filepath.Join(libDir, "index.js"), `
		exports.name = "mylib";
		exports.version = "2.0";
	`)

	vm := New()
	ms := NewModuleSystem(vm, dir)
	ms.RegisterGlobals(filepath.Join(dir, "main.js"))

	vm.Run(`
		const lib = require("./mylib");
		var name = lib.name;
	`)

	if vm.Get("name").String() != "mylib" {
		t.Errorf("expected 'mylib', got '%s'", vm.Get("name").String())
	}
}

func TestRequire_Caching(t *testing.T) {
	dir := setupModuleDir(t)
	// Use object-based state which persists across calls (reference semantics)
	writeFile(t, filepath.Join(dir, "counter.js"), `
		const state = { count: 0 };
		exports.inc = () => { state.count = state.count + 1; return state.count; };
		exports.get = () => state.count;
	`)

	vm := New()
	ms := NewModuleSystem(vm, dir)
	ms.RegisterGlobals(filepath.Join(dir, "main.js"))

	// Verify caching: require same module twice returns same object
	result1, _ := ms.Require("./counter")
	result2, _ := ms.Require("./counter")

	if result1 != result2 {
		t.Error("require should return cached module (same reference)")
	}
}

func TestRequire_NestedRequires(t *testing.T) {
	dir := setupModuleDir(t)
	writeFile(t, filepath.Join(dir, "a.js"), `
		const b = require("./b");
		exports.result = "a+" + b.name;
	`)
	writeFile(t, filepath.Join(dir, "b.js"), `
		exports.name = "b";
	`)

	vm := New()
	ms := NewModuleSystem(vm, dir)
	ms.RegisterGlobals(filepath.Join(dir, "main.js"))

	vm.Run(`
		const a = require("./a");
		var result = a.result;
	`)

	if vm.Get("result").String() != "a+b" {
		t.Errorf("expected 'a+b', got '%s'", vm.Get("result").String())
	}
}

func TestRequire_SubdirectoryModules(t *testing.T) {
	dir := setupModuleDir(t)
	writeFile(t, filepath.Join(dir, "lib", "helper.js"), `
		exports.help = () => "helped";
	`)

	vm := New()
	ms := NewModuleSystem(vm, dir)
	ms.RegisterGlobals(filepath.Join(dir, "main.js"))

	vm.Run(`
		const helper = require("./lib/helper");
		var r = helper.help();
	`)

	if vm.Get("r").String() != "helped" {
		t.Errorf("expected 'helped', got '%s'", vm.Get("r").String())
	}
}

func TestRequire_RelativeFromSubdir(t *testing.T) {
	dir := setupModuleDir(t)
	writeFile(t, filepath.Join(dir, "lib", "a.js"), `
		const b = require("./b");
		exports.msg = b.msg;
	`)
	writeFile(t, filepath.Join(dir, "lib", "b.js"), `
		exports.msg = "from b";
	`)

	vm := New()
	ms := NewModuleSystem(vm, dir)
	ms.RegisterGlobals(filepath.Join(dir, "main.js"))

	vm.Run(`
		const a = require("./lib/a");
		var msg = a.msg;
	`)

	if vm.Get("msg").String() != "from b" {
		t.Errorf("expected 'from b', got '%s'", vm.Get("msg").String())
	}
}

func TestRequire_ParentDirectory(t *testing.T) {
	dir := setupModuleDir(t)
	writeFile(t, filepath.Join(dir, "shared.js"), `
		exports.value = 42;
	`)
	writeFile(t, filepath.Join(dir, "sub", "child.js"), `
		const shared = require("../shared");
		exports.val = shared.value;
	`)

	vm := New()
	ms := NewModuleSystem(vm, dir)
	ms.RegisterGlobals(filepath.Join(dir, "main.js"))

	vm.Run(`
		const child = require("./sub/child");
		var val = child.val;
	`)

	if vm.Get("val").Number() != 42 {
		t.Errorf("expected 42, got %v", vm.Get("val").Number())
	}
}

func TestRequire_ModuleExportsClass(t *testing.T) {
	dir := setupModuleDir(t)
	writeFile(t, filepath.Join(dir, "animal.js"), `
		class Animal {
			constructor(name) {
				this.name = name;
			}
			speak() {
				return this.name + " speaks";
			}
		}
		module.exports = Animal;
	`)

	vm := New()
	ms := NewModuleSystem(vm, dir)
	ms.RegisterGlobals(filepath.Join(dir, "main.js"))

	vm.Run(`
		const Animal = require("./animal");
		const dog = new Animal("Rex");
		var speech = dog.speak();
	`)

	if vm.Get("speech").String() != "Rex speaks" {
		t.Errorf("expected 'Rex speaks', got '%s'", vm.Get("speech").String())
	}
}

func TestRequire_ModuleNotFound(t *testing.T) {
	dir := setupModuleDir(t)

	vm := New()
	ms := NewModuleSystem(vm, dir)
	ms.RegisterGlobals(filepath.Join(dir, "main.js"))

	vm.Run(`
		const mod = require("./nonexistent");
		var hasError = mod.name === "Error";
	`)

	if !vm.Get("hasError").Truthy() {
		t.Error("expected error object for missing module")
	}
}

func TestRequire_DirnamFilename(t *testing.T) {
	dir := setupModuleDir(t)
	mainFile := filepath.Join(dir, "main.js")

	vm := New()
	ms := NewModuleSystem(vm, dir)
	ms.RegisterGlobals(mainFile)

	absMain, _ := filepath.Abs(mainFile)
	absDir := filepath.Dir(absMain)

	fn := vm.Get("__filename").String()
	dn := vm.Get("__dirname").String()

	if fn != absMain {
		t.Errorf("__filename: expected %s, got %s", absMain, fn)
	}
	if dn != absDir {
		t.Errorf("__dirname: expected %s, got %s", absDir, dn)
	}
}

func TestRequire_PackageJson(t *testing.T) {
	dir := setupModuleDir(t)
	pkgDir := filepath.Join(dir, "node_modules", "mypkg")
	writeFile(t, filepath.Join(pkgDir, "package.json"), `{
		"name": "mypkg",
		"main": "lib/main.js"
	}`)
	writeFile(t, filepath.Join(pkgDir, "lib", "main.js"), `
		exports.hello = "from mypkg";
	`)

	vm := New()
	ms := NewModuleSystem(vm, dir)
	ms.RegisterGlobals(filepath.Join(dir, "main.js"))

	vm.Run(`
		const pkg = require("mypkg");
		var hello = pkg.hello;
	`)

	if vm.Get("hello").String() != "from mypkg" {
		t.Errorf("expected 'from mypkg', got '%s'", vm.Get("hello").String())
	}
}

func TestRequire_NodeModulesIndex(t *testing.T) {
	dir := setupModuleDir(t)
	pkgDir := filepath.Join(dir, "node_modules", "simple")
	writeFile(t, filepath.Join(pkgDir, "index.js"), `
		exports.works = true;
	`)

	vm := New()
	ms := NewModuleSystem(vm, dir)
	ms.RegisterGlobals(filepath.Join(dir, "main.js"))

	vm.Run(`
		const simple = require("simple");
		var works = simple.works;
	`)

	if !vm.Get("works").Truthy() {
		t.Error("expected require('simple') to find node_modules/simple/index.js")
	}
}

func TestRequire_IsolatedScopes(t *testing.T) {
	dir := setupModuleDir(t)
	writeFile(t, filepath.Join(dir, "mod.js"), `
		var secret = "hidden";
		exports.getSecret = () => secret;
	`)

	vm := New()
	ms := NewModuleSystem(vm, dir)
	ms.RegisterGlobals(filepath.Join(dir, "main.js"))

	vm.Run(`
		const mod = require("./mod");
		var s = mod.getSecret();
		var leaked = typeof secret;
	`)

	if vm.Get("s").String() != "hidden" {
		t.Errorf("expected 'hidden', got '%s'", vm.Get("s").String())
	}
	if vm.Get("leaked").String() != "undefined" {
		t.Errorf("module internal var should not leak, got type '%s'", vm.Get("leaked").String())
	}
}

func TestRequire_JSON(t *testing.T) {
	dir := setupModuleDir(t)
	writeFile(t, filepath.Join(dir, "data.json"), `{"name":"test","version":"1.0"}`)

	vm := New()
	ms := NewModuleSystem(vm, dir)
	ms.RegisterGlobals(filepath.Join(dir, "main.js"))

	vm.Run(`
		const data = require("./data.json");
		var name = data.name;
		var ver = data.version;
	`)

	if vm.Get("name").String() != "test" {
		t.Errorf("expected 'test', got '%s'", vm.Get("name").String())
	}
	if vm.Get("ver").String() != "1.0" {
		t.Errorf("expected '1.0', got '%s'", vm.Get("ver").String())
	}
}
