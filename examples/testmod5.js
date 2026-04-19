var debug = require('debug');
process.stderr.write("before call, debug typeof: " + typeof debug + "\n");
var d = debug('express:application');
process.stderr.write("after call, d typeof: " + typeof d + "\n");

var app = exports = module.exports = {};
app.init = function init() { return 42; };
process.stderr.write("app.init set, app keys: " + Object.getOwnPropertyNames(app).length + "\n");
process.stderr.write("module.exports keys: " + Object.getOwnPropertyNames(module.exports).length + "\n");
process.stderr.write("module.exports === app: " + (module.exports === app) + "\n");
