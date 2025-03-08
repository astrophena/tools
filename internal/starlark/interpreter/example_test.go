// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package interpreter_test

import (
	"context"
	"fmt"
	"log"

	"go.astrophena.name/tools/internal/starlark/interpreter"
	"go.astrophena.name/tools/internal/starlark/stdlib"
)

func ExampleInterpreter() {
	files := map[string]string{
		"hello.star": `hello()
`,
	}

	intr := &interpreter.Interpreter{
		Packages: map[string]interpreter.Loader{
			interpreter.MainPkg:   interpreter.MemoryLoader(files),
			interpreter.StdlibPkg: stdlib.Loader(),
		},
		Logger: func(file string, line int, message string) {
			fmt.Printf("[%s:%d] %s\n", file, line, message)
		},
	}
	if err := intr.Init(context.Background()); err != nil {
		log.Fatal(err)
	}
	if _, err := intr.ExecModule(context.Background(), interpreter.MainPkg, "hello.star"); err != nil {
		log.Fatal(err)
	}

	// Output: [@stdlib//builtins.star:11] Hello, world!
}
