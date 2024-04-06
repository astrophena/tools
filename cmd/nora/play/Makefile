build:
	@ GOOS=js GOARCH=wasm go build -o nora.wasm

clean:
	@ rm -f nora.wasm

serve: build
	@ python3 -m http.server 3000

.PHONY: build clean serve
