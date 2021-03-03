package main

import (
	"fmt"

	"github.com/shirou/gopsutil/v3/cpu"
)

func main() {

	vs, _ := cpu.Info()

	fmt.Printf("Physical CPU numbers: %v\n", vs[len(vs)-1].PhysicalID)

}
