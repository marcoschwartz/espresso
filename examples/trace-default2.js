process.stdout.write("a\n");
var f = function mimeScore(mimeType, source) { return source; };
process.stdout.write("b\n");
var g = function mimeScore(mimeType, source = 'def') { return source; };
process.stdout.write("c\n");
