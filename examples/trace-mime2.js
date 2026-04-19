var db = require('mime-db');
var keys = Object.keys(db);
process.stdout.write("total keys: " + keys.length + "\n");
var t0 = Date.now();
var done = 0;
for (var i = 0; i < 50; i++) {
    var type = keys[i];
    var mime = db[type];
    var exts = mime.extensions;
    done++;
}
process.stdout.write("50 iterations took " + (Date.now() - t0) + "ms\n");
