package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"io/ioutil"
	"log"

	"github.com/atotto/clipboard"
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

	markdownString := fmt.Sprintf(`<img alt="" src="data:image/png;base64,%s" />`, base64String)
	err = clipboard.WriteAll(markdownString)
	if err != nil {
		log.Panicf("write clipboard error: %v", err)
	}

	log.Printf("write clipboard sucessed.")
}
