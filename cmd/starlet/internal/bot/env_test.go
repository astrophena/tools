// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package bot_test

import (
	"os"
	"testing"
)

func TestEnvironmentDocumentation(t *testing.T) {
	const docFile = "doc.md"

	b := testBot(t, testMux(t, nil), map[string]string{
		"bot.star": ``, // a dummy bot.star is needed for Load
	})
	got := b.Documentation()

	if *update {
		if err := os.WriteFile(docFile, []byte(got), 0o644); err != nil {
			t.Fatalf("failed to update file: %v", err)
		}
		t.Logf("updated file: %s", docFile)
	}

	want, err := os.ReadFile(docFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	if string(want) != got {
		t.Errorf("Documentation() output does not match file.\nGot:\n%s\n\nWant:\n%s", got, string(want))
		t.Logf("To update the file, run: go test -v ./... -update.")
	}
}
