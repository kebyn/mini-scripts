package main

import (
	"flag"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"time"
)

func listDir(name string) {
	now := time.Now()
	files, err := ioutil.ReadDir(name)
	if err != nil {
		log.Fatalf("List \t%v error: %v", name, err)
	}

	for _, file := range files {
		fileName := filepath.Join(name, file.Name())
		if file.IsDir() {
			listDir(fileName)
		}
		if now.Sub(file.ModTime()) > (time.Duration(7*24) * time.Hour) {
			err := os.Remove(fileName)
			if err != nil {
				log.Fatalf("Remove \t%v error: %v", fileName, err)
			}
			log.Printf("Remove \t%v created by %v", fileName, file.ModTime())
		}
	}
}

func main() {
	var path string
	flag.StringVar(&path, "p", "./", "monitoring path")
	flag.Parse()
	listDir(path)
}
