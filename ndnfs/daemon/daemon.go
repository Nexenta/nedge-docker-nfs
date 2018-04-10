package daemon

import (
	log "github.com/sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
	"path/filepath"
)

var (
	defaultDir = filepath.Join(volume.DefaultDockerRootDirectory, "nedge")
)

func Start(cfgFile string, debug bool) {
	if debug {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}
	log.Info("Default docker root nedge: ", defaultDir)
	d := DriverAlloc(cfgFile)
	h := volume.NewHandler(d)
	log.Info("Driver Created, Handler Initialized")
	log.Info(h.ServeUnix("root", "ndnfs"))
}
