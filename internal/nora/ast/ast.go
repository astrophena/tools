// Package ast contains types for constructing Nora program syntax trees.
package ast

import (
	"strings"

	"go.astrophena.name/tools/internal/nora/token"
)

// Node represents an AST node.
type Node interface {
	TokenLiteral() string
	String() string
}

type Statement interface {
	Node
	statementNode()
}

type Expression interface {
	Node
	expressionNode()
}

// Program is a root AST node, representing the Nora program.
type Program struct {
	Statements []Statement
}

func (p *Program) TokenLiteral() string {
	if len(p.Statements) > 0 {
		return p.Statements[0].TokenLiteral()
	}
	return ""
}

func (p *Program) String() string {
	var sb strings.Builder

	for _, s := range p.Statements {
		sb.WriteString(s.String())
	}

	return sb.String()
}

type LetStatement struct {
	Token token.Token // token.Let
	Name  *Identifier
	Value Expression
}

func (ls *LetStatement) statementNode()       {}
func (ls *LetStatement) TokenLiteral() string { return ls.Token.Literal }

func (ls *LetStatement) String() string {
	var sb strings.Builder

	sb.WriteString(ls.TokenLiteral() + " ")
	sb.WriteString(ls.Name.String())
	sb.WriteString(" = ")

	if ls.Value != nil {
		sb.WriteString(ls.Value.String())
	}

	sb.WriteString(";")

	return sb.String()
}

type Identifier struct {
	Token token.Token // token.Ident
	Value string
}

func (i *Identifier) expressionNode()      {}
func (i *Identifier) TokenLiteral() string { return i.Token.Literal }
func (i *Identifier) String() string       { return i.Value }

type ReturnStatement struct {
	Token       token.Token // token.Return
	ReturnValue Expression
}

func (rs *ReturnStatement) statementNode()       {}
func (rs *ReturnStatement) TokenLiteral() string { return rs.Token.Literal }

func (rs *ReturnStatement) String() string {
	var sb strings.Builder

	sb.WriteString(rs.TokenLiteral() + " ")

	if rs.ReturnValue != nil {
		sb.WriteString(rs.ReturnValue.String())
	}

	sb.WriteString(";")

	return sb.String()
}

type ExpressionStatement struct {
	Token      token.Token // the first of the expression
	Expression Expression
}

func (es *ExpressionStatement) statementNode()       {}
func (es *ExpressionStatement) TokenLiteral() string { return es.Token.Literal }

func (es *ExpressionStatement) String() string {
	if es.Expression != nil {
		return es.Expression.String()
	}
	return ""
}

type StringLiteral struct {
	Token token.Token
	Value string
}

func (sl *StringLiteral) expressionNode()      {}
func (sl *StringLiteral) TokenLiteral() string { return sl.Token.Literal }
func (sl *StringLiteral) String() string       { return sl.Token.Literal }

type CommentStatement struct {
	Token token.Token // token.Comment
	Value string
}

func (cs *CommentStatement) statementNode()       {}
func (cs *CommentStatement) TokenLiteral() string { return cs.Token.Literal }
func (cs *CommentStatement) String() string       { return "// " + cs.Token.Literal + "\n" }

type PrefixExpression struct {
	Token    token.Token // prefix token, e.g. !
	Operator string
	Right    Expression
}

func (pe *PrefixExpression) expressionNode()      {}
func (pe *PrefixExpression) TokenLiteral() string { return pe.Token.Literal }
func (pe *PrefixExpression) String() string {
	var sb strings.Builder

	sb.WriteString("(")
	sb.WriteString(pe.Operator)
	sb.WriteString(pe.Right.String())
	sb.WriteString(")")

	return sb.String()
}

type InfixExpression struct {
	Token    token.Token // The operator token, e.g. +
	Left     Expression
	Operator string
	Right    Expression
}

func (ie *InfixExpression) expressionNode()      {}
func (ie *InfixExpression) TokenLiteral() string { return ie.Token.Literal }
func (ie *InfixExpression) String() string {
	var sb strings.Builder

	sb.WriteString("(")
	sb.WriteString(ie.Left.String())
	sb.WriteString(" " + ie.Operator + " ")
	sb.WriteString(ie.Right.String())
	sb.WriteString(")")

	return sb.String()
}

type IntegerLiteral struct {
	Token token.Token
	Value int64
}

func (il *IntegerLiteral) expressionNode()      {}
func (il *IntegerLiteral) TokenLiteral() string { return il.Token.Literal }
func (il *IntegerLiteral) String() string       { return il.Token.Literal }

type Bool struct {
	Token token.Token
	Value bool
}

func (b *Bool) expressionNode()      {}
func (b *Bool) TokenLiteral() string { return b.Token.Literal }
func (b *Bool) String() string       { return b.Token.Literal }
