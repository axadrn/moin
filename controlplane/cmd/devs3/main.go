// devs3 is the S3 server of `task dev`. The app talks to it exactly as it
// talks to R2 or S3 in production; only S3_ENDPOINT and the keys differ. Its
// objects live in one file, so they survive restarts.
package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3bolt"
)

func main() {
	port := flag.String("port", "9000", "port on 127.0.0.1")
	file := flag.String("file", "tmp/devs3.db", "file that keeps the objects")
	flag.Parse()

	err := os.MkdirAll(filepath.Dir(*file), 0o755)
	if err != nil {
		log.Fatal(err)
	}
	backend, err := s3bolt.NewFile(*file)
	if err != nil {
		log.Fatal(err)
	}
	address := net.JoinHostPort("127.0.0.1", *port)
	log.Printf("dev S3 on http://%s, objects in %s", address, *file)
	log.Fatal(http.ListenAndServe(address, gofakes3.New(backend).Server()))
}
