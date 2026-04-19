// Espresso Node.js Compatibility Diagnostic
// Usage: ./espresso diagnose.js [module-to-test]
//
// Each test shows EXACTLY what was expected vs what was returned,
// so you can pinpoint the engine bug without further debugging.

var passed = 0;
var failed = 0;
var results = [];
var targetModule = process.argv[2] || null;

function test(cat, name, fn) {
    try {
        var r = fn();
        if (r === true) { passed++; }
        else { failed++; results.push({ cat: cat, name: name, detail: String(r) }); }
    } catch (e) {
        failed++; results.push({ cat: cat, name: name, detail: "THREW: " + String(e.message || e).substring(0, 200) });
    }
}

// Helper: returns descriptive string on failure instead of false
function eq(actual, expected, label) {
    if (actual === expected) return true;
    return (label || "") + " expected " + JSON.stringify(expected) + " got " + JSON.stringify(actual) + " (type: " + typeof actual + ")";
}

// ═══ CORE LANGUAGE ═══

test("lang", "template literals", function() { var x = 42; return eq(`val=${x}`, "val=42"); });
test("lang", "tagged templates fn`x${1}y`", function() {
    function tag(s, v) { return s[0] + v + s[1]; }
    var r = tag`a${1}b`;
    return eq(r, "a1b");
});
test("lang", "destructuring {a,b} = obj", function() { var { a, b } = { a: 1, b: 2 }; return eq(a, 1) === true && eq(b, 2); });
test("lang", "destructuring [a,b] = arr", function() { var [a, b] = [1, 2]; return eq(a, 1) === true && eq(b, 2); });
test("lang", "array rest [a, ...r] = arr", function() {
    var [a, ...r] = [1,2,3];
    if (a !== 1) return "a=" + a;
    if (!Array.isArray(r)) return "rest not array: " + typeof r;
    return eq(r.length, 2, "rest.length");
});
test("lang", "spread [...a, x]", function() { var a = [1,2]; var b = [...a,3]; return eq(b.length, 3) === true && eq(b[2], 3); });
test("lang", "spread fn(...args)", function() { function f(a,b) { return a+b; } return eq(f(...[3,4]), 7); });
test("lang", "nullish ??", function() { var x = null; return eq(x ?? 5, 5); });
test("lang", "optional ?.", function() { var o = {a:{b:1}}; return eq(o?.a?.b, 1) === true && eq(o?.c?.d, undefined); });
test("lang", "hex 0xff", function() { return eq(0xff, 255) === true && eq(0xDEAD, 57005); });
test("lang", "bitwise", function() {
    if ((0xf0 & 0x0f) !== 0) return "& failed: " + (0xf0 & 0x0f);
    if ((5 | 3) !== 7) return "| failed: " + (5 | 3);
    if ((1 << 4) !== 16) return "<< failed: " + (1 << 4);
    return true;
});
test("lang", "for...in", function() {
    var o = {a:1,b:2}; var keys = [];
    for (var p in o) keys.push(p);
    if (keys.length === 0) return "no iterations, p=" + typeof p;
    if (keys.indexOf("a") < 0) return "missing 'a', keys=" + JSON.stringify(keys);
    return eq(keys.length, 2, "key count");
});
test("lang", "for...of array", function() {
    var arr = [10,20,30]; var sum = 0;
    for (var x of arr) { sum += x; }
    return eq(sum, 60, "sum");
});
test("lang", "for...of Map", function() {
    var m = new Map(); m.set("a",1); m.set("b",2);
    var keys = [];
    for (var e of m) { keys.push(e[0]); }
    if (keys.length === 0) return "no iterations";
    return eq(keys.length, 2, "entry count");
});
test("lang", "for await...of", function() {
    var items = [Promise.resolve(10), Promise.resolve(20)]; var sum = 0;
    for await (var v of items) { sum += v; }
    return eq(sum, 30, "sum");
});
test("lang", "try/catch/finally", function() {
    var r = "";
    try { throw new Error("x"); } catch(e) { r += "c"; } finally { r += "f"; }
    return eq(r, "cf");
});
test("lang", "switch/case", function() {
    var r; switch("b") { case "a": r=1; break; case "b": r=2; break; default: r=3; }
    return eq(r, 2);
});

// ═══ OBJECT BUILTINS ═══

test("obj", "Object.keys", function() { return eq(Object.keys({a:1,b:2}).length, 2); });
test("obj", "Object.entries", function() { var e = Object.entries({a:1}); return eq(e.length, 1) === true && eq(e[0][0], "a"); });
test("obj", "Object.fromEntries", function() { return eq(Object.fromEntries([["a",1]]).a, 1); });
test("obj", "Object.assign", function() { var o = Object.assign({},{a:1},{b:2}); return eq(o.a, 1) === true && eq(o.b, 2); });

