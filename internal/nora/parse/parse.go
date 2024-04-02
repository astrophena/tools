// Package parse parses Nora programs into a syntax tree.
package parse

import (
	"errors"
	"fmt"

	"go.astrophena.name/tools/internal/nora/ast"
	"go.astrophena.name/tools/internal/nora/lex"
	"go.astrophena.name/tools/internal/nora/token"
)

type (
	prefixParseFunc func() ast.Expression
	infixParseFunc  func(ast.Expression) ast.Expression
)

// Parser implements a Nora program parser.
type Parser struct {
	l *lex.Lexer

	curToken  token.Token
	peekToken token.Token

	errors []error // encountered during parsing

	prefixParseFuncs map[token.Type]prefixParseFunc
	infixParseFuncs  map[token.Type]infixParseFunc
}

func (p *Parser) registerPrefix(typ token.Type, f prefixParseFunc) {
	p.prefixParseFuncs[typ] = f
}

func (p *Parser) registerInfix(typ token.Type, f infixParseFunc) {
	p.infixParseFuncs[typ] = f
}

// New returns a new Parser.
func New(l *lex.Lexer) *Parser {
	p := &Parser{l: l}

	p.nextToken()
	p.nextToken()

	return p
}

func (p *Parser) Errors() []error { return p.errors }

func (p *Parser) peekError(t token.Type) {
	p.errors = append(p.errors, fmt.Errorf("expected next token to be %s, got %s instead", t, p.peekToken.Type))
}

func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()
}

func (p *Parser) ParseProgram() (*ast.Program, error) {
	prog := &ast.Program{}
	prog.Statements = []ast.Statement{}

	for p.curToken.Type != token.EOF {
		stmt := p.parseStatement()
		if stmt != nil {
			prog.Statements = append(prog.Statements, stmt)
		}
		p.nextToken()
	}

	errs := p.Errors()
	if len(errs) > 0 {
		// If we got a single error, it's better to return it directly.
		if len(errs) == 1 {
			return nil, errs[0]
		}
		return nil, errors.Join(errs...)
	}

	return prog, nil
}

func (p *Parser) parseStatement() ast.Statement {
	switch p.curToken.Type {
	case token.Let:
		return p.parseLetStatement()
	case token.Return:
		return p.parseReturnStatement()
	default:
		return nil
	}
}

func (p *Parser) parseLetStatement() *ast.LetStatement {
	stmt := &ast.LetStatement{Token: p.curToken}

	if !p.expectPeek(token.Ident) {
		return nil
	}

	stmt.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

	if !p.expectPeek(token.Assign) {
		return nil
	}

	for !p.curTokenIs(token.Semicolon) {
		p.nextToken()
	}

	return stmt
}

func (p *Parser) parseReturnStatement() *ast.ReturnStatement {
	stmt := &ast.ReturnStatement{Token: p.curToken}

	p.nextToken()

	for !p.curTokenIs(token.Semicolon) {
		p.nextToken()
	}

	return stmt
}

func (p *Parser) curTokenIs(t token.Type) bool {
	return p.curToken.Type == t
}

func (p *Parser) peekTokenIs(t token.Type) bool {
	return p.peekToken.Type == t
}

func (p *Parser) expectPeek(t token.Type) bool {
	if p.peekTokenIs(t) {
		p.nextToken()
		return true
	}
	p.peekError(t)
	return false
}
