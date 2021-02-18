package main

import (
	"embed"
	"log"

	"fmt"
)

//go:embed data
var f embed.FS

func main() {
	d, err := f.ReadFile("data/number")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(d))
	d, err = f.ReadFile("data/country")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(d))
}
