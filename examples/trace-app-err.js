var p = require('./node_modules/express/lib/application.js');
process.stdout.write("name: " + p.name + "\n");
process.stdout.write("message: " + p.message + "\n");
process.stdout.write("stack: " + p.stack + "\n");
