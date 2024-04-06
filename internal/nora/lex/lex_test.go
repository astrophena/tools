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

!-/*5;
5 < 10 > 5;

if (5 < 10) {
	return true;
} else {
	return false;
}

10 == 10;
10 != 9;
"foobar"
"foo bar"
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

		{token.Bang, "!"},
		{token.Minus, "-"},
		{token.Slash, "/"},
		{token.Asterisk, "*"},
		{token.Int, "5"},
		{token.Semicolon, ";"},

		{token.Int, "5"},
		{token.Lt, "<"},
		{token.Int, "10"},
		{token.Gt, ">"},
		{token.Int, "5"},
		{token.Semicolon, ";"},

		{token.If, "if"},
		{token.LeftParen, "("},
		{token.Int, "5"},
		{token.Lt, "<"},
		{token.Int, "10"},
		{token.RightParen, ")"},
		{token.LeftBrace, "{"},
		{token.Return, "return"},
		{token.True, "true"},
		{token.Semicolon, ";"},
		{token.RightBrace, "}"},
		{token.Else, "else"},
		{token.LeftBrace, "{"},
		{token.Return, "return"},
		{token.False, "false"},
		{token.Semicolon, ";"},
		{token.RightBrace, "}"},

		{token.Int, "10"},
		{token.Eq, "=="},
		{token.Int, "10"},
		{token.Semicolon, ";"},

		{token.Int, "10"},
		{token.Ne, "!="},
		{token.Int, "9"},
		{token.Semicolon, ";"},

		{token.String, "foobar"},
		{token.String, "foo bar"},

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
