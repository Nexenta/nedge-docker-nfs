package main

import (
	"github.com/qeas/nedge-docker-nfs/ndnfs/ndnfscli"
	"os"
)

const (
	VERSION = "0.0.1"
)

func main() {
	ncli := ndnfscli.NewCli(VERSION)
	ncli.Run(os.Args)
}
