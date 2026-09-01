package commands

import "github.com/chadsmith12/berth/pkg/cli"

func NewRootApp() *cli.App {
	app := cli.NewApp("berth")
	app.AddCommand(NewInitCommand())
	return app
}
