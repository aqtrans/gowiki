// +build ignore

package main

import (
	"log"
	"net/http"

	"github.com/shurcooL/vfsgen"
)

func main() {
	var Assets http.FileSystem = http.Dir("assets")
	err := vfsgen.Generate(Assets, vfsgen.Options{
		Filename:     "vfs/assets/assets_vfsdata.go",
		PackageName:  "assets",
		BuildTags:    "!dev",
		VariableName: "Assets",
	})
	if err != nil {
		log.Fatalln(err)
	}
}
