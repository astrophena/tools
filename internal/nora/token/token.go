// Package token defines constants representing the lexical tokens of Nora.
package token

// Type represents a token type.
type Type string

// Token represents a token.
type Token struct {
	Type    Type
	Literal string
}

// Token types.
const (
	Illegal Type = "ILLEGAL"
	EOF     Type = "EOF"

	// Identifiers and literals.
	Ident Type = "IDENT" // identifiers
	Int   Type = "INT"   // integers

	// Operators.
	Assign Type = "="
	Plus   Type = "+"

	// Delimiters.
	Comma      Type = ","
	Semicolon  Type = ";"
	LeftParen  Type = "("
	RightParen Type = ")"
	LeftBrace  Type = "{"
	RightBrace Type = "}"

	// Keywords.
	Function Type = "FUNCTION"
	Let      Type = "LET"
)

var keywords = map[string]Type{
	"fn":  Function,
	"let": Let,
}

// LookupIdent returns a token type for the identifier.
func LookupIdent(ident string) Type {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return Ident
}
