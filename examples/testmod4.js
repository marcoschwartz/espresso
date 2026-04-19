'use strict';
var finalhandler = require('finalhandler');
var debug = require('debug')('express:application');

var app = exports = module.exports = {};
app.init = function init() { return 42; };
