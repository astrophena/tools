module go.astrophena.name/tools

go 1.24.2

require (
	github.com/dhowden/tag v0.0.0-20240417053706-3d75831295e8
	github.com/golang-jwt/jwt/v5 v5.2.2
	github.com/google/go-cmp v0.7.0
	github.com/landlock-lsm/go-landlock v0.0.0-20250303204525-1544bccde3a3
	github.com/lmittmann/tint v1.1.2
	github.com/mmcdole/gofeed v1.3.0
	github.com/muesli/reflow v0.3.0
	github.com/tobischo/gokeepasslib/v3 v3.6.1
	go.astrophena.name/base v0.8.2-0.20250724161251-08d4d0c48b1b
	go.starlark.net v0.0.0-20250623223156-8bf495bf4e9a
	golang.org/x/term v0.33.0
	rsc.io/markdown v0.0.0-20241212154241-6bf72452917f
)

require (
	github.com/BurntSushi/toml v1.4.1-0.20240526193622-a339e1f7089c // indirect
	github.com/PuerkitoBio/goquery v1.8.0 // indirect
	github.com/andybalholm/cascadia v1.3.1 // indirect
	github.com/benbjohnson/hashfs v0.2.2 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/mattn/go-runewidth v0.0.12 // indirect
	github.com/mmcdole/goxpp v1.1.1-0.20240225020742-a0c311522b23 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	github.com/tobischo/argon2 v0.1.0 // indirect
	golang.org/x/crypto v0.38.0 // indirect
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678 // indirect
	golang.org/x/mod v0.25.0 // indirect
	golang.org/x/net v0.40.0 // indirect
	golang.org/x/sync v0.15.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
	golang.org/x/text v0.26.0 // indirect
	golang.org/x/tools v0.33.0 // indirect
	google.golang.org/protobuf v1.34.1 // indirect
	honnef.co/go/tools v0.6.0 // indirect
	kernel.org/pub/linux/libs/security/libcap/psx v1.2.70 // indirect
)

// Doesn't exist in this repository.
retract [v0.0.1, v0.2.0]

tool honnef.co/go/tools/cmd/staticcheck

tool (
	go.astrophena.name/tools/internal/devtools/addcopyright
	go.astrophena.name/tools/internal/devtools/genreadme
	go.astrophena.name/tools/internal/devtools/new
	go.astrophena.name/tools/internal/devtools/pre-commit
)
