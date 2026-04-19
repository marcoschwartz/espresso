package espresso

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var RequireDebug bool

// ─── Module System (require/module.exports) ─────────────
// Implements Node.js-compatible CommonJS modules.
// require(path) loads .js files, evaluates them in an isolated scope
// with module.exports/exports, and caches the result.

// ModuleSystem manages the require() function and module cache.
type ModuleSystem struct {
	vm      *VM
	cache   map[string]*Value // resolved path → exports
	baseDirs []string         // stack of directories for nested requires
	mu      sync.Mutex
}

// NewModuleSystem creates a module system bound to a VM.
func NewModuleSystem(vm *VM, baseDir string) *ModuleSystem {
	ms := &ModuleSystem{
		vm:       vm,
		cache:    make(map[string]*Value),
		baseDirs: []string{baseDir},
	}
	return ms
}

// RegisterGlobals injects require() and __dirname/__filename into VM scope.
func (ms *ModuleSystem) RegisterGlobals(filename string) {
	ms.vm.RegisterFunc("require", func(args []*Value) *Value {
		if len(args) == 0 {
			return Undefined
		}
		path := args[0].toStr()
		result, err := ms.Require(path)
		if err != nil {
			// Return error object
			return newError("Error", err.Error())
		}
		return result
	})

	if filename != "" {
		absFile, _ := filepath.Abs(filename)
		ms.vm.Set("__filename", absFile)
		ms.vm.Set("__dirname", filepath.Dir(absFile))
	}
}

// Require loads and evaluates a module, returning its exports.
func (ms *ModuleSystem) Require(path string) (*Value, error) {
	// Strip node: prefix — map to built-in stubs or bare name
	if strings.HasPrefix(path, "node:") {
		bareName := strings.TrimPrefix(path, "node:")
		// Check built-in modules first
		if builtin, ok := ms.getBuiltinModule(bareName); ok {
			return builtin, nil
		}
		// Fall through to resolve bare name from node_modules
		path = bareName
	}

	// Check built-in modules for bare names too (e.g. require('events'))
	if !strings.HasPrefix(path, "./") && !strings.HasPrefix(path, "../") && !strings.HasPrefix(path, "/") {
		if builtin, ok := ms.getBuiltinModule(path); ok {
			return builtin, nil
		}
	}

	resolved, err := ms.resolve(path)
	if err != nil {
		if RequireDebug {
			fmt.Fprintf(os.Stderr, "[require] FAIL resolve %q: %v\n", path, err)
		}
		return nil, err
	}

	if RequireDebug {
		fmt.Fprintf(os.Stderr, "[require] %q -> %s\n", path, resolved)
	}

	// Check cache
	ms.mu.Lock()
	if cached, ok := ms.cache[resolved]; ok {
		ms.mu.Unlock()
		if RequireDebug {
			fmt.Fprintf(os.Stderr, "[require] CACHED %s (typ=%d keys=%d)\n", resolved, cached.typ, len(cached.object))
		}
		return cached, nil
	}
	ms.mu.Unlock()

	// Read file
	code, err := os.ReadFile(resolved)
	if err != nil {
		return nil, err
	}

	// JSON files — parse and return directly
	if strings.HasSuffix(resolved, ".json") {
		val := JsonToValue(code)
		ms.mu.Lock()
		ms.cache[resolved] = val
		ms.mu.Unlock()
		return val, nil
	}

	// Pre-cache the exports object BEFORE evaluation to handle circular requires.
	// This matches Node.js behavior: circular requires get partial exports.
	exports := NewObj(make(map[string]*Value))
	ms.mu.Lock()
	ms.cache[resolved] = exports
	ms.mu.Unlock()

	// Evaluate the module (populates the exports object)
	finalExports := ms.evalModule(resolved, string(code), exports)

	// If module.exports was reassigned, update the cache
	if finalExports != exports {
		ms.mu.Lock()
		ms.cache[resolved] = finalExports
		ms.mu.Unlock()
	}

	return finalExports, nil
}

