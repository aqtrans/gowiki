package main

import (
	"log"
//	"net/http"
        "jba.io/go/wiki/vfs/templates"
	"github.com/shurcooL/vfsgen"
)

func main() {
	err := vfsgen.Generate(templates.Templates, vfsgen.Options{
		PackageName:  "main",
		BuildTags:    "!dev",
		VariableName: "Templates",
	})
	if err != nil {
		log.Fatalln(err)
	}
}
