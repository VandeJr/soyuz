package lsp

import (
	"encoding/json"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"soyuz/internal/parser"
)

// LSP 3.17 inlay hint types — not present in glsp v0.2.2 (which covers 3.16).

type InlayHintParams struct {
	TextDocument protocol.TextDocumentIdentifier `json:"textDocument"`
	Range        protocol.Range                  `json:"range"`
}

type InlayHintKind int

const (
	InlayHintKindType      InlayHintKind = 1
	InlayHintKindParameter InlayHintKind = 2
)

type InlayHint struct {
	Position    protocol.Position `json:"position"`
	Label       string            `json:"label"`
	Kind        InlayHintKind     `json:"kind,omitempty"`
	PaddingLeft bool              `json:"paddingLeft,omitempty"`
}

const methodInlayHint = "textDocument/inlayHint"

// Handle overrides the embedded protocol.Handler.Handle to intercept
// textDocument/inlayHint (LSP 3.17), which glsp 3.16 does not know about.
func (h *Handler) Handle(ctx *glsp.Context) (r any, validMethod bool, validParams bool, err error) {
	if ctx.Method == methodInlayHint {
		var params InlayHintParams
		if err = json.Unmarshal(ctx.Params, &params); err != nil {
			return nil, true, false, err
		}
		hints, err := h.handleInlayHints(ctx, &params)
		return hints, true, true, err
	}
	return h.Handler.Handle(ctx)
}

func (h *Handler) handleInlayHints(_ *glsp.Context, params *InlayHintParams) ([]InlayHint, error) {
	result := h.engine.Get(params.TextDocument.URI)
	if result == nil {
		return nil, nil
	}

	var hints []InlayHint
	walkAST(result.AST, func(n parser.Node) {
		vd, ok := n.(*parser.VarDecl)
		if !ok || vd.Type != nil || vd.Name == "" {
			return
		}
		t, ok := result.Check.NodeTypes[n]
		if !ok {
			return
		}
		// Place the hint right after the variable name.
		pos := toLSPPosition(vd.NamePos)
		pos.Character += protocol.UInteger(len(vd.Name))
		hints = append(hints, InlayHint{
			Position:    pos,
			Label:       ": " + t.String(),
			Kind:        InlayHintKindType,
			PaddingLeft: false,
		})
	})
	return hints, nil
}