// evalModule evaluates JS code in an isolated module scope with
// module, exports, require, __filename, __dirname.
func (ms *ModuleSystem) evalModule(filename, code string, exports *Value) *Value {
	// Use the pre-created exports object (for circular require support)
	module := NewObj(map[string]*Value{
		"exports": exports,
		"id":      NewStr(filename),
		"loaded":  False,
	})

	// Create a child VM with the module's scope
	childVM := New()

	// Copy parent scope (builtins, globals)
	for k, v := range ms.vm.scope {
		childVM.scope[k] = v
	}

	// Set module-specific globals
	childVM.scope["module"] = module
	childVM.scope["exports"] = exports
	childVM.scope["__filename"] = NewStr(filename)
	childVM.scope["__dirname"] = NewStr(filepath.Dir(filename))

	// Register require for this module's directory
	dir := filepath.Dir(filename)
	ms.mu.Lock()
	ms.baseDirs = append(ms.baseDirs, dir)
	ms.mu.Unlock()

	childVM.RegisterFunc("require", func(args []*Value) *Value {
		if len(args) == 0 {
			return Undefined
		}
		path := args[0].toStr()
		result, err := ms.Require(path)
		if err != nil {
			return newError("Error", err.Error())
		}
		return result
	})

	// Run module code
	if os.Getenv("ESPRESSO_DEBUG_REQ") == "1" {
		fmt.Fprintf(os.Stderr, "[evalModule BEFORE Run] %s: module.object[exports]=%p (keys=%d)\n", filename, module.object["exports"], len(module.object["exports"].object))
	}
	childVM.Run(code)
	if os.Getenv("ESPRESSO_DEBUG_REQ") == "1" {
		fmt.Fprintf(os.Stderr, "[evalModule AFTER Run]  %s: module.object[exports]=%p (keys=%d)\n", filename, module.object["exports"], len(module.object["exports"].object))
	}

	// Patch fnBody functions on exports to capture module scope.
	// ExtractFunctions creates standalone functions that don't close over
	// the module's const/var declarations. Fix by storing the module scope.
	moduleScope := childVM.Scope()
	if RequireDebug {
		// Log before patch
		if strV, ok := exports.object["string"]; ok {
			fmt.Fprintf(os.Stderr, "[require] BEFORE patch %s: string typ=%d str=%q\n", filename, strV.typ, strV.str)
		}
	}
	patchExportedFunctions(exports, moduleScope)
	if RequireDebug {
		fmt.Fprintf(os.Stderr, "[require] AFTER  patch %s: obj_keys=%d\n", filename, len(exports.object))
	}

	// Pop base dir
	ms.mu.Lock()
	if len(ms.baseDirs) > 1 {
		ms.baseDirs = ms.baseDirs[:len(ms.baseDirs)-1]
	}
	ms.mu.Unlock()

	// Mark as loaded
	module.object["loaded"] = True

	// Return module.exports (which the module may have reassigned)
	finalExp := module.object["exports"]
	if os.Getenv("ESPRESSO_DEBUG_REQ") == "1" {
		fmt.Fprintf(os.Stderr, "[evalModule] %s: orig=%p final=%p keys=%d\n", filename, exports, finalExp, len(finalExp.object))
	}
	return finalExp
}

// resolve finds the absolute path for a require() call.
func (ms *ModuleSystem) resolve(path string) (string, error) {
	// Get current base directory
	ms.mu.Lock()
	baseDir := ms.baseDirs[len(ms.baseDirs)-1]
	ms.mu.Unlock()

	// Relative paths
	if strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") || strings.HasPrefix(path, "/") {
		return ms.resolveFile(baseDir, path)
	}

	// Built-in module names — check node_modules
	return ms.resolveNodeModule(baseDir, path)
}

// resolveFile resolves a relative or absolute file path.
func (ms *ModuleSystem) resolveFile(baseDir, path string) (string, error) {
	var resolved string
	if filepath.IsAbs(path) {
		resolved = path
	} else {
		resolved = filepath.Join(baseDir, path)
	}
	resolved = filepath.Clean(resolved)

	// Try exact path
	if fileExists(resolved) {
		return resolved, nil
	}

	// Try .js extension
	if fileExists(resolved + ".js") {
		return resolved + ".js", nil
	}

	// Try .cjs extension (CommonJS)
	if fileExists(resolved + ".cjs") {
		return resolved + ".cjs", nil
	}

	// Try .json extension
	if fileExists(resolved + ".json") {
		return resolved + ".json", nil
	}

	// Try package.json main field BEFORE index.js (package.json is authoritative)
	pkgPath := filepath.Join(resolved, "package.json")
	if fileExists(pkgPath) {
		main, err := readPackageMain(pkgPath)
		if err == nil && main != "" {
			mainPath := filepath.Join(resolved, main)
			if fileExists(mainPath) {
				return mainPath, nil
			}
			if fileExists(mainPath + ".js") {
				return mainPath + ".js", nil
			}
		}
	}

	// Try as directory with index.js
	indexPath := filepath.Join(resolved, "index.js")
	if fileExists(indexPath) {
		return indexPath, nil
	}

	return "", &os.PathError{Op: "require", Path: path, Err: os.ErrNotExist}
}

