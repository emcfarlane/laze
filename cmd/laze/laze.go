// Implements a laze builder.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/emcfarlane/laze"
)

// TODO: add support for fmt starlark files on build.
// laze fmt: https://github.com/bazelbuild/buildtools/blob/master/buildifier2/buildifier2.go

func run() error {
	flag.Parse()

	args := flag.Args()

	if len(args) < 1 {
		return fmt.Errorf("missing label")
	}

	label := args[len(args)-1]
	args = args[:len(args)-1]

	b := laze.Builder{}

	ctx := context.Background()
	a, err := b.Build(ctx, args, label)
	if err != nil {
		return err
	}

	// Report error on failed actions.
	if err := a.FailureErr(); err != nil {
		return err
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
