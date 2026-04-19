var db = require('mime-db');
process.stdout.write("starting forEach\n");
var count = 0;
Object.keys(db).forEach(function forEachMimeType(type) {
    count++;
    if (count > 2600) { process.stdout.write("runaway\n"); throw new Error("stop"); }
    if (type === undefined) { process.stdout.write("UNDEF TYPE at count=" + count + "\n"); return; }
});
process.stdout.write("done, count=" + count + "\n");
