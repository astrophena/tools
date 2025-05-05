// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package interpreter_test

import (
	"context"
	"fmt"
	"log"

	"go.astrophena.name/tools/internal/starlark/interpreter"
)

func ExampleInterpreter() {
	files := map[string]string{
		"hello.star": `print("Hello, world!")`,
	}

	intr := &interpreter.Interpreter{
		Packages: map[string]interpreter.Loader{
			interpreter.MainPkg: interpreter.MemoryLoader(files),
		},
		Logger: func(_ string, _ int, message string) {
			fmt.Printf("%s\n", message)
		},
	}
	if err := intr.Init(context.Background()); err != nil {
		log.Fatal(err)
	}
	if _, err := intr.ExecModule(context.Background(), interpreter.MainPkg, "hello.star"); err != nil {
		log.Fatal(err)
	}

	// Output: Hello, world!
}
