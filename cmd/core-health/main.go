package main

import (
	"flag"
	"fmt"
)

func main() {
	jsonFlag := flag.Bool("json", false, "output JSON")
	flag.Parse()

	root, _ := findProjectRoot()
	rep := BuildHealthReport(root, osRunner{})

	if *jsonFlag {
		fmt.Println(rep.JSON())
	} else {
		fmt.Print(rep.String())
	}
}
