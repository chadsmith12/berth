package commands

import "github.com/chadsmith12/berth/pkg/cli"

func NewRootApp() *cli.App {
	app := cli.NewApp("berth")
	app.AddCommand(NewGenerateCommand())
	app.AddCommand(NewLaunchCommand())
	app.AddCommand(NewDeployCommand())
	app.AddCommand(NewStatusCommand())
	app.AddCommand(NewLogsCommand())
	app.AddCommand(NewAuthCommand())
	return app
}
