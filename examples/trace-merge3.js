var proto = require('/root/code/frontends/lungo/espresso/node_modules/express/lib/application.js');
process.stdout.write("proto === globalThis.__APP_MOD_EXPORTS__: " + (proto === globalThis.__APP_MOD_EXPORTS__) + "\n");
process.stdout.write("proto keys: " + Object.getOwnPropertyNames(proto).length + "\n");
process.stdout.write("global keys: " + Object.getOwnPropertyNames(globalThis.__APP_MOD_EXPORTS__).length + "\n");
