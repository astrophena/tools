// Package format implements standard formatting of Nora source.
package format

import (
	"go.astrophena.name/tools/internal/nora/lex"
	"go.astrophena.name/tools/internal/nora/parse"
)

// Source formats src in canonical style and returns the result or an (I/O or
// syntax) error. src is expected to be a syntactically correct Nora source
// file, or a list of Nora declarations or statements.
func Source(src []byte) ([]byte, error) {
	l := lex.New(string(src))
	p := parse.New(l)

	prog, err := p.ParseProgram()
	if err != nil {
		return nil, err
	}

	return []byte(prog.String()), nil
}
