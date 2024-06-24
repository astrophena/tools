package httputil

import "testing"

func TestEscapeForJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty string", in: "", want: ""},
		{name: "basic string", in: "Hello, world!", want: "Hello, world!"},
		{name: "escape backslash", in: "This has a \\ backslash", want: "This has a \\\\ backslash"},
		{name: "escape quotes", in: "He said, \"Hello\"!", want: "He said, \\\"Hello\\\"!"},
		{name: "escape control character (tab)", in: "This has a tab\tcharacter", want: "This has a tab\\\tcharacter"},
		{name: "escape control character (newline)", in: "This has a newline\ncharacter", want: "This has a newline\\\ncharacter"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := escapeForJSON(tc.in)
			if got != tc.want {
				t.Errorf("escapeForJSON(%q): want %q, got %q", tc.in, tc.want, got)
			}
		})
	}
}
