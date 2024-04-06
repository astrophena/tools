//go:build ignore

package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	log.SetFlags(0)

	hostport := "localhost:3000"
	if len(os.Args) > 1 {
		hostport = os.Args[1]
	}

	log.Printf("Serving on %s.", hostport)
	if err := http.ListenAndServe(hostport, http.FileServerFS(os.DirFS("."))); err != nil {
		log.Fatal(err)
	}
}
