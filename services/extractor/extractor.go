package main

import (
	"fmt"
	"log"

	"github.com/otiai10/gosseract/v2"
)

func main() {
	client := gosseract.NewClient()
	defer client.Close()
	err := client.SetImage("/tmp/image.png")
	if err != nil {
		log.Fatal("Super error: ", err)
	}

	text, err := client.Text()
	if err != nil {
		log.Fatal("Super error: ", err)
	}
	fmt.Println(text)
}
