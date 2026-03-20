package espresso

// ─── Compiled Evaluation ────────────────────────────────────────
// Instead of walking tokens on every call, we compile JS code into
// Go closures at compile time. At execution time, we run the closures
// directly — no token parsing needed.

// compiledExpr evaluates to a Value given a scope.
type compiledExpr func(scope map[string]*Value) *Value

// compiledPage is the top-level compiled representation of a function body.
type compiledPage struct {
	Preamble   []compiledStmt
	ReturnExpr compiledExpr
}

type compiledStmt struct {
	// Simple assignment: name = expr
	Name string
	Expr compiledExpr
	// Array destructuring: [name1, name2] = expr
	IsArrayDestructure bool
	Names              []string
	// If statement
	IsIf      bool
	Condition compiledExpr
	IfBody    *compiledPage
	ElseBody  *compiledPage
	// For loop: for (init; cond; update) { body }
	IsForLoop  bool
	InitStmt   *compiledStmt
	LoopCond   compiledExpr
	LoopUpdate compiledExpr
	LoopBody   *compiledPage
	// For...of: for (const x of arr) { body }
	IsForOf  bool
	IterVar  string
	IterExpr compiledExpr
	// While loop
	IsWhile bool
	// Try/catch
	IsTryCatch bool
	TryBody    *compiledPage
	// Reassignment: name = expr (without const/let)
	IsReassign bool
	// Compound assignment: name += expr, name -= expr
	IsCompound bool
	CompoundOp string // "+=" or "-="
	// Increment/decrement: name++ or name--
	IsIncrement bool
	IncrDelta   float64 // +1 or -1
	// No-op (unused statements)
	IsNoop bool
	// Side-effect expression (console.log etc.) — executed but not assigned
	ExprFn compiledExpr
}

// ─── Execution ──────────────────────────────────────────────────

func (cp *compiledPage) execute(scope map[string]*Value) *Value {
	result := cp.executeStatements(scope)
	if result != nil {
		return result
	}
	if cp.ReturnExpr != nil {
		return cp.ReturnExpr(scope)
	}
	return Undefined
}

func (cp *compiledPage) executeStatements(scope map[string]*Value) *Value {
	for _, stmt := range cp.Preamble {
		if stmt.IsNoop {
			continue
		} else if stmt.ExprFn != nil {
			stmt.ExprFn(scope)
			continue
		} else if stmt.IsIf {
			if stmt.Condition(scope).truthy() {
				if stmt.IfBody != nil {
					if result := stmt.IfBody.executeStatements(scope); result != nil {
						return result
					}
				}
			} else if stmt.ElseBody != nil {
				if result := stmt.ElseBody.executeStatements(scope); result != nil {
					return result
				}
			}
		} else if stmt.IsForLoop {
			// Execute init
			if stmt.InitStmt != nil && stmt.InitStmt.Expr != nil {
				scope[stmt.InitStmt.Name] = stmt.InitStmt.Expr(scope)
			}
			for iter := 0; iter < 10000; iter++ {
				if stmt.LoopCond != nil && !stmt.LoopCond(scope).truthy() {
					break
				}
				if stmt.LoopBody != nil {
					if result := stmt.LoopBody.executeStatements(scope); result != nil {
						return result
					}
				}
				if stmt.LoopUpdate != nil {
					stmt.LoopUpdate(scope)
				}
			}
		} else if stmt.IsForOf {
			arr := stmt.IterExpr(scope)
			if arr.typ == TypeArray {
				for _, item := range arr.array {
					scope[stmt.IterVar] = item
					if stmt.LoopBody != nil {
						if result := stmt.LoopBody.executeStatements(scope); result != nil {
							return result
						}
					}
				}
			}
		} else if stmt.IsWhile {
			for iter := 0; iter < 10000; iter++ {
				if stmt.LoopCond != nil && !stmt.LoopCond(scope).truthy() {
					break
				}
				if stmt.LoopBody != nil {
					if result := stmt.LoopBody.executeStatements(scope); result != nil {
						return result
					}
				}
			}
		} else if stmt.IsTryCatch {
			if stmt.TryBody != nil {
				if result := stmt.TryBody.executeStatements(scope); result != nil {
					return result
				}
			}
		} else if stmt.IsIncrement {
			if v, ok := scope[stmt.Name]; ok {
				scope[stmt.Name] = newNum(v.toNum() + stmt.IncrDelta)
			}
		} else if stmt.IsCompound {
			if v, ok := scope[stmt.Name]; ok {
				val := stmt.Expr(scope)
				if stmt.CompoundOp == "+=" {
					if v.typ == TypeString || val.typ == TypeString {
						scope[stmt.Name] = newStr(v.toStr() + val.toStr())
					} else {
						scope[stmt.Name] = newNum(v.toNum() + val.toNum())
					}
				} else {
					scope[stmt.Name] = newNum(v.toNum() - val.toNum())
				}
			}
		} else if stmt.IsReassign {
			scope[stmt.Name] = stmt.Expr(scope)
		} else if stmt.IsArrayDestructure {
			val := stmt.Expr(scope)
			if val.typ == TypeArray {
				for i, name := range stmt.Names {
					if i < len(val.array) {
						scope[name] = val.array[i]
					} else {
						scope[name] = Undefined
					}
				}
			} else {
				for _, name := range stmt.Names {
					scope[name] = Undefined
				}
			}
		} else if stmt.Expr != nil {
			scope[stmt.Name] = stmt.Expr(scope)
		}
	}
	return nil
}
