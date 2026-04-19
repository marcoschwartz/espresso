package espresso

import (
	"testing"
)

func TestClassBasicConstructorAndMethods(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		class Person {
			constructor(name, age) {
				this.name = name;
				this.age = age;
			}
			greet() {
				return "Hello, " + this.name;
			}
			getAge() {
				return this.age;
			}
		}
		const p = new Person("Alice", 30);
	`)
	if err != nil {
		t.Fatal(err)
	}

	p := vm.Get("p")
	if p.IsUndefined() {
		t.Fatal("expected p to be defined")
	}
	if p.Get("name").String() != "Alice" {
		t.Errorf("expected name=Alice, got %s", p.Get("name").String())
	}
	if p.Get("age").Number() != 30 {
		t.Errorf("expected age=30, got %v", p.Get("age").Number())
	}
}

func TestClassMethodCallWithThis(t *testing.T) {
	vm := New()
	result, err := vm.Run(`
		class Counter {
			constructor(start) {
				this.count = start;
			}
			increment() {
				this.count = this.count + 1;
				return this.count;
			}
			getValue() {
				return this.count;
			}
		}
		const c = new Counter(10);
		c.increment();
		c.increment();
		const val = c.getValue();
	`)
	_ = result
	if err != nil {
		t.Fatal(err)
	}

	val := vm.Get("val")
	if val.Number() != 12 {
		t.Errorf("expected val=12, got %v", val.Number())
	}
}

func TestClassNewCreatesInstance(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		class Box {
			constructor(w, h) {
				this.width = w;
				this.height = h;
			}
			area() {
				return this.width * this.height;
			}
		}
		const b1 = new Box(3, 4);
		const b2 = new Box(5, 6);
		const a1 = b1.area();
		const a2 = b2.area();
	`)
	if err != nil {
		t.Fatal(err)
	}

	a1 := vm.Get("a1")
	a2 := vm.Get("a2")
	if a1.Number() != 12 {
		t.Errorf("expected a1=12, got %v", a1.Number())
	}
	if a2.Number() != 30 {
		t.Errorf("expected a2=30, got %v", a2.Number())
	}
}

func TestClassExtends(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		class Animal {
			constructor(name) {
				this.name = name;
			}
			speak() {
				return this.name + " makes a sound";
			}
		}
		class Dog extends Animal {
			constructor(name, breed) {
				super(name);
				this.breed = breed;
			}
			bark() {
				return this.name + " barks";
			}
		}
		const d = new Dog("Rex", "Labrador");
		const nm = d.name;
		const br = d.breed;
		const bk = d.bark();
	`)
	if err != nil {
		t.Fatal(err)
	}

	if vm.Get("nm").String() != "Rex" {
		t.Errorf("expected name=Rex, got %s", vm.Get("nm").String())
	}
	if vm.Get("br").String() != "Labrador" {
		t.Errorf("expected breed=Labrador, got %s", vm.Get("br").String())
	}
	if vm.Get("bk").String() != "Rex barks" {
		t.Errorf("expected 'Rex barks', got %s", vm.Get("bk").String())
	}
}

func TestClassMethodInheritance(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		class Shape {
			constructor(type) {
				this.type = type;
			}
			describe() {
				return "I am a " + this.type;
			}
		}
		class Circle extends Shape {
			constructor(radius) {
				super("circle");
				this.radius = radius;
			}
		}
		const c = new Circle(5);
		const desc = c.describe();
		const r = c.radius;
	`)
	if err != nil {
		t.Fatal(err)
	}

	if vm.Get("desc").String() != "I am a circle" {
		t.Errorf("expected 'I am a circle', got %s", vm.Get("desc").String())
	}
	if vm.Get("r").Number() != 5 {
		t.Errorf("expected radius=5, got %v", vm.Get("r").Number())
	}
}

func TestClassInstanceof(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		class Vehicle {}
		class Car extends Vehicle {
			constructor() {
				super();
			}
		}
		const car = new Car();
		const isCar = car instanceof Car;
		const isVehicle = car instanceof Vehicle;
	`)
	if err != nil {
		t.Fatal(err)
	}

	if !vm.Get("isCar").Bool() {
		t.Error("expected car instanceof Car to be true")
	}
	if !vm.Get("isVehicle").Bool() {
		t.Error("expected car instanceof Vehicle to be true")
	}
}

func TestClassGetterSetter(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		class Temperature {
			constructor(celsius) {
				this._celsius = celsius;
			}
			get fahrenheit() {
				return this._celsius * 9 / 5 + 32;
			}
			set fahrenheit(f) {
				this._celsius = (f - 32) * 5 / 9;
			}
		}
		const temp = new Temperature(100);
		const f1 = temp.fahrenheit;
	`)
	if err != nil {
		t.Fatal(err)
	}

	f1 := vm.Get("f1")
	if f1.Number() != 212 {
		t.Errorf("expected fahrenheit=212, got %v", f1.Number())
	}
}

func TestClassNoConstructor(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		class Empty {}
		const e = new Empty();
	`)
	if err != nil {
		t.Fatal(err)
	}

	e := vm.Get("e")
	if e.IsUndefined() {
		t.Error("expected e to be defined")
	}
	if e.Type() != TypeObject {
		t.Errorf("expected object type, got %v", e.Type())
	}
}

func TestClassMultipleInstances(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		class Point {
			constructor(x, y) {
				this.x = x;
				this.y = y;
			}
		}
		const p1 = new Point(1, 2);
		const p2 = new Point(3, 4);
	`)
	if err != nil {
		t.Fatal(err)
	}

	p1 := vm.Get("p1")
	p2 := vm.Get("p2")
	if p1.Get("x").Number() != 1 || p1.Get("y").Number() != 2 {
		t.Errorf("p1 wrong: x=%v y=%v", p1.Get("x").Number(), p1.Get("y").Number())
	}
	if p2.Get("x").Number() != 3 || p2.Get("y").Number() != 4 {
		t.Errorf("p2 wrong: x=%v y=%v", p2.Get("x").Number(), p2.Get("y").Number())
	}
}

func TestStaticMethod(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		class Foo {
			static create(x) { return new Foo(x); }
			constructor(x) { this.x = x; }
		}
		return Foo.create(42).x;
	`)
	if r.Number() != 42 { t.Errorf("expected 42, got %v", r.Number()) }
}

func TestInstanceof_ErrorSubclass(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		class AppError extends Error {
			constructor(msg, code) { super(msg); this.code = code; }
		}
		const e = new AppError("fail", 500);
		const results = [];
		results.push(e instanceof AppError);
		results.push(e instanceof Error);
		results.push(e.code === 500);
		return results.every(x => x === true);
	`)
	if !r.Truthy() {
		t.Error("expected AppError to be instanceof both AppError and Error")
	}
}

func TestErrorSubclass_MessageViaSuper(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		class McpError extends Error {
			constructor(code, msg) { super(msg); this.code = code; }
		}
		const e = new McpError(500, "server error");
		return e.message + "|" + e.code;
	`)
	if r.String() != "server error|500" {
		t.Errorf("expected 'server error|500', got '%s'", r.String())
	}
}

func TestInstanceof_DeepChain(t *testing.T) {
	vm := New()
	r, _ := vm.Run(`
		class A { constructor() { this.a = true; } }
		class B extends A { constructor() { super(); this.b = true; } }
		class C extends B { constructor() { super(); this.c = true; } }
		const c = new C();
		return (c instanceof C) && (c instanceof B) && (c instanceof A);
	`)
	if !r.Truthy() {
		t.Error("expected deep inheritance instanceof to work")
	}
}
