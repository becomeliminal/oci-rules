// Command registry serves an in-memory OCI registry on a free loopback port
// for the plugin's signing tests, and writes host:port to the file named by
// its one argument once it is listening. It serves until killed.
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/google/go-containerregistry/pkg/registry"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: registry <address file>")
		os.Exit(2)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[1], []byte(listener.Addr().String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := http.Serve(listener, registry.New()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
