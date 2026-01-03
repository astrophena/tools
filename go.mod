module go.astrophena.name/tools

go 1.25.1

require (
	github.com/arl/statsviz v0.8.0
	github.com/dhowden/tag v0.0.0-20240417053706-3d75831295e8
	github.com/landlock-lsm/go-landlock v0.0.0-20250303204525-1544bccde3a3
	github.com/mmcdole/gofeed v1.3.0
	github.com/ncruces/go-sqlite3 v0.30.4
	github.com/tobischo/gokeepasslib/v3 v3.6.1
	go.astrophena.name/base v0.15.1-0.20260101134053-3ba1f7e085ba
	go.starlark.net v0.0.0-20250906160240-bf296ed553ea
	golang.org/x/sync v0.19.0
	golang.org/x/term v0.38.0
	rsc.io/markdown v0.0.0-20241212154241-6bf72452917f
)

require (
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/PuerkitoBio/goquery v1.8.0 // indirect
	github.com/andybalholm/cascadia v1.3.1 // indirect
	github.com/go4org/hashtriemap v0.0.0-20251130024219-545ba229f689 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/lmittmann/tint v1.1.2 // indirect
	github.com/mmcdole/goxpp v1.1.1-0.20240225020742-a0c311522b23 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/ncruces/julianday v1.0.0 // indirect
	github.com/ncruces/wbt v0.2.0 // indirect
	github.com/tetratelabs/wazero v1.11.0 // indirect
	github.com/tobischo/argon2 v0.1.0 // indirect
	golang.org/x/crypto v0.46.0 // indirect
	golang.org/x/exp/typeparams v0.0.0-20251219203646-944ab1f22d93 // indirect
	golang.org/x/mod v0.31.0 // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/text v0.32.0 // indirect
	golang.org/x/tools v0.40.0 // indirect
	google.golang.org/protobuf v1.34.1 // indirect
	honnef.co/go/tools v0.6.1 // indirect
	kernel.org/pub/linux/libs/security/libcap/psx v1.2.70 // indirect
)

// Doesn't exist in this repository.
retract [v0.0.1, v0.2.0]

tool (
	go.astrophena.name/base/devtools/addcopyright
	go.astrophena.name/base/devtools/pre-commit
	go.astrophena.name/tools/cmd/deploy
)

tool honnef.co/go/tools/cmd/staticcheck
