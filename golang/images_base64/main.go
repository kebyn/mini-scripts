package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/atotto/clipboard"
)

func detectImageMime(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	default:
		return "image/png"
	}
}

func main() {
	var filename string
	flag.StringVar(&filename, "f", "", "picture file name")
	flag.Parse()

	if filename == "" {
		log.Fatal("please specify a picture file using -f <filename>")
	}

	buffer, err := os.ReadFile(filename)
	if err != nil {
		log.Fatalf("open file error: %v", err)
	}

	base64String := base64.StdEncoding.EncodeToString(buffer)
	mimeType := detectImageMime(filename)

	markdownString := fmt.Sprintf(`<img alt="%s" src="data:%s;base64,%s" />`, filepath.Base(filename), mimeType, base64String)
	if err := clipboard.WriteAll(markdownString); err != nil {
		log.Fatalf("write clipboard error: %v", err)
	}

	log.Printf("write clipboard succeeded.")
}
