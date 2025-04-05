package cmd

import (
	"log"
	"os"

	"github.com/magicdrive/enma/internal/commandline"
)

func Execute(version string) {
	err := commandline.Execute(version, os.Args[1:])
	if err != nil {
		log.Fatalf("Faital Error: %v\n", err)
	}
}
