package version

import (
	"flag"
	"os"
	"testing"

	"go.astrophena.name/base/testutil"
)

var update = flag.Bool("update", false, "update golden files in testdata")

func TestInfo_String(t *testing.T) {
	testutil.RunGolden(t, "testdata/*.json", func(t *testing.T, match string) []byte {
		b, err := os.ReadFile(match)
		if err != nil {
			t.Fatal(err)
		}
		i := testutil.UnmarshalJSON[Info](t, b)
		return []byte(i.String())
	}, *update)
}
