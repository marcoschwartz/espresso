process.stdout.write("loading db.json...\n");
var t0 = Date.now();
var db = require('mime-db');
process.stdout.write("loaded in " + (Date.now() - t0) + "ms, keys: " + Object.keys(db).length + "\n");
process.stdout.write("loading mime-types...\n");
var t1 = Date.now();
var mt = require('mime-types');
process.stdout.write("loaded in " + (Date.now() - t1) + "ms\n");
