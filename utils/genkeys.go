package main

import (
	"log"

	"jba.io/go/httputils"
)

func main() {

	log.Println(httputils.RandKey(32))
	log.Println(httputils.RandKey(64))

}
