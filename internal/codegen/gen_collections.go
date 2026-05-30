package codegen

import (
	"fmt"

	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"soyuz/internal/checker"
	"soyuz/internal/parser"
)

// collectionElemKind: 0 = Int, 1 = String (for list/map to_string runtime).
func collectionElemKind(t checker.Type) int64 {
	switch bt := t.(type) {
	case *checker.BasicType:
		switch bt.Name {
		case "Int", "Char":
			return 0
		case "String":
			return 1
		}
	}
	return 0
}

func (g *Generator) checkerTypeForExpr(n parser.Node) checker.Type {
	if t, ok := g.check.NodeTypes[n]; ok {
		return t
	}
	return nil
}

func (g *Generator) emitCollectionToString(val value.Value, soyuzType checker.Type) (value.Value, error) {
	st, ok := soyuzType.(*checker.SpecializedType)
	if !ok {
		return val, fmt.Errorf("not a collection type")
	}
	ct, ok := st.Base.(*checker.ClassType)
	if !ok {
		return val, fmt.Errorf("not a collection type")
	}

	obj := val
	if !val.Type().Equal(types.I8Ptr) {
		obj = g.current.NewBitCast(val, types.I8Ptr)
	}

	switch ct.Name {
	case "List":
		kind := collectionElemKind(st.Params[0])
		return g.current.NewCall(
			g.findFunc("soyuz_list_to_string"),
			obj,
			constant.NewInt(types.I64, kind),
		), nil
	case "Map":
		keyIsStr := int64(0)
		if bt, ok := st.Params[0].(*checker.BasicType); ok && bt.Name == "String" {
			keyIsStr = 1
		}
		valKind := collectionElemKind(st.Params[1])
		return g.current.NewCall(
			g.findFunc("soyuz_map_to_string"),
			obj,
			constant.NewInt(types.I64, keyIsStr),
			constant.NewInt(types.I64, valKind),
		), nil
	default:
		return val, fmt.Errorf("unsupported collection %s", ct.Name)
	}
}

func (g *Generator) isListOrMapType(t checker.Type) bool {
	st, ok := t.(*checker.SpecializedType)
	if !ok {
		return false
	}
	ct, ok := st.Base.(*checker.ClassType)
	if !ok {
		return false
	}
	return ct.Name == "List" || ct.Name == "Map"
}
