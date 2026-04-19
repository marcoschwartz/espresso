var m = require('./testmod3.js');
process.stdout.write("keys: " + Object.getOwnPropertyNames(m).length + "\n");
process.stdout.write("init: " + typeof m.init + "\n");
process.stdout.write("get: " + typeof m.get + "\n");
