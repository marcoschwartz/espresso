var debug = require('debug');
// NOT calling debug here

var app = exports = module.exports = {};
app.init = function init() { return 42; };
process.stderr.write("[inside] app keys: " + Object.getOwnPropertyNames(app).length + "\n");
