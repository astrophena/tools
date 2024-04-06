// Package lex implements the Nora lexer.
package lex

import "go.astrophena.name/tools/internal/nora/token"

// Lexer is the Nora lexer.
type Lexer struct {
	input   string
	pos     int  // current position in input (points to current char)
	readPos int  // current reading position in input (after current char)
	ch      rune // currently examined char
}

// New returns a new Lexer lexing input.
func New(input string) *Lexer {
	l := &Lexer{input: input}
	l.readChar()
	return l
}

// NextToken lexes the next token.
func (l *Lexer) NextToken() token.Token {
	var tok token.Token

	l.skipWhitespace()

	switch l.ch {
	case '=':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			lit := string(ch) + string(l.ch)
			tok = token.Token{Type: token.Eq, Literal: lit}
		} else {
			tok = newToken(token.Assign, l.ch)
		}
	case ';':
		tok = newToken(token.Semicolon, l.ch)
	case '(':
		tok = newToken(token.LeftParen, l.ch)
	case ')':
		tok = newToken(token.RightParen, l.ch)
	case ',':
		tok = newToken(token.Comma, l.ch)
	case '+':
		tok = newToken(token.Plus, l.ch)
	case '-':
		tok = newToken(token.Minus, l.ch)
	case '!':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			lit := string(ch) + string(l.ch)
			tok = token.Token{Type: token.Ne, Literal: lit}
		} else {
			tok = newToken(token.Bang, l.ch)
		}
	case '/':
		tok = newToken(token.Slash, l.ch)
	case '*':
		tok = newToken(token.Asterisk, l.ch)
	case '<':
		tok = newToken(token.Lt, l.ch)
	case '>':
		tok = newToken(token.Gt, l.ch)
	case '{':
		tok = newToken(token.LeftBrace, l.ch)
	case '}':
		tok = newToken(token.RightBrace, l.ch)
	case '"':
		tok.Type = token.String
		tok.Literal = l.readString()
	case 0:
		tok.Literal = ""
		tok.Type = token.EOF
	default:
		if isLetter(l.ch) {
			tok.Literal = l.readIdent()
			tok.Type = token.LookupIdent(tok.Literal)
			return tok
		} else if isDigit(l.ch) {
			tok.Literal = l.readNum()
			tok.Type = token.Int
			return tok
		}
		tok = newToken(token.Illegal, l.ch)
	}

	l.readChar()
	return tok
}

func newToken(typ token.Type, ch rune) token.Token {
	return token.Token{Type: typ, Literal: string(ch)}
}

func (l *Lexer) readChar() {
	if l.readPos >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = rune(l.input[l.readPos])
	}
	l.pos = l.readPos
	l.readPos += 1
}

func (l *Lexer) peekChar() rune {
	if l.readPos >= len(l.input) {
		return 0
	}
	return rune(l.input[l.readPos])
}

func (l *Lexer) readNum() string {
	pos := l.pos
	for isDigit(l.ch) {
		l.readChar()
	}
	return l.input[pos:l.pos]
}

func (l *Lexer) readIdent() string {
	pos := l.pos
	for isLetter(l.ch) {
		l.readChar()
	}
	return l.input[pos:l.pos]
}

func (l *Lexer) readString() string {
	pos := l.pos + 1
	for {
		l.readChar()
		if l.ch == '"' || l.ch == 0 {
			break
		}
	}
	return l.input[pos:l.pos]
}

func (l *Lexer) skipWhitespace() {
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\n' || l.ch == '\r' {
		l.readChar()
	}
}

func isLetter(ch rune) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch == '_'
}

func isDigit(ch rune) bool {
	return '0' <= ch && ch <= '9'
}
