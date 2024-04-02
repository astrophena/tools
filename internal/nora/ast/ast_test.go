package ast

import (
	"testing"

	"go.astrophena.name/tools/internal/nora/token"
	"go.astrophena.name/tools/internal/testutil"
)

func TestString(t *testing.T) {
	prog := &Program{
		Statements: []Statement{
			&LetStatement{
				Token: token.Token{Type: token.Let, Literal: "let"},
				Name: &Identifier{
					Token: token.Token{Type: token.Ident, Literal: "myVar"},
					Value: "myVar",
				},
				Value: &Identifier{
					Token: token.Token{Type: token.Ident, Literal: "anotherVar"},
					Value: "anotherVar",
				},
			},
		},
	}

	testutil.AssertEqual(t, "let myVar = anotherVar;", prog.String())
}
