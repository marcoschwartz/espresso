var m = require('./testmod.js');
process.stdout.write("m keys: " + Object.getOwnPropertyNames(m).length + "\n");
process.stdout.write("m === __TESTMOD_X__: " + (m === globalThis.__TESTMOD_X__) + "\n");
process.stdout.write("m === __TESTMOD_MODEXP__: " + (m === globalThis.__TESTMOD_MODEXP__) + "\n");
process.stdout.write("X keys: " + Object.getOwnPropertyNames(globalThis.__TESTMOD_X__).length + "\n");
