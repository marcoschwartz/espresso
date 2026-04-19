var m = require('./reassign-mod3.js');
process.stdout.write("[outside] m keys: " + Object.getOwnPropertyNames(m).length + "\n");
process.stdout.write("[outside] m.a=" + m.a + " init=" + typeof m.init + "\n");
