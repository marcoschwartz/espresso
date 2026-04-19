// Test if chained var assignment updates module.exports correctly when accessed from outside
var x = exports = module.exports = { foo: 1 };
x.bar = 2;
x.baz = 3;
globalThis.__TESTMOD_X__ = x;
globalThis.__TESTMOD_MODEXP__ = module.exports;
