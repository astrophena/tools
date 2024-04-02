package parse

import (
	"fmt"
	"testing"

	"go.astrophena.name/tools/internal/nora/ast"
	"go.astrophena.name/tools/internal/nora/lex"
	"go.astrophena.name/tools/internal/testutil"
)

func TestLetStatements(t *testing.T) {
	const input = `
let x = 5;
let y = 10;
let foobar = 838383;
`

	l := lex.New(input)
	p := New(l)

	prog, err := p.ParseProgram()
	if err != nil {
		t.Fatalf("ParseProgram(): %v", err)
	}
	if len(prog.Statements) != 3 {
		t.Fatalf("prog.Statements should contain 3 statements, got %d", len(prog.Statements))
	}

	tests := []struct {
		wantIdent string
	}{
		{"x"},
		{"y"},
		{"foobar"},
	}

	for i, tt := range tests {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			stmt := prog.Statements[i]
			testLetStatement(t, stmt, tt.wantIdent)
		})
	}
}

func testLetStatement(t *testing.T, s ast.Statement, name string) {
	testutil.AssertEqual(t, s.TokenLiteral(), "let")

	letStmt, ok := s.(*ast.LetStatement)
	if !ok {
		t.Fatalf("s not *ast.LetStatement, got %T", s)
	}

	testutil.AssertEqual(t, name, letStmt.Name.Value)
	testutil.AssertEqual(t, name, letStmt.Name.TokenLiteral())
}

func TestReturnStatements(t *testing.T) {
	const input = `
return 5;
return 10;
return 993322;
`

	l := lex.New(input)
	p := New(l)

	prog, err := p.ParseProgram()
	if err != nil {
		t.Fatalf("ParseProgram:() %v", err)
	}
	if len(prog.Statements) != 3 {
		t.Fatalf("prog.Statements should contain 3 statements, got %d", len(prog.Statements))
	}

	for _, stmt := range prog.Statements {
		returnStmt, ok := stmt.(*ast.ReturnStatement)
		if !ok {
			t.Errorf("stmt not *ast.ReturnStatement, got %T", stmt)
			continue
		}
		testutil.AssertEqual(t, "return", returnStmt.TokenLiteral())
	}
}

func TestIdentifierExpression(t *testing.T) {
	const input = "foobar;"

	l := lex.New(input)
	p := New(l)

	prog, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	if len(prog.Statements) != 1 {
		t.Fatalf("program should contain exactly one statement, instead got %d", len(prog.Statements))
	}

	stmt, ok := prog.Statements[0].(*ast.ExpressionStatement)
	if !ok {
		t.Fatalf("program statement is not ast.ExpressionStatement, got %T", prog.Statements[0])
	}

	ident, ok := stmt.Expression.(*ast.Identifier)
	if !ok {
		t.Fatalf("expression not *ast.Identifier, got %T", stmt.Expression)
	}
	testutil.AssertEqual(t, "foobar", ident.Value)
	testutil.AssertEqual(t, "foobar", ident.TokenLiteral())
}
