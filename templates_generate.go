// +build ignore

package main

import (
	"log"
	"net/http"

	"github.com/shurcooL/vfsgen"
)

func main() {
	var Templates http.FileSystem = http.Dir("templates")
	err := vfsgen.Generate(Templates, vfsgen.Options{
		Filename:     "vfs/templates/templates_vfsdata.go",
		PackageName:  "templates",
		BuildTags:    "!dev",
		VariableName: "Templates",
	})
	if err != nil {
		log.Fatalln(err)
	}
}
