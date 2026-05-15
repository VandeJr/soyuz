package checker

import "soyuz/internal/parser"

// preferredWeakNames are field names that are heuristically treated as back-references.
var preferredWeakNames = map[string]bool{
	"prev": true, "parent": true, "owner": true, "up": true, "back": true,
}

// typeEdge is a directed edge in the type graph.
type typeEdge struct {
	field  string
	target string
}

// heapTargetName returns the concrete type name for a type expression that refers to
// a heap type (record or class), unwrapping Optional if needed. Returns "" for primitives.
func heapTargetName(te parser.TypeExpr) string {
	if te == nil {
		return ""
	}
	switch t := te.(type) {
	case *parser.NamedType:
		switch t.Name {
		case "Int", "Float", "Bool", "String":
			return ""
		default:
			return t.Name
		}
	case *parser.OptionalType:
		return heapTargetName(t.Inner)
	}
	return ""
}

// DetectImplicitWeakFields builds a type graph from type declarations in prog,
// runs Tarjan SCC, and returns a map[typeName][fieldName] = true for every field
// that should be treated as a weak reference to break reference cycles.
func DetectImplicitWeakFields(prog *parser.Program) map[string]map[string]bool {
	// Build adjacency list: typeName → []typeEdge
	edges := make(map[string][]typeEdge)
	// Collect all declared type names so we only follow intra-program edges.
	knownTypes := make(map[string]bool)

	for _, node := range prog.Body {
		switch n := node.(type) {
		case *parser.RecordDecl:
			knownTypes[n.Name] = true
		case *parser.ClassDecl:
			knownTypes[n.Name] = true
		}
	}

	for _, node := range prog.Body {
		switch n := node.(type) {
		case *parser.RecordDecl:
			for _, f := range n.Fields {
				if tgt := heapTargetName(f.Type); tgt != "" && knownTypes[tgt] {
					edges[n.Name] = append(edges[n.Name], typeEdge{f.Name, tgt})
				}
			}
		case *parser.ClassDecl:
			for _, member := range n.Body {
				if vd, ok := member.(*parser.VarDecl); ok {
					if tgt := heapTargetName(vd.Type); tgt != "" && knownTypes[tgt] {
						edges[n.Name] = append(edges[n.Name], typeEdge{vd.Name, tgt})
					}
				}
			}
		}
	}

	// Tarjan's SCC.
	index := 0
	stack := []string{}
	onStack := map[string]bool{}
	indices := map[string]int{}
	lowlinks := map[string]int{}
	var sccs [][]string

	var strongconnect func(v string)
	strongconnect = func(v string) {
		indices[v] = index
		lowlinks[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		for _, e := range edges[v] {
			w := e.target
			if _, visited := indices[w]; !visited {
				strongconnect(w)
				if lowlinks[w] < lowlinks[v] {
					lowlinks[v] = lowlinks[w]
				}
			} else if onStack[w] {
				if indices[w] < lowlinks[v] {
					lowlinks[v] = indices[w]
				}
			}
		}

		if lowlinks[v] == indices[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			sccs = append(sccs, scc)
		}
	}

	for name := range knownTypes {
		if _, visited := indices[name]; !visited {
			strongconnect(name)
		}
	}

	// For each SCC that contains a cycle, pick a field to mark as weak.
	result := make(map[string]map[string]bool)

	markWeak := func(typeName, fieldName string) {
		if result[typeName] == nil {
			result[typeName] = make(map[string]bool)
		}
		result[typeName][fieldName] = true
	}

	for _, scc := range sccs {
		sccSet := make(map[string]bool)
		for _, n := range scc {
			sccSet[n] = true
		}

		// Check for cycle: size>1 always has a cycle; size==1 only if there's a self-loop.
		hasCycle := len(scc) > 1
		if !hasCycle {
			v := scc[0]
			for _, e := range edges[v] {
				if e.target == v {
					hasCycle = true
					break
				}
			}
		}
		if !hasCycle {
			continue
		}

		// Collect all intra-SCC edges grouped by source node.
		type candidate struct{ typeName, fieldName string }
		var preferred []candidate
		var fallback []candidate

		for _, node := range scc {
			for _, e := range edges[node] {
				if sccSet[e.target] {
					if preferredWeakNames[e.field] {
						preferred = append(preferred, candidate{node, e.field})
					} else {
						fallback = append(fallback, candidate{node, e.field})
					}
				}
			}
		}

		// Pick one preferred name if available; otherwise use the first fallback.
		if len(preferred) > 0 {
			markWeak(preferred[0].typeName, preferred[0].fieldName)
		} else if len(fallback) > 0 {
			markWeak(fallback[0].typeName, fallback[0].fieldName)
		}
	}

	return result
}
