package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "wantastic-portal is no longer a standalone binary;")
	fmt.Fprintln(os.Stderr, "build and run wantastic-core instead (the portal lives in-process there).")
	os.Exit(1)
}
