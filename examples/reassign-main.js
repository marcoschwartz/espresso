var m = require('./reassign-mod.js');
process.stdout.write("typeof m: " + typeof m + "\n");
process.stdout.write("keys: " + Object.getOwnPropertyNames(m).join(",") + "\n");
process.stdout.write("init typeof: " + typeof m.init + "\n");
process.stdout.write("greet typeof: " + typeof m.greet + "\n");
if (typeof m.init === "function") {
    process.stdout.write("init() = " + m.init() + "\n");
}
