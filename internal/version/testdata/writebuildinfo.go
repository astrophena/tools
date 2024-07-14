//go:build ignore

package main

import (
	"debug/buildinfo"
	"encoding/json"
	"log"
	"os"
)

func main() {
	log.SetFlags(0)
	args := os.Args[1:]
	if len(args) < 2 {
		log.Fatal("Usage: go run writebuildinfo.go <binary> <path>")
	}
	var (
		binary = args[0]
		path   = args[1]
	)
	bi, err := buildinfo.ReadFile(binary)
	if err != nil {
		log.Fatal(err)
	}
	b, err := json.MarshalIndent(bi, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		log.Fatal(err)
	}
}
