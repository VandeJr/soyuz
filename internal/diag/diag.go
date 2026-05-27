package diag

import (
	"fmt"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

type Severity int

const (
	SeverityError Severity = iota
	SeverityWarning
)

type Diagnostic struct {
	File     string
	Start    lexer.Position
	End      lexer.Position
	Severity Severity
	Code     string
	Message  string
}

func (d Diagnostic) String() string {
	loc := d.Start.String()
	if d.File != "" {
		loc = fmt.Sprintf("[%s %s]", d.File, loc)
	}
	prefix := d.Code
	if prefix != "" {
		prefix += ": "
	}
	return fmt.Sprintf("%s%s%s", prefix, loc, d.Message)
}

func SpanEnd(start lexer.Position, width int) lexer.Position {
	if width < 1 {
		width = 1
	}
	return lexer.Position{Line: start.Line, Column: start.Column + width}
}

func FromParseErrors(file string, errs []parser.ParseError) []Diagnostic {
	out := make([]Diagnostic, len(errs))
	for i, e := range errs {
		end := e.End
		if end.Line == 0 && end.Column == 0 {
			end = SpanEnd(e.Position, 4)
		}
		out[i] = Diagnostic{
			File:     file,
			Start:    e.Position,
			End:      end,
			Severity: SeverityError,
			Code:     "E0100",
			Message:  e.Message,
		}
	}
	return out
}

func FromTypeErrors(errs []checker.TypeError) []Diagnostic {
	out := make([]Diagnostic, len(errs))
	for i, e := range errs {
		end := e.End
		if end.Line == 0 && end.Column == 0 {
			end = SpanEnd(e.Pos, 4)
		}
		code := e.Code
		if code == "" {
			code = "E0200"
		}
		out[i] = Diagnostic{
			File:     e.File,
			Start:    e.Pos,
			End:      end,
			Severity: SeverityError,
			Code:     code,
			Message:  e.Message,
		}
	}
	return out
}

func FromTypeWarnings(warns []checker.TypeWarning) []Diagnostic {
	out := make([]Diagnostic, len(warns))
	for i, w := range warns {
		end := w.End
		if end.Line == 0 && end.Column == 0 {
			end = SpanEnd(w.Pos, 4)
		}
		code := w.Code
		if code == "" {
			code = "W0300"
		}
		out[i] = Diagnostic{
			File:     w.File,
			Start:    w.Pos,
			End:      end,
			Severity: SeverityWarning,
			Code:     code,
			Message:  w.Message,
		}
	}
	return out
}

func Merge(all ...[]Diagnostic) []Diagnostic {
	var merged []Diagnostic
	for _, batch := range all {
		merged = append(merged, batch...)
	}
	return merged
}
