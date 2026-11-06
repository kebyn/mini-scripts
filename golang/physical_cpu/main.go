package main

import (
	"fmt"
	"log"

	"github.com/shirou/gopsutil/v3/cpu"
)

func main() {
	count, err := cpu.Counts(false)
	if err != nil {
		log.Fatalf("failed to get physical CPU count: %v", err)
	}
	fmt.Printf("Physical CPU cores: %d\n", count)
}
