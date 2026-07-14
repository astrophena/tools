// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package flatpak

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	boot "go.astrophena.name/tools/cmd/boot/internal"
	"go.astrophena.name/tools/cmd/boot/internal/testutil"
)

func TestUpdate(t *testing.T) {
	cases := map[string]struct {
		updates       string
		dryRun        bool
		want          boot.Result
		wantApplyCall bool
	}{
		"applies updates": {
			updates:       "org.example.App",
			want:          boot.ResultChange,
			wantApplyCall: true,
		},
		"dry run does not apply": {
			updates: "org.example.App",
			dryRun:  true,
			want:    boot.ResultChange,
		},
		"skips without updates": {want: boot.ResultSkip},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			log := filepath.Join(t.TempDir(), "flatpak.log")
			testutil.Commands(t, map[string]string{
				"flatpak": `#!/bin/sh
echo "$@" >> "` + log + `"
case "$*" in
"remote-ls --updates") echo "` + tc.updates + `"; exit 0 ;;
"update -y") exit 0 ;;
*) exit 1 ;;
esac
`,
			})

			h := testutil.NewTask(t, "test")
			m := &impl{}
			action := h.EmitOne("flatpak.update", m.update, nil, nil)
			got, err := action.Apply(t.Context(), tc.dryRun)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("result = %s, want %s", got, tc.want)
			}
			output, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(string(output), "update -y"); got != tc.wantApplyCall {
				t.Fatalf("update call = %v, want %v:\n%s", got, tc.wantApplyCall, output)
			}
		})
	}
}
