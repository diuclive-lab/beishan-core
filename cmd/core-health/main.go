package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	jsonFlag := flag.Bool("json", false, "output JSON")
	flag.Parse()

	root, _ := os.Getwd()
	rep := BuildHealthReport(root, osRunner{})

	if *jsonFlag {
		fmt.Println(rep.JSON())
	} else {
		fmt.Print(rep.String())
	}
}
