// Package parse parses Nora programs into a syntax tree.
package parse

import (
	"go.astrophena.name/tools/internal/nora/ast"
	"go.astrophena.name/tools/internal/nora/lex"
	"go.astrophena.name/tools/internal/nora/token"
)

// Parser implements a Nora program parser.
type Parser struct {
	l *lex.Lexer

	curToken  token.Token
	peekToken token.Token
}

// New returns a new Parser.
func New(l *lex.Lexer) *Parser {
	p := &Parser{l: l}

	p.nextToken()
	p.nextToken()

	return p
}

func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()
}

func (p *Parser) ParseProgram() *ast.Program {
	return nil
}
