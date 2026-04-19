'use strict';
// Same top imports as application.js
var finalhandler = require('finalhandler');
var debug = require('debug')('express:application');
var View = require('./node_modules/express/lib/view');
var http = require('node:http');
var methods = require('./node_modules/express/lib/utils').methods;
var compileETag = require('./node_modules/express/lib/utils').compileETag;
var compileQueryParser = require('./node_modules/express/lib/utils').compileQueryParser;
var compileTrust = require('./node_modules/express/lib/utils').compileTrust;
var resolve = require('node:path').resolve;
var once = require('once');
var Router = require('router');

var slice = Array.prototype.slice;
var flatten = Array.prototype.flat;

var app = exports = module.exports = {};
app.init = function init() { return 42; };
app.use = function use(fn) { return "use"; };
app.get = function get(name) { return "get"; };
