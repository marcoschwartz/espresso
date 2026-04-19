process.stderr.write("requiring external.cjs directly...\n");
const m = require("./node_modules/zod/v3/external.cjs");
if (m.message) {
    process.stderr.write("ERR: " + m.message + "\n");
} else {
    process.stderr.write("OK keys=" + Object.keys(m).length + "\n");
}
