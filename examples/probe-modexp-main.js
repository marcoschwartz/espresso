var m = require('./probe-modexp.js');
process.stdout.write("[outside] m === globalThis.__NEW__: " + (m === globalThis.__NEW__) + "\n");
process.stdout.write("[outside] m.marker: " + m.marker + "\n");
process.stdout.write("[outside] globalThis.__LAST_MOD__.exports === m: " + (globalThis.__LAST_MOD__.exports === m) + "\n");
