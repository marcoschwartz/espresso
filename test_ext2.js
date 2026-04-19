process.stderr.write("__dirname=" + __dirname + "\n");
const m = require("./external.cjs");
if (m.message) process.stderr.write("ERR: " + m.message + "\n");
else process.stderr.write("OK\n");
