process.stdout.write("1\n");
var bodyParser = require('body-parser');
process.stdout.write("2 body-parser typeof: " + typeof bodyParser + "\n");
var events = require('node:events');
process.stdout.write("3 node:events typeof: " + typeof events + "\n");
var mixin = require('merge-descriptors');
process.stdout.write("4 merge-descriptors typeof: " + typeof mixin + "\n");
var Router = require('router');
process.stdout.write("5 router typeof: " + typeof Router + "\n");