test("obj", "Object.defineProperty getter", function() {
    var o = {};
    Object.defineProperty(o, "x", { get: function() { return 42; } });
    return eq(o.x, 42, "getter");
});
test("obj", "Object.defineProperty value", function() {
    var o = {};
    Object.defineProperty(o, "x", { value: 99 });
    return eq(o.x, 99, "value");
});
test("obj", "Object.defineProperty value=true", function() {
    var o = {};
    Object.defineProperty(o, "__esModule", { value: true });
    return eq(o.__esModule, true);
});
test("obj", "Object.getOwnPropertyDescriptor", function() {
    var o = {x:1};
    var d = Object.getOwnPropertyDescriptor(o, "x");
    if (d === null || d === undefined) return "returned " + typeof d;
    if (typeof d !== "object") return "not object: " + typeof d;
    return true;
});
test("obj", "Object.create", function() {
    var p = {greet: function() { return "hi"; }};
    var o = Object.create(p);
    if (typeof o.greet !== "function") return "greet not found: " + typeof o.greet;
    return eq(o.greet(), "hi");
});
test("obj", "hasOwnProperty.call", function() {
    var o = {a:1};
    var result = Object.prototype.hasOwnProperty.call(o, "a");
    return eq(result, true, "hasOwn");
});

// ═══ ARRAYS ═══

test("arr", "map", function() { return eq([1,2,3].map(x=>x*2).join(","), "2,4,6"); });
test("arr", "filter", function() { return eq([1,2,3].filter(x=>x>1).length, 2); });
test("arr", "find arrow", function() { return eq([1,2,3].find(x=>x>1), 2); });
test("arr", "find function()", function() { return eq([1,2,3].find(function(x){return x>1;}), 2); });
test("arr", "reduce", function() { return eq([1,2,3].reduce((a,b)=>a+b,0), 6); });
test("arr", "includes", function() { return eq([1,2,3].includes(2), true); });
test("arr", "flat", function() { return eq([[1],[2,3]].flat().length, 3); });
test("arr", "Array.isArray", function() { return eq(Array.isArray([]), true) === true && eq(Array.isArray("x"), false); });
test("arr", "Array.from", function() { var a = Array.from("abc"); return eq(a.length, 3) === true && eq(a[0], "a"); });

// ═══ STRINGS ═══

test("str", "includes/startsWith/endsWith", function() {
    return "hello".includes("ell") && "hello".startsWith("he") && "hello".endsWith("lo") || "method returned false";
});
test("str", "trim/trimStart/trimEnd", function() {
    return eq("  x  ".trim(), "x") === true && eq("  x  ".trimStart(), "x  ") === true && eq("  x  ".trimEnd(), "  x");
});
test("str", "split/join", function() { return eq("a,b".split(",").join("-"), "a-b"); });
test("str", "padStart", function() { return eq("5".padStart(3,"0"), "005"); });
test("str", "indexOf", function() { return eq("abcabc".indexOf("bc"), 1); });

// ═══ CLASSES ═══

