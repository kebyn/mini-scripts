package main

import (
	"encoding/base64"
	"flag"
	"io/ioutil"
	"log"
)

func main() {
	var filename string
	flag.StringVar(&filename, "f", "", "picture file name")
	flag.Parse()

	buffer, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Panicf("open file error: %v", err)
	}

	base64String := base64.StdEncoding.EncodeToString(buffer)

	log.Printf("%v", base64String)

}
