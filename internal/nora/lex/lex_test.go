package lex

import (
	"testing"

	"go.astrophena.name/tools/internal/nora/token"
)

func TestNextToken(t *testing.T) {
	const in = `let five = 5;
let ten = 10;

let add = fn(x, y) {
	x + y;
};

let result = add(five, ten);
`

	tests := []struct {
		wantType    token.Type
		wantLiteral string
	}{
		{token.Let, "let"},
		{token.Ident, "five"},
		{token.Assign, "="},
		{token.Int, "5"},
		{token.Semicolon, ";"},

		{token.Let, "let"},
		{token.Ident, "ten"},
		{token.Assign, "="},
		{token.Int, "10"},
		{token.Semicolon, ";"},

		{token.Let, "let"},
		{token.Ident, "add"},
		{token.Assign, "="},
		{token.Function, "fn"},
		{token.LeftParen, "("},
		{token.Ident, "x"},
		{token.Comma, ","},
		{token.Ident, "y"},
		{token.RightParen, ")"},
		{token.LeftBrace, "{"},
		{token.Ident, "x"},
		{token.Plus, "+"},
		{token.Ident, "y"},
		{token.Semicolon, ";"},
		{token.RightBrace, "}"},
		{token.Semicolon, ";"},

		{token.Let, "let"},
		{token.Ident, "result"},
		{token.Assign, "="},
		{token.Ident, "add"},
		{token.LeftParen, "("},
		{token.Ident, "five"},
		{token.Comma, ","},
		{token.Ident, "ten"},
		{token.RightParen, ")"},
		{token.Semicolon, ";"},

		{token.EOF, ""},
	}

	l := New(in)

	for i, tt := range tests {
		tok := l.NextToken()

		if tok.Type != tt.wantType {
			t.Fatalf("tests[%d]: want type %q, got %q", i, tt.wantType, tok.Type)
		}
		if tok.Literal != tt.wantLiteral {
			t.Fatalf("tests[%d]: want literal %q, got %q", i, tt.wantLiteral, tok.Literal)
		}
	}
}
