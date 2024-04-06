//go:build js

//go:generate cp "$(go env GOROOT)/misc/wasm/wasm_exec.js" .

// play is a Nora playground, running on WebAssembly in browser.
//
// # Getting started
//
// Run:
//
//	$ make serve
//
// Open http://localhost:3000 in your browser.
package main

import (
	"fmt"
	"strings"
	"syscall/js"

	"go.astrophena.name/tools/internal/nora/lex"
	"go.astrophena.name/tools/internal/nora/token"
)

func run() js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) != 1 {
			return "no arguments passed"
		}

		doc := js.Global().Get("document")
		if !doc.Truthy() {
			return "unable to get document object"
		}
		outputArea := doc.Call("getElementById", "output")
		if !outputArea.Truthy() {
			return "unable to get output text area"
		}

		input := args[0].String()
		l := lex.New(input)

		var sb strings.Builder
		for tok := l.NextToken(); tok.Type != token.EOF; tok = l.NextToken() {
			fmt.Fprintf(&sb, "%+v\n", tok)
		}
		outputArea.Set("value", sb.String())
		return nil
	})
}

func main() {
	js.Global().Set("run", run())
	<-make(chan struct{})
}
