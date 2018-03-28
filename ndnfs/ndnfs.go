package main

import (
	"github.com/Nexenta/nedge-docker-nfs/ndnfs/ndnfscli"
	"os"
)

const (
	VERSION = "0.0.1"
)

func main() {
	ncli := ndnfscli.NewCli(VERSION)
	ncli.Run(os.Args)
}
