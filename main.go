package main

import (
	"fmt"
	"image"
	_ "image/jpeg"
	"os"
	"time"
)

func main() {

	start := time.Now()
	f, _ := os.Open("1.jpg")
	img, _, _ := image.Decode(f)
	of, _ := os.Create("1.webp")
	encodeWebp(of, img, 33)
	of.Close()
	f.Close()

	fmt.Println(time.Now().Sub(start).Nanoseconds() / 1e6)
}
