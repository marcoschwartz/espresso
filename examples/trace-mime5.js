var db = require('mime-db');
var mimeScore = require('./node_modules/mime-types/mimeScore');
var extensions = Object.create(null);
var types = Object.create(null);
var conflicts = [];

function _preferredType(ext, type0, type1) {
  var score0 = type0 ? mimeScore(type0, db[type0].source) : 0;
  var score1 = type1 ? mimeScore(type1, db[type1].source) : 0;
  return score0 > score1 ? type0 : type1;
}

var t0 = Date.now();
var N = 2522;
var keys = Object.keys(db);
for (var i = 0; i < N; i++) {
    var type = keys[i];
    var mime = db[type];
    var exts = mime.extensions;
    if (!exts || !exts.length) continue;
    extensions[type] = exts;
    for (var j = 0; j < exts.length; j++) {
        var ext = exts[j];
        types[ext] = _preferredType(ext, types[ext], type);
    }
}
process.stdout.write(N + " iters took " + (Date.now() - t0) + "ms\n");
