package ndnfscli

import (
	"fmt"
	"github.com/urfave/cli"
	"github.com/qeas/nedge-docker-nfs/ndnfs/driver"
	"github.com/docker/go-plugins-helpers/volume"
	log "github.com/Sirupsen/logrus"
	"path/filepath"
)

const socketAddress = "/run/docker/plugins/ndnfs.sock"

var	defaultDir = filepath.Join(volume.DefaultDockerRootDirectory, "ndnfs")

func NdnfsCmdNotFound(c *cli.Context, command string) {
	fmt.Println(command, " not found.");
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
		DaemonStartCmd,
	}
	return app
}

var (
	DaemonStartCmd = cli.Command{
		Name:  "start",
		Usage: "Start the Nedge Docker Daemon: `start [flags]`",
		Flags: []cli.Flag{
			cli.BoolFlag{
				Name:  "verbose, v",
				Usage: "Enable verbose/debug logging: `[--verbose]`",
			},
			cli.StringFlag{
				Name:  "config, c",
				Usage: "Config file for daemon (default: /opt/nedge/etc/ccow/ndnfs.json): `[--config /opt/nedge/etc/ccow/ndnfs.json]`",
			},
		},
		Action: cmdDaemonStart,
	}
)

func cmdDaemonStart(c *cli.Context) {
	verbose := c.Bool("verbose")
	cfg := c.String("config")
	if cfg == "" {
		cfg = "/opt/nedge/etc/ccow/ndnfs.json"
	}
	Start(cfg, verbose)
}

func Start(cfgFile string, debug bool) {
	if debug {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}
	log.Info("Default docker root ndnfs: ", defaultDir)
	d := driver.DriverAlloc(cfgFile)
	h := volume.NewHandler(d)
	log.Info("Driver Created, Handler Initialized")
	log.Info(h.ServeUnix(socketAddress, 0))
}
