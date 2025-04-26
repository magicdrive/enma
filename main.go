package main

import (
	"github.com/magicdrive/enma/internal/common"

	cmd "github.com/magicdrive/enma/cmd/enma"
)

var version string

func main() {
	cmd.Execute(common.Version())
}
