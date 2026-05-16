package codegen

import (
	"soyuz/internal/checker"
	"soyuz/internal/parser"

	"github.com/llir/llvm/ir/types"
)

func (g *Generator) mapSoyuzTypeToLLVM(te parser.TypeExpr) types.Type {
	if te == nil {
		return types.I64
	}
	switch t := te.(type) {
	case *parser.FuncType:
		_ = t
		return types.I8Ptr // closure fat-pointer (SoyuzClosure*)
	case *parser.NamedType:
		switch t.Name {
		case "Int":
			return types.I64
		case "Float":
			return types.Double
		case "Bool":
			return types.I1
		case "String":
			return g.soyuzStringPtrType
		case "Char":
			return types.I32
		default:
			if si, ok := g.structs[t.Name]; ok {
				return types.NewPointer(si.typ)
			}
			if ei, ok := g.enums[t.Name]; ok {
				return types.NewPointer(ei.typ)
			}
		}
	case *parser.OptionalType:
		if ei, ok := g.enums["Option"]; ok {
			return types.NewPointer(ei.typ)
		}
		return types.I8Ptr
	case *parser.GenericType:
		switch t.Name {
		case "List":
			return types.NewPointer(g.structs["SoyuzList"].typ)
		case "Map":
			return types.NewPointer(g.structs["SoyuzMap"].typ)
		case "Option":
			if ei, ok := g.enums["Option"]; ok {
				return types.NewPointer(ei.typ)
			}
		case "Result":
			if ei, ok := g.enums["Result"]; ok {
				return types.NewPointer(ei.typ)
			}
		default:
			if decl, ok := g.genericRecordDecls[t.Name]; ok {
				sub := make(map[string]types.Type)
				for i, gp := range decl.Generics {
					if i < len(t.Params) {
						sub[gp.Name] = g.mapSoyuzTypeToLLVM(t.Params[i])
					}
				}
				if si, err := g.getOrCreateSpecializedRecord(decl, sub); err == nil {
					return types.NewPointer(si.typ)
				}
			}
			if decl, ok := g.genericEnumDecls[t.Name]; ok {
				sub := make(map[string]types.Type)
				for i, gp := range decl.Generics {
					if i < len(t.Params) {
						sub[gp.Name] = g.mapSoyuzTypeToLLVM(t.Params[i])
					}
				}
				if ei, err := g.getOrCreateSpecializedEnum(decl, sub); err == nil {
					return types.NewPointer(ei.typ)
				}
			}
		}
	case *parser.TupleType:
		if len(t.Elements) == 0 {
			return types.I64
		}
		elems := make([]types.Type, len(t.Elements))
		for i, e := range t.Elements {
			elems[i] = g.mapSoyuzTypeToLLVM(e)
		}
		return types.NewPointer(types.NewStruct(elems...))
	}
	return types.I64
}

func (g *Generator) mapTypeToLLVM(t checker.Type) types.Type {
	switch t.String() {
	case "Int":
		return types.I64
	case "Float":
		return types.Double
	case "Bool":
		return types.I1
	case "String":
		return g.soyuzStringPtrType
	case "Char":
		return types.I32
	case "Unit":
		return types.Void
	default:
		if st, ok := t.(*checker.SpecializedType); ok {
			if st.Base.String() == "Option" {
				return types.NewPointer(g.enums["Option"].typ)
			}
			if st.Base.String() == "Result" {
				return types.NewPointer(g.enums["Result"].typ)
			}
			// Lazy monomorphization for generic records
			if bt, ok := st.Base.(*checker.RecordType); ok {
				if decl, ok := g.genericRecordDecls[bt.Name]; ok {
					sub := make(map[string]types.Type)
					for i, gp := range decl.Generics {
						if i < len(st.Params) {
							sub[gp.Name] = g.mapTypeToLLVM(st.Params[i])
						}
					}
					si, err := g.getOrCreateSpecializedRecord(decl, sub)
					if err == nil {
						return types.NewPointer(si.typ)
					}
				}
			}
			// Lazy monomorphization for generic enums
			if et, ok := st.Base.(*checker.EnumType); ok {
				if decl, ok := g.genericEnumDecls[et.Name]; ok {
					sub := make(map[string]types.Type)
					for i, gp := range decl.Generics {
						if i < len(st.Params) {
							sub[gp.Name] = g.mapTypeToLLVM(st.Params[i])
						}
					}
					if ei, err := g.getOrCreateSpecializedEnum(decl, sub); err == nil {
						return types.NewPointer(ei.typ)
					}
				}
			}
			if ct, ok := st.Base.(*checker.ClassType); ok {
				if ct.Name == "List" {
					return types.NewPointer(g.structs["SoyuzList"].typ)
				}
				if ct.Name == "Map" {
					return types.NewPointer(g.structs["SoyuzMap"].typ)
				}
			}
		}
		if si, ok := g.structs[t.String()]; ok {
			return types.NewPointer(si.typ)
		}
		if ei, ok := g.enums[t.String()]; ok {
			return types.NewPointer(ei.typ)
		}
		if _, ok := t.(*checker.FuncType); ok {
			return types.I8Ptr
		}
		if _, ok := t.(*checker.InterfaceType); ok {
			return types.I8Ptr // interface values are fat pointers (SoyuzClosure{obj, vtable})
		}
		if _, ok := t.(*checker.ClassType); ok {
			if si, ok2 := g.structs[t.String()]; ok2 {
				return types.NewPointer(si.typ)
			}
		}
		if tt, ok := t.(*checker.TupleType); ok {
			if len(tt.Elements) == 0 {
				return types.I64
			}
			elems := make([]types.Type, len(tt.Elements))
			for i, e := range tt.Elements {
				elems[i] = g.mapTypeToLLVM(e)
			}
			return types.NewPointer(types.NewStruct(elems...))
		}
	}
	return types.I64
}
