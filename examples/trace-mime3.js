var db = require('mime-db');
var mimeScore = require('./node_modules/mime-types/mimeScore');
var keys = Object.keys(db);
process.stdout.write("total keys: " + keys.length + "\n");
var extensions = {};
var types = {};
var t0 = Date.now();
var N = 100;
for (var i = 0; i < N; i++) {
    var type = keys[i];
    var mime = db[type];
    var exts = mime.extensions;
    if (!exts || !exts.length) continue;
    extensions[type] = exts;
    for (var j = 0; j < exts.length; j++) {
        var ext = exts[j];
        var score = mimeScore(type, mime.source);
        types[ext] = type;
    }
}
process.stdout.write(N + " iters took " + (Date.now() - t0) + "ms\n");
