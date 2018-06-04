package driver

import (
	"fmt"

	"github.com/Nexenta/nedge-docker-nfs/ndnfs/nedgeprovider"
	log "github.com/Sirupsen/logrus"
)

type NfsServiceData struct {
	Service    nedgeprovider.NedgeService
	NfsVolumes []nedgeprovider.NedgeNFSVolume
}

type ClusterData struct {
	nfsServicesData []NfsServiceData
}

func (nfsServiceData *NfsServiceData) FindNFSVolumeByVolumeID(volumeID nedgeprovider.VolumeID) (resultNfsVolume nedgeprovider.NedgeNFSVolume, err error) {

	for _, nfsVolume := range nfsServiceData.NfsVolumes {
		if nfsVolume.VolumeID.FullObjectPath() == volumeID.FullObjectPath() {
			return nfsVolume, nil
		}
	}
	return resultNfsVolume, fmt.Errorf("Can't find NFS volume by VolumeID : %+v\n", volumeID)
}

func (nfsServiceData *NfsServiceData) GetNFSVolumeAndEndpoint(volumeID nedgeprovider.VolumeID) (nfsVolume nedgeprovider.NedgeNFSVolume, endpoint string, err error) {
	nfsVolume, err = nfsServiceData.FindNFSVolumeByVolumeID(volumeID)
	if err != nil {
		return nfsVolume, "", err
	}

	return nfsVolume, fmt.Sprintf("%s:%s", nfsServiceData.Service.Network[0], nfsVolume.Share), err
}

/*FindApropriateService find service with minimal export count*/
func (clusterData ClusterData) FindApropriateServiceData() (*NfsServiceData, error) {

	var minService *NfsServiceData

	if len(clusterData.nfsServicesData) > 0 {
		minService = &clusterData.nfsServicesData[0]

		for _, data := range clusterData.nfsServicesData[1:] {
			currentValue := len(data.NfsVolumes)
			if len(minService.NfsVolumes) > currentValue {
				minService = &data
			}
		}
	} else {
		return minService, fmt.Errorf("No NFS Services available along nedge cluster")
	}

	return minService, nil
}

func (clusterData ClusterData) FindServiceDataByVolumeID(volumeID nedgeprovider.VolumeID) (result *NfsServiceData, err error) {
	log.Debug(DN, "FindServiceDataByVolumeID ")

	for _, data := range clusterData.nfsServicesData {
		for _, nfsVolume := range data.NfsVolumes {
			if nfsVolume.Path == volumeID.FullObjectPath() {
				return &data, nil
			}
		}
	}

	return nil, fmt.Errorf("Can't find NFS service data by VolumeID %s", volumeID)
}

/*FillNfsVolumes Fills outer volumes hashmap, format {VolumeID: volume nfs endpoint} */
func (clusterData ClusterData) FillNfsVolumes(vmap map[string]string, defaultCluster string) {

	for _, data := range clusterData.nfsServicesData {
		for _, nfsVolume := range data.NfsVolumes {

			var volumePath string
			if defaultCluster != "" && nfsVolume.VolumeID.Cluster == defaultCluster {
				volumePath = nfsVolume.VolumeID.MinimalObjectPath()
			} else {
				volumePath = nfsVolume.VolumeID.FullObjectPath()
			}
			vname := volumePath
			vmap[vname] = fmt.Sprintf("%s:%s", data.Service.Network[0], nfsVolume.Share)
		}
	}
}

/* FindNfsServiceData finfs and returns pointer to NfsServiceData stored in ClusterData */
func (clusterData ClusterData) FindNfsServiceData(serviceName string) (serviceData *NfsServiceData, err error) {
	for _, serviceData := range clusterData.nfsServicesData {
		if serviceData.Service.Name == serviceName {
			return &serviceData, nil
		}
	}

	return nil, fmt.Errorf("Can't find Service Data by name %s", serviceName)
}