// resolveNodeModule walks up directories looking for node_modules.
func (ms *ModuleSystem) resolveNodeModule(baseDir, name string) (string, error) {
	// Split scoped packages: @scope/pkg/subpath → pkg=@scope/pkg, sub=./subpath
	pkgName, subPath := splitPackagePath(name)

	dir := baseDir
	for {
		nmDir := filepath.Join(dir, "node_modules", pkgName)

		// If there's a subpath, try exports field first
		if subPath != "" {
			pkgPath := filepath.Join(nmDir, "package.json")
			if fileExists(pkgPath) {
				resolved := resolveExports(pkgPath, nmDir, subPath)
				if resolved != "" && fileExists(resolved) {
					return resolved, nil
				}
			}
		}

		// Try as file (full path including subpath)
		resolved, err := ms.resolveFile(dir, filepath.Join("node_modules", name))
		if err == nil {
			return resolved, nil
		}

		// Try as directory with package.json main field
		pkgPath := filepath.Join(nmDir, "package.json")
		if fileExists(pkgPath) && subPath == "" {
			main, err := readPackageMain(pkgPath)
			if err == nil && main != "" {
				mainPath := filepath.Join(nmDir, main)
				if fileExists(mainPath) {
					return mainPath, nil
				}
				if fileExists(mainPath + ".js") {
					return mainPath + ".js", nil
				}
			}
		}

		// Try index.js in node_modules/name/
		indexPath := filepath.Join(nmDir, "index.js")
		if fileExists(indexPath) && subPath == "" {
			return indexPath, nil
		}

		// Walk up
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", &os.PathError{Op: "require", Path: name, Err: os.ErrNotExist}
}

// splitPackagePath splits a require path into package name and subpath.
// "@scope/pkg/sub/path" → ("@scope/pkg", "./sub/path")
// "pkg/sub/path" → ("pkg", "./sub/path")
// "pkg" → ("pkg", "")
func splitPackagePath(name string) (string, string) {
	parts := strings.Split(name, "/")
	if strings.HasPrefix(name, "@") && len(parts) >= 2 {
		pkg := parts[0] + "/" + parts[1]
		if len(parts) > 2 {
			return pkg, "./" + strings.Join(parts[2:], "/")
		}
		return pkg, ""
	}
	if len(parts) > 1 {
		return parts[0], "./" + strings.Join(parts[1:], "/")
	}
	return name, ""
}

// resolveExports resolves a subpath through package.json exports field.
func resolveExports(pkgJsonPath, pkgDir, subPath string) string {
	data, err := os.ReadFile(pkgJsonPath)
	if err != nil {
		return ""
	}
	src := string(data)

	// Find "exports" field — use the LAST occurrence (some packages have two)
	idx := strings.LastIndex(src, `"exports"`)
	if idx < 0 {
		return ""
	}

	// Parse the exports object using our JSON parser
	rest := src[idx+9:]
	// Skip : and whitespace
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == '\r' || rest[0] == ':') {
		rest = rest[1:]
	}
	if len(rest) == 0 || rest[0] != '{' {
		return ""
	}

	exportsJSON := extractBalanced(rest, '{', '}')
	if exportsJSON == "" {
		return ""
	}

	exports := JsonToValue([]byte(exportsJSON))
	if exports == nil || exports.typ != TypeObject || exports.object == nil {
		return ""
	}

	// Normalize subPath to start with ./
	if !strings.HasPrefix(subPath, "./") {
		subPath = "./" + subPath
	}

	// Try exact match first: exports["./server/mcp.js"]
	if entry, ok := exports.object[subPath]; ok {
		resolved := getRequirePath(entry)
		if resolved != "" {
			return filepath.Join(pkgDir, resolved)
		}
	}

	// Try wildcard match: exports["./*"] with pattern substitution
	if wildcard, ok := exports.object["./*"]; ok {
		resolved := getRequirePath(wildcard)
		if resolved != "" && strings.Contains(resolved, "*") {
			// subPath is "./server/mcp.js", strip "./"
			sub := strings.TrimPrefix(subPath, "./")
			mapped := strings.ReplaceAll(resolved, "*", sub)
			return filepath.Join(pkgDir, mapped)
		}
	}

	return ""
}

