// Package token defines constants representing the lexical tokens of Nora.
package token

//go:generate go run golang.org/x/tools/cmd/stringer -type=Type

// Type represents a token type.
type Type int

// Token represents a token.
type Token struct {
	Type    Type
	Literal string
}

// Token types. Their integer representations may change. Don't rely on them.
const (
	Illegal Type = iota
	EOF

	// Identifiers and literals.
	Ident   // identifiers
	Int     // integers
	String  // strings
	Comment // comments

	// Operators.
	Assign   // =
	Plus     // +
	Minus    // -
	Bang     // !
	Asterisk // *
	Slash    // /
	Lt       // <
	Gt       // >
	Eq       // ==
	Ne       // !=

	// Delimiters.
	Comma      // ,
	Semicolon  // ;
	LeftParen  // (
	RightParen // )
	LeftBrace  // {
	RightBrace // }

	// Keywords.
	Function // fn
	Let      // let
	True     // true
	False    // false
	If       // if
	Else     // else
	Return   // return
)

var keywords = map[string]Type{
	"fn":     Function,
	"let":    Let,
	"true":   True,
	"false":  False,
	"if":     If,
	"else":   Else,
	"return": Return,
}

// LookupIdent returns a token type for the identifier.
func LookupIdent(ident string) Type {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return Ident
}
