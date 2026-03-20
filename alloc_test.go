package espresso

import (
	"testing"
)

func BenchmarkFullScript(b *testing.B) {
	code := `
const arr = [];
for (let i = 0; i < 10000; i++) { arr.push(i); }
const sum = arr.map(n => n * 2).filter(n => n > 10000).reduce((a, b) => a + b, 0);
const obj = {};
for (let i = 0; i < 2000; i++) { obj["k" + i] = i; }
const ks = Object.keys(obj);
let total = 0;
for (let i = 0; i < ks.length; i++) { total = total + obj[ks[i]]; }
const nums = [];
for (let i = 0; i < 5000; i++) { nums.push(10000 - i); }
nums.sort((a, b) => a - b);
return total;
`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vm := New()
		vm.Run(code)
	}
}

func BenchmarkForLoop(b *testing.B) {
	code := `const arr = []; for (let i = 0; i < 10000; i++) { arr.push(i); } return arr.length;`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vm := New()
		vm.Run(code)
	}
}

func BenchmarkMapFilter(b *testing.B) {
	code := `var arr = []; for (let i = 0; i < 5000; i++) { arr.push(i); } return arr.map(n => n * 2).filter(n => n > 5000).length;`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vm := New()
		vm.Run(code)
	}
}

func BenchmarkSort(b *testing.B) {
	code := `var arr = []; for (let i = 0; i < 5000; i++) { arr.push(5000 - i); } arr.sort((a, b) => a - b); return arr[0];`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vm := New()
		vm.Run(code)
	}
}

func BenchmarkStringConcat(b *testing.B) {
	code := `var s = ""; for (let i = 0; i < 2000; i++) { s = s + "x"; } return s.length;`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vm := New()
		vm.Run(code)
	}
}

func BenchmarkObjKeys(b *testing.B) {
	code := `const obj = {}; for (let i = 0; i < 2000; i++) { obj["k" + i] = i; } return Object.keys(obj).length;`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vm := New()
		vm.Run(code)
	}
}
