// Package parse parses Nora programs into a syntax tree.
package parse

import (
	"errors"
	"fmt"
	"strconv"

	"go.astrophena.name/tools/internal/nora/ast"
	"go.astrophena.name/tools/internal/nora/lex"
	"go.astrophena.name/tools/internal/nora/token"
)

// Precedence in expression.
const (
	_ int = iota
	lowest
	equals      // ==
	lessGreater // > or <
	sum         // +
	product     // *
	prefix      // -X or !X
	call        // myFunction(x)
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

	p.prefixParseFuncs = make(map[token.Type]prefixParseFunc)
	p.registerPrefix(token.Ident, p.parseIdentifier)
	p.registerPrefix(token.String, p.parseString)
	p.registerPrefix(token.Int, p.parseIntegerLiteral)
	p.registerPrefix(token.Bang, p.parsePrefixExpression)
	p.registerPrefix(token.Minus, p.parsePrefixExpression)

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

func (p *Parser) errorf(format string, args ...any) {
	p.errors = append(p.errors, fmt.Errorf(format, args...))
}

func (p *Parser) parseStatement() ast.Statement {
	switch p.curToken.Type {
	case token.Comment:
		return p.parseCommentStatement()
	case token.Let:
		return p.parseLetStatement()
	case token.Return:
		return p.parseReturnStatement()
	default:
		return p.parseExpressionStatement()
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

func (p *Parser) parseCommentStatement() *ast.CommentStatement {
	return &ast.CommentStatement{Token: p.curToken, Value: p.curToken.Literal}
}

func (p *Parser) parseReturnStatement() *ast.ReturnStatement {
	stmt := &ast.ReturnStatement{Token: p.curToken}

	p.nextToken()

	for !p.curTokenIs(token.Semicolon) {
		p.nextToken()
	}

	return stmt
}

func (p *Parser) parseExpressionStatement() *ast.ExpressionStatement {
	stmt := &ast.ExpressionStatement{Token: p.curToken}

	stmt.Expression = p.parseExpression(lowest)

	if p.peekTokenIs(token.Semicolon) {
		p.nextToken()
	}

	return stmt
}

func (p *Parser) parseExpression(precedence int) ast.Expression {
	prefix := p.prefixParseFuncs[p.curToken.Type]
	if prefix == nil {
		p.errorf("no prefix parse function for %s found", p.curToken.Type)
		return nil
	}
	leftExp := prefix()

	return leftExp
}

func (p *Parser) parsePrefixExpression() ast.Expression {
	expr := &ast.PrefixExpression{
		Token:    p.curToken,
		Operator: p.curToken.Literal,
	}

	p.nextToken()

	expr.Right = p.parseExpression(prefix)

	return expr
}

func (p *Parser) parseIdentifier() ast.Expression {
	return &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
}

func (p *Parser) parseIntegerLiteral() ast.Expression {
	lit := &ast.IntegerLiteral{Token: p.curToken}
	value, err := strconv.ParseInt(p.curToken.Literal, 0, 64)
	if err != nil {
		p.errorf("could not parse %q as integer", p.curToken.Literal)
		return nil
	}
	lit.Value = value
	return lit
}

func (p *Parser) parseString() ast.Expression {
	return &ast.StringLiteral{Token: p.curToken, Value: p.curToken.Literal}
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
