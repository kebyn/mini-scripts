package main

import (
	"fmt"
	"os"
	"strconv"
)

// Bytes2Human converts byte count into a human-readable string (B, KB, MB, GB, TB, PB).
func Bytes2Human(inBytes int64) string {
	suffixes := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	suffix := 0
	for inBytes > 9999 && suffix < len(suffixes) {
		inBytes = inBytes >> 10
		suffix++
	}

	if suffix >= len(suffixes) {
		suffix--
	}
	return fmt.Sprintf("%d%s", inBytes, suffixes[suffix])
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <bytes> [bytes...]\n", os.Args[0])
		os.Exit(1)
	}
	for _, arg := range os.Args[1:] {
		val, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid byte count %q: %v\n", arg, err)
			os.Exit(1)
		}
		fmt.Println(Bytes2Human(val))
	}
}
