//go:build !linux

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "ecompeg2ts-tc is only supported on Linux")
	os.Exit(2)
}
