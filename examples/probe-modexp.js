// Does module.exports = X update module?
var orig = module.exports;
process.stderr.write("orig === module.exports: " + (orig === module.exports) + "\n");
var NEW = { marker: "new-obj" };
module.exports = NEW;
process.stderr.write("module.exports === NEW: " + (module.exports === NEW) + "\n");
process.stderr.write("module.exports === orig: " + (module.exports === orig) + "\n");
// Store to global for external check
globalThis.__LAST_MOD__ = module;
globalThis.__NEW__ = NEW;
