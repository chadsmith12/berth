package main

import (
	"os"

	"github.com/chadsmith12/berth/pkg/commands"
)

func main() {
	app := commands.NewRootApp()
	os.Exit(app.Execute(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
