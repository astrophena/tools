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
	stmt := parseExpressionStatement(t, input)

	ident, ok := stmt.Expression.(*ast.Identifier)
	if !ok {
		t.Fatalf("expression not *ast.Identifier, got %T", stmt.Expression)
	}
	testutil.AssertEqual(t, "foobar", ident.Value)
	testutil.AssertEqual(t, "foobar", ident.TokenLiteral())
}

func TestStringExpression(t *testing.T) {
	const input = `"hello world";`
	stmt := parseExpressionStatement(t, input)

	sl, ok := stmt.Expression.(*ast.StringLiteral)
	if !ok {
		t.Fatalf("expression not *ast.StringLiteral, got %T", stmt.Expression)
	}
	testutil.AssertEqual(t, "hello world", sl.Value)
}

func parseExpressionStatement(t *testing.T, input string) *ast.ExpressionStatement {
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

	return stmt
}

func TestParsingPrefixExpressions(t *testing.T) {
	cases := []struct {
		in  string
		op  string
		val int64
	}{
		{"!5;", "!", 5},
		{"-15;", "-", 15},
	}

	for i, tc := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			stmt := parseExpressionStatement(t, tc.in)

			exp, ok := stmt.Expression.(*ast.PrefixExpression)
			if !ok {
				t.Fatalf("stmt is not *ast.PrefixExpression, got %T", stmt.Expression)
			}

			testutil.AssertEqual(t, tc.op, exp.Operator)
			testIntegerLiteral(t, exp.Right, tc.val)
		})
	}
}

func testIntegerLiteral(t *testing.T, il ast.Expression, value int64) {
	integ, ok := il.(*ast.IntegerLiteral)
	if !ok {
		t.Errorf("il not *ast.IntegerLiteral. got %T", il)
	}
	testutil.AssertEqual(t, value, integ.Value)
	testutil.AssertEqual(t, fmt.Sprintf("%d", value), integ.TokenLiteral())
}

func TestParsingInfixExpressions(t *testing.T) {
	cases := []struct {
		in       string
		leftVal  int64
		op       string
		rightVal int64
	}{
		{"5 + 5;", 5, "+", 5},
		{"5 - 5;", 5, "-", 5},
		{"5 * 5;", 5, "*", 5},
		{"5 / 5;", 5, "/", 5},
		{"5 > 5;", 5, ">", 5},
		{"5 < 5;", 5, "<", 5},
		{"5 == 5;", 5, "==", 5},
		{"5 != 5;", 5, "!=", 5},
	}

	for i, tc := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			stmt := parseExpressionStatement(t, tc.in)

			exp, ok := stmt.Expression.(*ast.InfixExpression)
			if !ok {
				t.Fatalf("stmt is not *ast.InfixExpression, got %T", stmt.Expression)
			}

			testIntegerLiteral(t, exp.Left, tc.leftVal)
			testutil.AssertEqual(t, tc.op, exp.Operator)
			testIntegerLiteral(t, exp.Right, tc.rightVal)
		})
	}
}

func TestOperatorPrecedenceParsing(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			"-a * b",
			"((-a) * b)",
		},
		{
			"!-a",
			"(!(-a))",
		},
		{
			"a + b + c",
			"((a + b) + c)",
		},
		{
			"a + b - c",
			"((a + b) - c)",
		},
		{
			"a * b * c",
			"((a * b) * c)",
		},
		{
			"a * b / c",
			"((a * b) / c)",
		},
		{
			"a + b / c",
			"(a + (b / c))",
		},
		{
			"a + b * c + d / e - f",
			"(((a + (b * c)) + (d / e)) - f)",
		},
		{
			"3 + 4; -5 * 5",
			"(3 + 4)((-5) * 5)",
		},
		{
			"5 > 4 == 3 < 4",
			"((5 > 4) == (3 < 4))",
		},
		{
			"5 < 4 != 3 > 4",
			"((5 < 4) != (3 > 4))",
		},
		{
			"3 + 4 * 5 == 3 * 1 + 4 * 5",
			"((3 + (4 * 5)) == ((3 * 1) + (4 * 5)))",
		},
		{
			"true",
			"true",
		},
		{
			"false",
			"false",
		},
		{
			"3 > 5 == false",
			"((3 > 5) == false)",
		},
		{
			"3 < 5 == true",
			"((3 < 5) == true)",
		},
	}

	for i, tc := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			l := lex.New(tc.in)
			p := New(l)

			prog, err := p.ParseProgram()
			if err != nil {
				t.Fatal(err)
			}

			testutil.AssertEqual(t, tc.want, prog.String())
		})
	}
}
