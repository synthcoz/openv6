// mkicons writes the app icon files used by installers:
//
//	go run ./tools/mkicons -png openv6.png -ico openv6.ico
package main

import (
	"flag"
	"log"
	"os"

	"openv6/internal/icon"
)

func main() {
	pngPath := flag.String("png", "", "write a 256px PNG here")
	icoPath := flag.String("ico", "", "write a multi-size ICO here")
	flag.Parse()
	if *pngPath != "" {
		if err := os.WriteFile(*pngPath, icon.PNG(icon.StateBrand, 256), 0o644); err != nil {
			log.Fatal(err)
		}
	}
	if *icoPath != "" {
		if err := os.WriteFile(*icoPath, icon.ICO(icon.StateBrand, 16, 24, 32, 48, 64, 128, 256), 0o644); err != nil {
			log.Fatal(err)
		}
	}
}
