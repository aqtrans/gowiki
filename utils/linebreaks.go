package main

import (
	"bytes"
	"io/ioutil"
	"log"
)

func main() {
	b, err := ioutil.ReadFile("crlf")
	if err != nil {
		log.Fatalln(err)
	}
	if bytes.Contains(b, []byte("\r\n")) {
		log.Println("crlf detected")
	}
}
