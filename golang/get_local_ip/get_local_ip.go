package main

import (
	"fmt"
	"net"
	"strings"
)

func main() {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		fmt.Printf("%v\n", err)
		return
	}
	for _, addr := range addrs {
		address := strings.Split(addr.String(), "/")
		ip := strings.Split(address[0], ".")
		if len(ip) != 1 && ip[0] != "127" {
			if ip[0] != "169" && ip[0] != "254" {
				fmt.Printf("%v\n", address[0])
			}
		}

	}
	fmt.Println("Press the Enter Key to terminate the console screen!")
	fmt.Scanln()
}
