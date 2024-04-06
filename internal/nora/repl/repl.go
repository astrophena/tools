// Package repl implements the Nora REPL.
package repl

import (
	"bufio"
	"context"
	"fmt"
	"io"

	"go.astrophena.name/tools/internal/nora/lex"
	"go.astrophena.name/tools/internal/nora/token"
)

const prompt = ">> "

// Start starts the Nora REPL, reading input from r, and writing to w.
func Start(ctx context.Context, r io.Reader, w io.Writer) error {
	s := bufio.NewScanner(r)

	fmt.Fprintf(w, prompt)

	for s.Scan() {
		line := s.Text()
		l := lex.New(line)

		for tok := l.NextToken(); tok.Type != token.EOF; tok = l.NextToken() {
			fmt.Fprintf(w, "%+v\n", tok)
		}

		fmt.Fprintf(w, prompt)
	}

	fmt.Fprintf(w, "\n")

	return s.Err()
}
