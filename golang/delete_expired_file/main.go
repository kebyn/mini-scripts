package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"time"
)

func cleanExpiredFiles(dir string, expireDuration time.Duration) {
	now := time.Now()
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("ReadDir %v error: %v", dir, err)
		return
	}

	for _, entry := range entries {
		fullPath := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			cleanExpiredFiles(fullPath, expireDuration)
			continue
		}
		info, err := entry.Info()
		if err != nil {
			log.Printf("Stat %v error: %v", fullPath, err)
			continue
		}
		if now.Sub(info.ModTime()) > expireDuration {
			if err := os.Remove(fullPath); err != nil {
				log.Printf("Remove %v error: %v", fullPath, err)
				continue
			}
			log.Printf("Removed %v (modified at %v)", fullPath, info.ModTime())
		}
	}
}

func main() {
	var (
		path string
		days int
	)
	flag.StringVar(&path, "p", "./", "monitoring directory path")
	flag.IntVar(&days, "d", 7, "expiration threshold in days")
	flag.Parse()

	expireDuration := time.Duration(days*24) * time.Hour
	cleanExpiredFiles(path, expireDuration)
}
