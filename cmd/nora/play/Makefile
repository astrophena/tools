build:
	@ GOOS=js GOARCH=wasm go build -o nora.wasm

clean:
	@ rm -f nora.wasm

serve: build
	@ go run serve.go

.PHONY: build clean serve
