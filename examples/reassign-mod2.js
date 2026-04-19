// Match the exact pattern of application.js
var app = exports = module.exports = {};
app.a = 1;
app.b = 2;
app.c = 3;
process.stderr.write("[inside] app.a=" + app.a + " module.exports.a=" + module.exports.a + " same=" + (app === module.exports) + "\n");
process.stderr.write("[inside] app keys: " + Object.getOwnPropertyNames(app).join(",") + "\n");
process.stderr.write("[inside] module.exports keys: " + Object.getOwnPropertyNames(module.exports).join(",") + "\n");
