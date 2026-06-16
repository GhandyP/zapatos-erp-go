package main

import (
	"log"
	"os"

	"zapatos-erp-go/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.New(os.Stderr, "zapatos-erp: ", log.LstdFlags).Fatal(err)
	}
}
