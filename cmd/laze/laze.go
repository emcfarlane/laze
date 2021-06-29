// Implements a laze builder.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/emcfarlane/laze"
)

// laze fmt: https://github.com/bazelbuild/buildtools/blob/master/buildifier2/buildifier2.go

// args
func main() {
	flag.Parse()

	args := flag.Args()

	switch len(args) {
	case 0:
		log.Fatal("Argument missing")
	case 1:
		log.Fatal("Arguments missing")
		//filename := flag.Args()[0]
		//ast, err := syntax.Parse(filename, nil, syntax.RetainComments)
		//if err != nil {
		//	log.Fatalf("%+v\n", err)
		//}
		//newAst := convertast.ConvFile(ast)
		//fmt.Print(build.FormatString(newAst))
	case 2:
		ctx := context.Background()
		switch args[0] {
		case "build":
			b := laze.Builder{}

			_, err := b.Build(ctx, args[1])
			if err != nil {
				log.Fatal(err)
			}

		case "run":
			// TODO: how to run?
			// provider is a single file?

		default:
			// TODO:
			log.Fatal("Unknown argument")
		}

	default:
		//log.Fatal("want at most one Skylark file name")
	}
}
