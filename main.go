package main

import (
	"fmt"
	"os"

	"./xcleaner"
)

func help() {
	fmt.Println(`Use the command tool as following:
  -loop: execute every 10 minutes;
  -single: execute one time at once;
  (if no parameters specified, run as single by default)`)
	fmt.Println("\n")
}

func main() {
	if len(os.Args) == 1 {
		xcleaner.SingleCheck()
	} else if len(os.Args) == 2 {
		if os.Args[1] == "-l" || os.Args[1] == "-loop" {
			xcleaner.Loop()
		} else if os.Args[1] == "-s" || os.Args[1] == "-single" {
			xcleaner.SingleCheck()
		} else if os.Args[1] == "-h" || os.Args[1] == "-help" {
			help()
		} else {
			fmt.Println("Unknown parameter: " + os.Args[1])
			help()
		}
	} else {
		help()
	}
}
