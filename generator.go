package espresso

// ─── Generators (function*, yield) ──────────────────────
// Implements generator functions that return iterator objects.
// Each .next() call evaluates code up to the next yield.

// jsGenerator holds the state of a generator instance.
type jsGenerator struct {
	segments [][]tok          // code segments between yields
	scope    map[string]*Value
	pos      int              // current segment index
	done     bool
	yieldExprs [][]tok        // yield value expressions
}

// newGeneratorFunc creates a generator function Value.
// When called, it returns an iterator with .next()/.return()/.throw().
func newGeneratorFunc(params []string, bodyTokens []tok, defScope map[string]*Value) *Value {
	// Parse the body into segments split by yield
	segments, yieldExprs := splitAtYields(bodyTokens)

	return NewNativeFunc(func(args []*Value) *Value {
		// Create a new scope for this generator instance
		scope := make(map[string]*Value, len(defScope))
		for k, v := range defScope {
			scope[k] = v
		}
		// Bind parameters
		for i, p := range params {
			if i < len(args) {
				scope[p] = args[i]
			} else {
				scope[p] = Undefined
			}
		}

		gen := &jsGenerator{
			segments:   segments,
			yieldExprs: yieldExprs,
			scope:      scope,
			pos:        0,
			done:       false,
		}

		return newIteratorValue(gen)
	})
}

// newIteratorValue wraps a generator in an iterator object with .next(), .return(), .throw().
func newIteratorValue(gen *jsGenerator) *Value {
	iter := NewObj(make(map[string]*Value))

	iter.object["next"] = NewNativeFunc(func(args []*Value) *Value {
		if gen.done {
			return NewObj(map[string]*Value{
				"value": Undefined,
				"done":  True,
			})
		}

		// If .next(val) is called, inject val as the result of the last yield
		if len(args) > 0 && gen.pos > 0 {
			gen.scope["__yield_input__"] = args[0]
		}

		// Execute the current segment (code before the next yield)
		if gen.pos < len(gen.segments) {
			seg := gen.segments[gen.pos]
			if len(seg) > 0 {
				toks := make([]tok, len(seg))
				copy(toks, seg)
				toks = append(toks, tok{t: tokEOF})
				ev := &evaluator{tokens: toks, pos: 0, scope: gen.scope}
				result := ev.evalStatements()
				// If the segment had a return statement
				if result != nil && !isThrow(result) && result != breakSentinel && result != continueSentinel {
					gen.done = true
					return NewObj(map[string]*Value{
						"value": result,
						"done":  True,
					})
				}
			}
		}

		// Evaluate the yield expression
		var yieldVal *Value
		if gen.pos < len(gen.yieldExprs) && gen.yieldExprs[gen.pos] != nil {
			toks := make([]tok, len(gen.yieldExprs[gen.pos]))
			copy(toks, gen.yieldExprs[gen.pos])
			toks = append(toks, tok{t: tokEOF})
			ev := &evaluator{tokens: toks, pos: 0, scope: gen.scope}
			yieldVal = ev.expr()
		} else {
			yieldVal = Undefined
		}

		gen.pos++

		// Check if we've passed the last yield
		if gen.pos >= len(gen.segments) {
			gen.done = true
			return NewObj(map[string]*Value{
				"value": yieldVal,
				"done":  True,
			})
		}

		return NewObj(map[string]*Value{
			"value": yieldVal,
			"done":  False,
		})
	})

	iter.object["return"] = NewNativeFunc(func(args []*Value) *Value {
		gen.done = true
		val := Undefined
		if len(args) > 0 {
			val = args[0]
		}
		return NewObj(map[string]*Value{
			"value": val,
			"done":  True,
		})
	})

	iter.object["throw"] = NewNativeFunc(func(args []*Value) *Value {
		gen.done = true
		return NewObj(map[string]*Value{
			"value": Undefined,
			"done":  True,
		})
	})

	return iter
}

// splitAtYields splits tokens at yield statements.
// Returns segments (code between yields) and yield expressions.
// For: `a = 1; yield x; b = 2; yield y; c = 3;`
// segments = [[a = 1], [b = 2], [c = 3]]
// yieldExprs = [[x], [y]]
func splitAtYields(tokens []tok) (segments [][]tok, yieldExprs [][]tok) {
	var currentSeg []tok

	i := 0
	for i < len(tokens) {
		if tokens[i].t == tokEOF {
			break
		}
		if tokens[i].t == tokIdent && tokens[i].v == "yield" {
			// Save current segment
			segments = append(segments, currentSeg)
			currentSeg = nil
			i++ // skip "yield"

			// Collect the yield expression tokens (until ; or })
			var expr []tok
			depth := 0
			for i < len(tokens) && tokens[i].t != tokEOF {
				if tokens[i].t == tokSemi {
					i++ // skip ;
					break
				}
				if tokens[i].t == tokLParen || tokens[i].t == tokLBrack || tokens[i].t == tokLBrace {
					depth++
				}
				if tokens[i].t == tokRParen || tokens[i].t == tokRBrack || tokens[i].t == tokRBrace {
					if depth == 0 {
						break
					}
					depth--
				}
				expr = append(expr, tokens[i])
				i++
			}
			yieldExprs = append(yieldExprs, expr)
		} else {
			currentSeg = append(currentSeg, tokens[i])
			i++
		}
	}

	// Final segment (code after last yield)
	segments = append(segments, currentSeg)

	return
}
