// === Espresso Benchmark Suite ===

// 1. Array pipeline (map/filter/reduce)
const arr = [];
for (let i = 0; i < 10000; i++) {
  arr.push(i);
}
const sum = arr.map(n => n * 2).filter(n => n > 10000).reduce((a, b) => a + b, 0);
console.log("Array pipeline: " + sum);

// 2. String building
let s = "";
for (let i = 0; i < 2000; i++) {
  s = s + "x";
}
console.log("String length: " + s.length);

// 3. Object create + sum values
const obj = {};
for (let i = 0; i < 2000; i++) {
  obj["k" + i] = i;
}
let total = 0;
const ks = Object.keys(obj);
for (let i = 0; i < ks.length; i++) {
  total = total + obj[ks[i]];
}
console.log("Object sum: " + total);

// 4. Sort
const nums = [];
for (let i = 0; i < 5000; i++) {
  nums.push(10000 - i);
}
nums.sort((a, b) => a - b);
console.log("Sort check: " + nums[0] + "," + nums[4999]);

// 5. JSON round-trip + filter
const data = [];
for (let i = 0; i < 500; i++) {
  data.push({id: i, name: "user" + i, active: i % 2 === 0});
}
const json = JSON.stringify(data);
const parsed = JSON.parse(json);
const active = parsed.filter(u => u.active);
console.log("JSON active: " + active.length);

// 6. String methods
const words = [];
for (let i = 0; i < 1000; i++) {
  words.push("  Hello World  ");
}
const cleaned = words.map(w => w.trim().toLowerCase().replace("world", "espresso"));
console.log("Strings: " + cleaned[0]);

// 7. Nested loops + flat
const matrix = [];
for (let i = 0; i < 100; i++) {
  const row = [];
  for (let j = 0; j < 100; j++) {
    row.push(i * 100 + j);
  }
  matrix.push(row);
}
const flat = matrix.flat();
console.log("Flat length: " + flat.length);

// 8. Template literals + ternary
const items = [];
for (let i = 0; i < 1000; i++) {
  const status = i % 3 === 0 ? "active" : i % 3 === 1 ? "pending" : "done";
  items.push(`item-${i}-${status}`);
}
console.log("Items: " + items.length + " last: " + items[999]);

// 9. Recursive fibonacci
function fib(n) {
  if (n <= 1) return n;
  return fib(n - 1) + fib(n - 2);
}
console.log("fib(20): " + fib(20));

// 10. Array find/some/every/indexOf
const big = [];
for (let i = 0; i < 10000; i++) {
  big.push(i);
}
const found = big.find(x => x === 7777);
const hasNeg = big.some(x => x < 0);
const allPos = big.every(x => x >= 0);
const idx = big.indexOf(9999);
console.log("Find: " + found + " some: " + hasNeg + " every: " + allPos + " idx: " + idx);

// 11. Object.entries + destructure-like
const config = {host: "localhost", port: "3000", debug: "true", env: "prod"};
const entries = Object.entries(config);
const pairs = entries.map(e => e[0] + "=" + e[1]);
console.log("Config: " + pairs.join("&"));

// 12. Chained string ops
let chain = "the quick brown fox jumps over the lazy dog";
chain = chain.toUpperCase();
chain = chain.replace("FOX", "CAT");
chain = chain.split(" ").slice(0, 5).join("-");
console.log("Chain: " + chain);

// 13. Nested object access
const users = [];
for (let i = 0; i < 500; i++) {
  users.push({
    id: i,
    profile: {name: "user" + i, settings: {theme: i % 2 === 0 ? "dark" : "light"}}
  });
}
const darkUsers = users.filter(u => u.profile.settings.theme === "dark");
console.log("Dark users: " + darkUsers.length);

// 14. Math-heavy computation
let mathSum = 0;
for (let i = 1; i <= 10000; i++) {
  mathSum = mathSum + (i * i) % 997;
}
console.log("Math sum: " + mathSum);
