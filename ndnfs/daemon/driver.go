package daemon

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"sync"
	"github.com/docker/go-plugins-helpers/volume"
	"github.com/Nexenta/nedge-docker-nfs/ndnfs/ndnfsapi"
)

var (
	DN = "ndnfsdriver "
)

type NdnfsDriver struct {
	Scope		string
	DefaultVolSz	int64
	Client		*ndnfsapi.Client
	Mutex		*sync.Mutex
}

func DriverAlloc(cfgFile string) NdnfsDriver {

	client, _ := ndnfsapi.ClientAlloc(cfgFile)
	d := NdnfsDriver{
		Scope:			"local",
		DefaultVolSz:	1024,
		Client:         client,
		Mutex:          &sync.Mutex{},
	}
	return d
}

func (d NdnfsDriver) Capabilities(r volume.Request) volume.Response {
	log.Debug(DN, "Received Capabilities req")
	return volume.Response{Capabilities: volume.Capability{Scope: d.Scope}}
}

func (d NdnfsDriver) Create(r volume.Request) volume.Response {
	log.Debugf(fmt.Sprintf("Create volume %s on %s\n", r.Name, "nedge"))
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	err := d.Client.CreateVolume(
		r.Name, r.Options)
	if err != nil {
		return volume.Response{Err: err.Error()}
	}
	return volume.Response{}
}

func (d NdnfsDriver) Get(r volume.Request) volume.Response {
	log.Debug(DN, "Get volume: ", r.Name, " Options: ", r.Options)
	name, mnt, err := d.Client.GetVolume(r.Name)
	if err != nil || name == "" {
		log.Info("Volume with name ", r.Name, " not found")
		return volume.Response{}
	}
	log.Debug("Device mountpoint is: ", mnt)
	return volume.Response{Volume: &volume.Volume{
		Name: r.Name, Mountpoint: mnt}}
}

func (d NdnfsDriver) List(r volume.Request) volume.Response {
	log.Info(DN, "List volume: ", r.Name, " Options: ", r.Options)
	vmap, err := d.Client.ListVolumes()
	if err != nil {
		log.Info("Failed to retrieve volume list", err)
		return volume.Response{Err: err.Error()}
	}
	var vols []*volume.Volume
	for name, mnt := range vmap {
		if name != "" {
			vols = append(vols, &volume.Volume{Name: name, Mountpoint: mnt})
		}
	}
	return volume.Response{Volumes: vols}
}

func (d NdnfsDriver) Mount(r volume.MountRequest) volume.Response {
	log.Info(DN, "Mount volume: ", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	mnt, err := d.Client.MountVolume(r.Name)
	if err != nil {
		log.Info("Failed to mount volume named ", r.Name, ": ", err)
		return volume.Response{Err: err.Error()}
	}
	return volume.Response{Mountpoint: mnt}
}

func (d NdnfsDriver) Path(r volume.Request) volume.Response {
	log.Info(DN, "Path volume: ", r.Name, " Options: ", r.Options)
	mnt := fmt.Sprintf("%s%s", d.Client.Config.Mountpoint, r.Name)
	return volume.Response{Mountpoint: mnt}
}

func (d NdnfsDriver) Remove(r volume.Request) volume.Response {
	log.Info(DN, "Remove volume: ", r.Name, " Options: ", r.Options)
	d.Mutex.Lock()
	d.Client.DeleteVolume(r.Name)
	defer d.Mutex.Unlock()
	return volume.Response{}
}

func (d NdnfsDriver) Unmount(r volume.UnmountRequest) volume.Response {
	log.Info(DN, "Unmount volume: ", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	d.Client.UnmountVolume(r.Name)
	return volume.Response{}
}
