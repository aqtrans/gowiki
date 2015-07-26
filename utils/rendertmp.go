package main

import (
    "github.com/opennota/markdown"
    "io/ioutil"
    "log"    
    "io"
    "os"
)

func main() {
    filename := "./test.md"
    body, err := ioutil.ReadFile(filename)
    if err != nil {
		log.Println(err)
    }
    html, err := os.Create("./test.html")
    if err != nil {
        log.Println(err)
    }
	md := markdown.New(markdown.HTML(true), markdown.Nofollow(true), markdown.Breaks(true))
	mdr := md.RenderToString(body)
    mdrr, err := io.WriteString(html, mdr)
    if err != nil {
        log.Println(err)
        log.Println(mdrr)
    }
    html.Close()    
}