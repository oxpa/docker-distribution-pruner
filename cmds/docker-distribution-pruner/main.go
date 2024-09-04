package main

import (
	"os"

	"github.com/oxpa/docker-distribution-pruner/experimental"
)

func main() {
	env := os.Getenv("EXPERIMENTAL")
	if env == "true" || env == "1" {
		experimental.Main()
		return
	}

	println("Use old experimental set of the features with `EXPERIMENTAL=true`")
	os.Exit(1)
}
