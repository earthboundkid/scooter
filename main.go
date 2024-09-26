package main

import (
	"os"

	"github.com/carlmjohnson/exitcode"
	"github.com/earthboundkid/scooter/mvfiles"
)

func main() {
	exitcode.Exit(mvfiles.CLI(os.Args[1:]))
}
