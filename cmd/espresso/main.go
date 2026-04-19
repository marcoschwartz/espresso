package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/marcoschwartz/espresso"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: espresso <file.js> [args...]")
		os.Exit(1)
	}

	filename := os.Args[1]
	code, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", filename, err)
		os.Exit(1)
	}

	vm := espresso.New()

	// Event loop — provides setTimeout, setInterval
	el := espresso.NewEventLoop(vm)
	el.RegisterGlobals()

	// process object — argv, env, exit, stdin, stdout, stderr
	argv := make([]*espresso.Value, len(os.Args))
	for i, arg := range os.Args {
		argv[i] = espresso.NewStr(arg)
	}
	vm.SetValue("process", espresso.NewObj(map[string]*espresso.Value{
		"argv": espresso.NewArr(argv),
		"env":  buildEnvObject(),
		"exit": espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
			code := 0
			if len(args) > 0 {
				code = int(args[0].Number())
			}
			os.Exit(code)
			return espresso.Undefined
		}),
	}))

	// Stdio — process.stdin.on('line'), process.stdout.write(), etc.
	espresso.RegisterStdio(vm, el, nil)

	// Buffer — binary data handling
	espresso.RegisterBuffer(vm)

	// Module system — require(), __dirname, __filename
	absFile, _ := filepath.Abs(filename)
	ms := espresso.NewModuleSystem(vm, filepath.Dir(absFile))
	ms.RegisterGlobals(absFile)

	// Run the script
	result, err := vm.Run(string(code))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Run the event loop (processes timers, stdin events, etc.)
	// Exits when no more refs (timers, listeners) are active.
	el.Run()

	if result != nil && !result.IsUndefined() {
		fmt.Println(result.String())
	}
}

func buildEnvObject() *espresso.Value {
	env := make(map[string]*espresso.Value)
	for _, e := range os.Environ() {
		for i := 0; i < len(e); i++ {
			if e[i] == '=' {
				env[e[:i]] = espresso.NewStr(e[i+1:])
				break
			}
		}
	}
	return espresso.NewObj(env)
}
