// Mimic express application.js: requires + reassign + many assignments
var bodyParser = require('body-parser'); // triggers needsInterpreter
var http = require('node:http');
var app = exports = module.exports = {};

app.a = 1;
app.b = 2;
app.c = 3;
app.d = 4;
app.e = 5;
app.init = function init() { return 42; };
// Intent use: something that might use setPrototypeOf
Object.setPrototypeOf(app, {});

process.stderr.write("[inside] app keys: " + Object.getOwnPropertyNames(app).length + " module.exports===app: " + (module.exports === app) + "\n");
