process.stderr.write("1: require zod/v3...\n");
try {
    const z = require("zod/v3");
    process.stderr.write("2: type=" + typeof z + " keys=" + Object.keys(z).length + "\n");
    if (z.message) process.stderr.write("3: ERR=" + z.message + "\n");
} catch(e) {
    process.stderr.write("ERR: " + e.message + "\n");
}
