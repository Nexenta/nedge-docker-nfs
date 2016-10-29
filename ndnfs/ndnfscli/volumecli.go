package ndnfscli

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/urfave/cli"
	"github.com/Nexenta/nedge-docker-nfs/ndnfs/ndnfsapi"
)

var (
	VolumeCmd =  cli.Command{
		Name:  "volume",
		Usage: "Volume related commands",
		Subcommands: []cli.Command{
			VolumeCreateCmd,
			VolumeDeleteCmd,
			VolumeListCmd,
		},
	}

	VolumeCreateCmd = cli.Command{
		Name:  "create",
		Usage: "create a new volume: `create [options] NAME`",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "tenant",
				Usage: "tenant path to create bucket in (cluster/tenant)",
			},
			cli.BoolFlag{
				Name:  "verbose, v",
				Usage: "Enable verbose/debug logging: `[--verbose]`",
			},
		},
		Action: cmdCreateVolume,
	}
	VolumeDeleteCmd = cli.Command{
		Name:  "delete",
		Usage: "delete an existing volume: `delete NAME`",
		Flags: []cli.Flag{
			cli.BoolFlag{
				Name:  "verbose, v",
				Usage: "Enable verbose/debug logging: `[--verbose]`",
			},
		},
		Action: cmdDeleteVolume,
	}
	VolumeListCmd = cli.Command{
		Name:  "list",
		Usage: "list existing volumes",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "range",
				Value: "",
				Usage: ": range of volume`",
			},
			cli.BoolFlag{
				Name:  "verbose, v",
				Usage: "Enable verbose/debug logging: `[--verbose]`",
			},
		},
		Action: cmdListVolume,
	}

)

func getClient(c *cli.Context) (client *ndnfsapi.Client) {
	cfg := c.String("config")
	if cfg == "" {
		cfg = "/opt/nedge/etc/ccow/ndnfs.json"
	}
	if c.Bool("v") == true {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}
	
	client, _ = ndnfsapi.ClientAlloc(cfg)
	return client
}

func cmdCreateVolume(c *cli.Context) cli.ActionFunc {
	name := c.Args().First()
	if name == "" {
		log.Error("Provide volume name as first argument")
		return nil
	}
	fmt.Println("cmdCreate: ", name, c.String("size"));
	client := getClient(c)
	client.CreateVolume(name, c.String("tenant"))
	return nil
}

func cmdDeleteVolume(c *cli.Context) cli.ActionFunc {
	name := c.Args().First()
	fmt.Println("cmdDelete: ", name);
	client := getClient(c)
	client.DeleteVolume(name)
	return nil
}

func cmdListVolume(c *cli.Context) cli.ActionFunc {
	fmt.Println("cmdListVolume");
	client := getClient(c)
	vlist, _ := client.ListVolumes()
	fmt.Println(vlist)
	return nil
}
