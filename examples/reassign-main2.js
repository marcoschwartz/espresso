var m = require('./reassign-mod2.js');
process.stdout.write("[outside] m keys: " + Object.getOwnPropertyNames(m).join(",") + "\n");
process.stdout.write("[outside] m.a=" + m.a + "\n");
