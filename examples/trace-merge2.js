var proto = require('/root/code/frontends/lungo/espresso/node_modules/express/lib/application.js');
var names = Object.getOwnPropertyNames(proto);
process.stdout.write("proto keys: " + names.join(",") + "\n");
for (var i = 0; i < names.length; i++) {
    process.stdout.write("  " + names[i] + " typeof: " + typeof proto[names[i]] + "\n");
}
