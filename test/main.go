package main

import (
	"github.com/Nexenta/nedge-docker-nfs/ndnfs/driver"
//	volume "github.com/docker/go-plugins-helpers/volume"

	"fmt"
)

func main() {
	
	ndnfs, err := driver.DriverAlloc("/etc/ndnfs/ndnfs.json")
	if err != nil {
		fmt.Printf("Driver alloc failed, Error: %s\n", err)
		return
	}
	fmt.Printf("Config is : %+v\n", ndnfs.Config)
/*
	fmt.Printf("List volume")
	vmap, err := ndnfs.ListVolumes()
	//var vols []*volume.Volume
	if err != nil {
		fmt.Printf("Failed to retrieve volume list", err)
		return
	}

	fmt.Printf("Volumes: %+v\n", vmap)


	volumeID := "nfs01@clu1/ten1/ndnfs01" 
	nfsVolume, mountpoint, err := ndnfs.GetVolumeByID(volumeID)
	if err != nil {
                fmt.Printf("Failed to retrieve volume %s error:\n", volumeID, err)
                return
        }
	fmt.Printf("Volume: %+v\n", nfsVolume)
        fmt.Printf("Mountpoint: %+v\n", mountpoint)
	*/

	//services, err := ndnfs.ListServices()
	//fmt.Printf("Services: %+v", services)
	/*
	//request := &volume.CreateRequest{Name: "nfs01:clu1/ten1/ndnfs02", Options: make(map[string]string)}
	request := &volume.RemoveRequest{Name: "nfs01:clu1/ten1/ndnfs02"}
	//err = ndnfs.Create(request)
	err = ndnfs.Remove(request)

	if err != nil {
                fmt.Printf("Failed to retrieve volume %s error:\n", volumeID, err)
                return
        }
	*/

	fmt.Printf("Done \n")

}
