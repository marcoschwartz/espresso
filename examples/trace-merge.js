var proto = require('./node_modules/express/lib/application.js');
process.stdout.write("proto keys count: " + Object.getOwnPropertyNames(proto).length + "\n");
process.stdout.write("proto.get typeof: " + typeof proto.get + "\n");
process.stdout.write("proto.use typeof: " + typeof proto.use + "\n");

var mixin = require('merge-descriptors');
var app = function() {};
mixin(app, proto, false);
process.stdout.write("app.get typeof: " + typeof app.get + "\n");
process.stdout.write("app.use typeof: " + typeof app.use + "\n");
process.stdout.write("Object.getOwnPropertyNames(app).length: " + Object.getOwnPropertyNames(app).length + "\n");
