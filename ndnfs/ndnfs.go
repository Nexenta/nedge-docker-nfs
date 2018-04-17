package main

import (
	"os"

	"github.com/Nexenta/nedge-docker-nfs/ndnfs/ndnfsapi"
	"github.com/Nexenta/nedge-docker-nfs/ndnfs/ndnfscli"
)

const (
	VERSION = "0.0.1"
)

var (
	client *ndnfsapi.Client
)

func main() {
	ncli := ndnfscli.NewCli(VERSION)
	ncli.Run(os.Args)
}
