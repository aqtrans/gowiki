package main

import (
    "jba.io/go/utils"
    "log"
)

func main() {

log.Println(utils.RandKey(32))
log.Println(utils.RandKey(64))

}
