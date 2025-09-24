module go.astrophena.name/tools

go 1.25

require (
	github.com/arl/statsviz v0.7.1
	github.com/dchest/uniuri v1.2.0
	github.com/dhowden/tag v0.0.0-20240417053706-3d75831295e8
	github.com/fsnotify/fsnotify v1.9.0
	github.com/golang-jwt/jwt/v5 v5.3.0
	github.com/google/go-cmp v0.7.0
	github.com/landlock-lsm/go-landlock v0.0.0-20250303204525-1544bccde3a3
	github.com/mmcdole/gofeed v1.3.0
	github.com/tailscale/sqlite v0.0.0-20250822145721-1673cdf564b7
	github.com/tobischo/gokeepasslib/v3 v3.6.1
	go.astrophena.name/base v0.12.5
	go.starlark.net v0.0.0-20250906160240-bf296ed553ea
	golang.org/x/term v0.35.0
	golang.org/x/time v0.13.0
	rsc.io/markdown v0.0.0-20241212154241-6bf72452917f
)

require (
	github.com/BurntSushi/toml v1.4.1-0.20240526193622-a339e1f7089c // indirect
	github.com/PuerkitoBio/goquery v1.8.0 // indirect
	github.com/andybalholm/cascadia v1.3.1 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/lmittmann/tint v1.1.2 // indirect
	github.com/mmcdole/goxpp v1.1.1-0.20240225020742-a0c311522b23 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/tobischo/argon2 v0.1.0 // indirect
	golang.org/x/crypto v0.41.0 // indirect
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678 // indirect
	golang.org/x/mod v0.27.0 // indirect
	golang.org/x/net v0.43.0 // indirect
	golang.org/x/sync v0.17.0 // indirect
	golang.org/x/sys v0.36.0 // indirect
	golang.org/x/text v0.29.0 // indirect
	golang.org/x/tools v0.36.0 // indirect
	golang.org/x/tools/go/expect v0.1.1-deprecated // indirect
	google.golang.org/protobuf v1.34.1 // indirect
	honnef.co/go/tools v0.6.0 // indirect
	kernel.org/pub/linux/libs/security/libcap/psx v1.2.70 // indirect
)

// Doesn't exist in this repository.
retract [v0.0.1, v0.2.0]

tool (
	go.astrophena.name/base/devtools/addcopyright
	go.astrophena.name/base/devtools/pre-commit
)

tool honnef.co/go/tools/cmd/staticcheck