// getRequirePath extracts the "require" path from an exports entry.
// Entry can be a string or an object with {require: "...", import: "..."}.
func getRequirePath(v *Value) string {
	if v == nil {
		return ""
	}
	if v.typ == TypeString {
		return v.str
	}
	if v.typ == TypeObject && v.object != nil {
		if req, ok := v.object["require"]; ok && req.typ == TypeString {
			return req.str
		}
		// Fallback to "default"
		if def, ok := v.object["default"]; ok && def.typ == TypeString {
			return def.str
		}
	}
	return ""
}

// extractBalanced extracts a balanced {...} substring.
func extractBalanced(s string, open, close byte) string {
	if len(s) == 0 || s[0] != open {
		return ""
	}
	depth := 0
	inStr := byte(0)
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inStr != 0 {
			if ch == inStr && (i == 0 || s[i-1] != '\\') {
				inStr = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			inStr = ch
			continue
		}
		if ch == open {
			depth++
		} else if ch == close {
			depth--
			if depth == 0 {
				return s[:i+1]
			}
		}
	}
	return ""
}

// patchExportedFunctions attaches the module scope to fnBody functions
// so they can access module-level const/var declarations when called
// from outside the module.
// patchExportedFunctions updates exports to use the scope-capturing versions
// of functions that were re-created during module evaluation.
// ExtractFunctions creates fnBody functions without scope capture.
// evalStatements later creates arrow functions with scope capture.
// Exports still point to the old fnBody versions — update them.
// patchExportedFunctions updates fnBody functions on exports to include
// the module's scope, so they can access module-level const/var/require.
// Mutates Values in-place so re-exported pointers also get the fix.
func patchExportedFunctions(exports *Value, moduleScope map[string]*Value) {
	if exports == nil || exports.object == nil {
		return
	}
	for k, v := range exports.object {
		if v == nil || v.typ != TypeFunc {
			continue
		}
		// If module scope has a newer arrow version (with proper closure capture),
		// mutate the EXISTING Value in-place so all re-exported pointers also get fixed.
		if newer, ok := moduleScope[k]; ok && newer.typ == TypeFunc && newer != v && newer.str == "__arrow" {
			v.str = newer.str
			v.num = newer.num
			v.fnBody = ""
			v.fnParams = nil
			v.bc = nil
		} else if v.fnBody != "" {
			// No arrow version — inject module scope into fnBody function
			v.fnScope = moduleScope
		}
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func readPackageMain(pkgPath string) (string, error) {
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return "", err
	}
	// Quick parse: find "main": "..." in package.json
	s := string(data)
	idx := strings.Index(s, `"main"`)
	if idx < 0 {
		return "", nil
	}
	rest := s[idx+6:]
	// Skip whitespace and colon
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t' || rest[0] == ':') {
		rest = rest[1:]
	}
	if len(rest) == 0 || rest[0] != '"' {
		return "", nil
	}
	rest = rest[1:]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return "", nil
	}
	return rest[:end], nil
}

// getBuiltinModule returns a built-in module stub if one exists.
func (ms *ModuleSystem) getBuiltinModule(name string) (*Value, bool) {
	switch name {
	case "events":
		return ms.builtinEvents(), true
	case "process":
		if p := ms.vm.Get("process"); !p.IsUndefined() {
			return p, true
		}
		return NewObj(make(map[string]*Value)), true
	case "stream":
		return ms.builtinStream(), true
	case "readline":
		return ms.builtinReadline(), true
	case "url":
		return ms.builtinURL(), true
	case "crypto":
		return ms.builtinCrypto(), true
	case "http", "https", "tls", "net":
		// Stub — return empty module
		return NewObj(make(map[string]*Value)), true
	case "path":
		return ms.builtinPath(), true
	case "fs", "fs/promises":
		return NewObj(make(map[string]*Value)), true
	case "util":
		return ms.builtinUtil(), true
	case "os":
		return NewObj(make(map[string]*Value)), true
	case "timers/promises":
		return ms.builtinTimersPromises(), true
	}
	return nil, false
}

