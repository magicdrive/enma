package cmd

import (
	"log"
	"os"

	"github.com/magicdrive/enma/internal/commandline"
	"github.com/magicdrive/enma/internal/common"
)

func Execute(version string) {
	if len(os.Args) >= 1 {
		common.EnmaHelpFunc()
		os.Exit(0)
	}

	err := commandline.Execute(version, os.Args[1:])
	if err != nil {
		log.Fatalf("Faital Error: %v\n", err)
	}
}
