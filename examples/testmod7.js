var M_BEFORE = module;
var EXP_BEFORE = module.exports;
var debug = require('debug');
var M_AFTER_REQ = module;
var EXP_AFTER_REQ = module.exports;
var d = debug('express:application');
var M_AFTER_CALL = module;
var EXP_AFTER_CALL = module.exports;

process.stderr.write("M: before===afterReq: " + (M_BEFORE === M_AFTER_REQ) + ", beforeReq===afterCall: " + (M_BEFORE === M_AFTER_CALL) + "\n");
process.stderr.write("EXP: before===afterReq: " + (EXP_BEFORE === EXP_AFTER_REQ) + ", beforeReq===afterCall: " + (EXP_BEFORE === EXP_AFTER_CALL) + "\n");

// Now set module.exports = X
var app = exports = module.exports = {};
process.stderr.write("after reassign: M_BEFORE.exports===app? " + (M_BEFORE.exports === app) + ", module===M_BEFORE: " + (module === M_BEFORE) + "\n");
app.init = function init() { return 42; };