func (ms *ModuleSystem) builtinStream() *Value {
	m := NewObj(make(map[string]*Value))
	// Readable stub
	m.object["Readable"] = NewNativeFunc(func(args []*Value) *Value {
		return NewObj(make(map[string]*Value))
	})
	// Writable stub
	m.object["Writable"] = NewNativeFunc(func(args []*Value) *Value {
		return NewObj(make(map[string]*Value))
	})
	return m
}

func (ms *ModuleSystem) builtinReadline() *Value {
	m := NewObj(make(map[string]*Value))
	m.object["createInterface"] = NewNativeFunc(func(args []*Value) *Value {
		rl := NewObj(make(map[string]*Value))
		rl.object["on"] = NewNativeFunc(func(a []*Value) *Value { return rl })
		rl.object["close"] = NewNativeFunc(func(a []*Value) *Value { return Undefined })
		return rl
	})
	return m
}

func (ms *ModuleSystem) builtinURL() *Value {
	m := NewObj(make(map[string]*Value))
	m.object["URL"] = NewNativeFunc(func(args []*Value) *Value {
		u := NewObj(make(map[string]*Value))
		if len(args) > 0 {
			raw := args[0].toStr()
			u.object["href"] = NewStr(raw)
			u.object["toString"] = NewNativeFunc(func(a []*Value) *Value { return NewStr(raw) })
		}
		return u
	})
	return m
}

func (ms *ModuleSystem) builtinCrypto() *Value {
	m := NewObj(make(map[string]*Value))
	m.object["randomUUID"] = NewNativeFunc(func(args []*Value) *Value {
		// Simple UUID v4
		b := make([]byte, 16)
		for i := range b {
			b[i] = byte(i * 17)
		}
		return NewStr("00000000-0000-4000-8000-000000000000")
	})
	return m
}

func (ms *ModuleSystem) builtinPath() *Value {
	m := NewObj(make(map[string]*Value))
	m.object["join"] = NewNativeFunc(func(args []*Value) *Value {
		parts := make([]string, len(args))
		for i, a := range args {
			parts[i] = a.toStr()
		}
		return NewStr(filepath.Join(parts...))
	})
	m.object["dirname"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return NewStr(".") }
		return NewStr(filepath.Dir(args[0].toStr()))
	})
	m.object["basename"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return NewStr("") }
		return NewStr(filepath.Base(args[0].toStr()))
	})
	m.object["resolve"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return NewStr(".") }
		result := args[0].toStr()
		for i := 1; i < len(args); i++ {
			next := args[i].toStr()
			if filepath.IsAbs(next) {
				result = next
			} else {
				result = filepath.Join(result, next)
			}
		}
		abs, _ := filepath.Abs(result)
		return NewStr(abs)
	})
	m.object["extname"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return NewStr("") }
		return NewStr(filepath.Ext(args[0].toStr()))
	})
	m.object["sep"] = NewStr(string(filepath.Separator))
	return m
}

func (ms *ModuleSystem) builtinUtil() *Value {
	m := NewObj(make(map[string]*Value))
	m.object["inherits"] = NewNativeFunc(func(args []*Value) *Value { return Undefined })
	m.object["inspect"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) > 0 { return NewStr(args[0].String()) }
		return NewStr("undefined")
	})
	m.object["promisify"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) > 0 { return args[0] }
		return Undefined
	})
	return m
}

func (ms *ModuleSystem) builtinTimersPromises() *Value {
	m := NewObj(make(map[string]*Value))
	m.object["setTimeout"] = NewNativeFunc(func(args []*Value) *Value {
		return MakeResolvedPromise(Undefined)
	})
	m.object["setInterval"] = NewNativeFunc(func(args []*Value) *Value {
		return MakeResolvedPromise(Undefined)
	})
	return m
}
