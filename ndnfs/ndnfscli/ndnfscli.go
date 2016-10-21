package ndnfscli

import (
	"fmt"
	"github.com/urfave/cli"
)


func NdnfsCmdNotFound(c *cli.Context, command string) {
	fmt.Println(command, " not found ");

}

func NdnfsInitialize(c *cli.Context) error {

	cfgFile := c.GlobalString("config")
	if cfgFile != "" {
		fmt.Println("Found config: ", cfgFile);
	}
	return nil
}

func NewCli(version string) *cli.App {
	app := cli.NewApp()
	app.Name = "ndnfs"
	app.Version = version
	app.Author = "nexentaedge@nexenta.com"
	app.Usage = "CLI for NexentaEdge volumes"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   "loglevel",
			Value:  "info",
			Usage:  "Specifies the logging level (debug|warning|error)",
			EnvVar: "LogLevel",
		},
	}
	app.CommandNotFound = NdnfsCmdNotFound
	app.Before = NdnfsInitialize
	app.Commands = []cli.Command{
		DaemonCmd,
		VolumeCmd,
	}
	return app
}
