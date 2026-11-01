module go.astrophena.name/tools

go 1.27

require (
	crawshaw.dev/jsonfile v0.0.0-20240206193014-699d1dad804e
	github.com/a-h/templ v0.3.1070
	github.com/dhowden/tag v0.0.0-20240417053706-3d75831295e8
	github.com/landlock-lsm/go-landlock v0.11.0
	github.com/microcosm-cc/bluemonday v1.0.27
	github.com/mmcdole/gofeed v1.5.0
	github.com/tailscale/sqlite v0.0.0-20260910121735-acbe2dadf94c
	github.com/tobischo/gokeepasslib/v3 v3.7.1
	go.astrophena.name/base v0.26.0
	go.starlark.net v0.0.0-20261021141710-4f4267fb140d
	golang.org/x/mod v0.42.0
	golang.org/x/term v0.47.0
	rsc.io/markdown v0.0.0-20241212154241-6bf72452917f
)

require (
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/a-h/parse v0.0.0-20250122154542-74294addb73e // indirect
	github.com/andybalholm/brotli v1.2.7 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cli/browser v1.3.0 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/go4org/hashtriemap v0.0.0-20260925222741-44e5305f85d9 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/lmittmann/tint v1.2.1 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mmcdole/goxpp/v2 v2.0.0 // indirect
	github.com/natefinch/atomic v1.0.1 // indirect
	github.com/restic/chunker v0.5.0 // indirect
	github.com/tobischo/argon2 v0.2.0 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/exp/typeparams v0.0.0-20260820142414-ca536658362e // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	golang.org/x/tools v0.51.0 // indirect
	honnef.co/go/tools v0.8.2 // indirect
	kernel.org/pub/linux/libs/security/libcap/psx v1.2.78 // indirect
)

// Doesn't exist in this repository.
retract [v0.0.1, v0.2.0]

tool (
	github.com/a-h/templ/cmd/templ
	go.astrophena.name/base/devtools/addcopyright
	go.astrophena.name/base/devtools/deploy
	go.astrophena.name/base/devtools/pre-commit
)

tool honnef.co/go/tools/cmd/staticcheck
