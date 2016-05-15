package main

import (
	"flag"
	"log"
	"os"

	"github.com/gadumitrachioaiei/goequal/equal"
)

func main() {
	typeName := flag.String("type", "", "Type to generate Equal function for")
	pkgName := flag.String("package", "", "Package type is part of")
	stdOut := flag.Bool("stdout", false, "Print to stdout")
	flag.Parse()
	if *typeName == "" || *pkgName == "" {
		log.Println("You have to specify type and package")
		os.Exit(2)
	}
	generator := equal.NewGenerator(*pkgName, *typeName, *stdOut, nil)
	generator.Generate()
}