test("class", "basic", function() { class F { constructor(x) { this.x=x; } } return eq(new F(1).x, 1); });
test("class", "extends", function() {
    class A { a() { return 1; } } class B extends A { b() { return 2; } }
    var b = new B();
    if (typeof b.a !== "function") return "inherited method missing: typeof b.a = " + typeof b.a;
    return eq(b.a(), 1) === true && eq(b.b(), 2);
});
test("class", "extends dotted parent", function() {
    var m = {}; class A { foo() { return "a"; } } m.A = A;
    class B extends m.A {}
    var b = new B();
    if (typeof b.foo !== "function") return "method missing on dotted-parent child";
    return eq(b.foo(), "a");
});
test("class", "super()", function() {
    class A { constructor(x) { this.x=x; } }
    class B extends A { constructor(x) { super(x); this.y=x*2; } }
    var b = new B(5);
    return eq(b.x, 5, "x") === true && eq(b.y, 10, "y");
});
test("class", "static", function() { class F { static make() { return new F(); } constructor() { this.ok=true; } } return eq(F.make().ok, true); });
test("class", "getter", function() { class F { get v() { return 42; } } return eq(new F().v, 42); });
test("class", "private #field", function() { class F { #x=1; get() { return this.#x; } } return eq(new F().get(), 1); });
test("class", "async method", function() { class F { async run() { return 1; } } return eq(typeof new F().run, "function"); });
test("class", "method destructured param", function() {
    var o = { fn({ a, b }) { return a + b; } };
    return eq(o.fn({a:3,b:4}), 7);
});

// ═══ CONSTRUCTOR PATTERNS ═══

test("new", "new Function()", function() { function F(x) { this.x=x; } return eq(new F(1).x, 1); });
test("new", "new returns object", function() { function F(x) { return {x:x}; } return eq(new F(1).x, 1); });
test("new", "new via param", function() { function mk(C,v) { return new C(v); } function F(x) { this.x=x; } return eq(mk(F,1).x, 1); });
test("new", "new dotted m.F()", function() { var m={}; m.F=function(x){this.x=x;}; return eq(new m.F(1).x, 1); });
test("new", "new on closure", function() {
    function outer() { return function(x) { this.x=x; return this; }; }
    var C = outer();
    var o = new C(1);
    if (typeof o !== "object") return "not object: " + typeof o;
    return eq(o.x, 1);
});
test("new", "Zod $constructor pattern", function() {
    function $ctor(name, init) {
        function _(def) {
            var inst = this && typeof this === "object" ? this : {};
            inst._type = name;
            init(inst, def);
            return inst;
        }
        return _;
    }
    var Cls = $ctor("Str", function(inst, def) { inst.kind = def.kind; });
    var o = new Cls({ kind: "string" });
    if (typeof o !== "object" || o === null) return "new returned: " + typeof o;
    return eq(o._type, "Str") === true && eq(o.kind, "string");
});

// ═══ PROMISES ═══

test("async", "Promise.resolve.then", function() {
    var v = "unset";
    Promise.resolve(42).then(function(r) { v = r; });
    return eq(v, 42);
});
test("async", "new Promise", function() {
    var v = "unset";
    new Promise(function(resolve) { resolve(99); }).then(function(r) { v = r; });
    return eq(v, 99);
});
test("async", "async/await", function() {
    var v = "unset";
    async function f() { return 42; }
    async function g() { v = await f(); }
    g();
    return eq(v, 42);
});

// ═══ COLLECTIONS ═══

test("coll", "Map", function() { var m = new Map(); m.set("a",1); return eq(m.get("a"), 1) === true && eq(m.size, 1); });
test("coll", "Set", function() { var s = new Set([1,2,3]); return eq(s.size, 3) === true && eq(s.has(2), true); });

// ═══ BUFFER ═══

test("buf", "from+toString", function() { return eq(Buffer.from("hi").toString(), "hi"); });
test("buf", "base64", function() { return eq(Buffer.from("hi").toString("base64"), "aGk="); });
test("buf", "concat", function() { return eq(Buffer.concat([Buffer.from("a"),Buffer.from("b")]).toString(), "ab"); });
test("buf", "indexOf", function() { return eq(Buffer.from("a\nb").indexOf("\n"), 1); });
test("buf", "subarray", function() { return eq(Buffer.from("hello").subarray(0,3).toString(), "hel"); });
test("buf", "toString(enc,start,end)", function() { return eq(Buffer.from("hello").toString("utf8",1,4), "ell"); });

// ═══ NODE BUILTINS ═══

test("node", "require('events')", function() { return eq(typeof require('events').EventEmitter, "function"); });
test("node", "EventEmitter on/emit", function() {
    var EE=require('events').EventEmitter; var ee=new EE(); var v;
    ee.on("x",function(d){v=d;}); ee.emit("x",42);
    return eq(v, 42);
});
test("node", "require('path').join", function() { return eq(require('path').join("/a","b"), "/a/b"); });
test("node", "require('node:process')", function() { return eq(typeof require('node:process'), "object"); });
test("node", "process.stdin.on", function() { return eq(typeof process.stdin.on, "function"); });
test("node", "setTimeout defined", function() { return eq(typeof setTimeout, "function"); });

// ═══ CJS PATTERNS ═══

test("cjs", "exports.X = fn", function() { var e = {}; e.f = function(){return 1;}; return eq(e.f(), 1); });
test("cjs", "Object.defineProperty(exports,'__esModule',{value:true})", function() {
    var e = {};
    Object.defineProperty(e, "__esModule", { value: true });
    return eq(e.__esModule, true, "__esModule");
});
test("cjs", "__exportStar for-in + hasOwn", function() {
    var src = {a:1,b:2}; var tgt = {};
    for (var p in src) {
        if (p !== "default" && !Object.prototype.hasOwnProperty.call(tgt, p)) tgt[p] = src[p];
    }
    return eq(tgt.a, 1, "a") === true && eq(tgt.b, 2, "b");
});

// ═══ TARGET MODULE ═══

if (targetModule) {
    test("mod", "require('" + targetModule + "')", function() {
        var m = require(targetModule);
        if (m === null || m === undefined) return "returned " + typeof m;
        var keys = Object.keys(m);
        process.stderr.write("    => " + keys.length + " exports: " + keys.join(", ").substring(0,300) + "\n");
        return true;
    });
}

// ═══ REPORT ═══

process.stderr.write("\n============================\n");
process.stderr.write("Espresso Compatibility Diagnostic\n");
if (targetModule) process.stderr.write("Target: " + targetModule + "\n");
process.stderr.write("============================\n\n");
process.stderr.write("PASSED: " + passed + " / " + (passed + failed) + "\n");
process.stderr.write("FAILED: " + failed + "\n");

if (results.length > 0) {
    process.stderr.write("\n--- FAILURES ---\n");
    var lastCat = "";
    for (var i = 0; i < results.length; i++) {
        var r = results[i];
        if (r.cat !== lastCat) { process.stderr.write("\n[" + r.cat + "]\n"); lastCat = r.cat; }
        process.stderr.write("  " + r.name + "\n");
        process.stderr.write("    => " + r.detail + "\n");
    }
}
process.stderr.write("\n");
