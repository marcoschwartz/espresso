var mods = ['http-errors','debug','encodeurl','escape-html','etag','fresh','fs','mime-types','ms','on-finished','range-parser','path','statuses','stream','util'];
for (var i = 0; i < mods.length; i++) {
    process.stdout.write(i + " " + mods[i] + " ... ");
    var m = require(mods[i]);
    process.stdout.write("ok (" + typeof m + ")\n");
}
