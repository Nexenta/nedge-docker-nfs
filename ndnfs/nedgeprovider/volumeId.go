package nedgeprovider

import (
	"fmt"
	"strings"
)

type VolumeID struct {
	Service string
	Cluster string
	Tenant  string
	Bucket  string
}

func NewVolumeID(service string, cluster string, tenant, string, bucket string) *VolumeID {
	volID := new(VolumeID)
	volID.Service = service
	volID.Cluster = cluster
	volID.Tenant = tenant
	volID.Bucket = bucket

	return volID
}

func ParseVolumeID(volumeID string, configOptions map[string]string) (resultObject VolumeID, missedParts map[string]bool, err error) {
	parts := strings.Split(volumeID, "@")

	// object path elements like cluster/tenant/bucket
	var pathObjects []string
	if len(parts) < 2 {
		// no service notation
		if service, ok := configOptions["service"]; ok {
			resultObject.Service = service
		}
		pathObjects = strings.Split(parts[0], "/")
	} else {
		resultObject.Service = parts[0]
		if resultObject.Service == "" {
			if service, ok := configOptions["service"]; ok {
				resultObject.Service = service
			}
		}
		pathObjects = strings.Split(parts[1], "/")
	}

	// bucket only
	if len(pathObjects) == 1 {
		if cluster, ok := configOptions["cluster"]; ok {
			resultObject.Cluster = cluster
		}

		if tenant, ok := configOptions["tenant"]; ok {
			resultObject.Tenant = tenant
		}

		resultObject.Bucket = pathObjects[0]
	} else if len(pathObjects) == 2 {
		// tenant and bucket only

		if cluster, ok := configOptions["cluster"]; ok {
			resultObject.Cluster = cluster
		}

		resultObject.Tenant = pathObjects[0]
		if resultObject.Tenant == "" {
			if tenant, ok := configOptions["tenant"]; ok {
				resultObject.Tenant = tenant
			}
		}

		resultObject.Bucket = pathObjects[1]
	} else {
		// cluster, tenant and bucket

		//Cluster
		resultObject.Cluster = pathObjects[0]
		if resultObject.Cluster == "" {
			if cluster, ok := configOptions["cluster"]; ok {
				resultObject.Cluster = cluster
			}
		}

		//Tenant
		resultObject.Tenant = pathObjects[1]
		if resultObject.Tenant == "" {
			if tenant, ok := configOptions["tenant"]; ok {
				resultObject.Tenant = tenant
			}
		}

		//Bucket
		resultObject.Bucket = pathObjects[2]
	}

	missedParts, err = resultObject.Validate()

	return resultObject, missedParts, err
}

func (volumeID *VolumeID) Validate() (map[string]bool, error) {

	missingParts := make(map[string]bool)
	if volumeID.Service == "" {
		missingParts["service"] = true
	}

	if volumeID.Cluster == "" {
		missingParts["cluster"] = true
	}

	if volumeID.Tenant == "" {
		missingParts["tenant"] = true
	}

	if volumeID.Bucket == "" {
		missingParts["bucket"] = true
	}

	if len(missingParts) > 0 {
		missingString := "["
		for key := range missingParts {
			missingString += " " + key
		}
		missingString += " ]"

		err := fmt.Errorf("VolumeID are missing %s values(s), check volume naming or your ndnfs.json options", missingString)
		return missingParts, err
	}
	return missingParts, nil
}

// fully specified bucket path
func (path *VolumeID) String() string {
	return fmt.Sprintf("%s@%s/%s/%s", path.Service, path.Cluster, path.Tenant, path.Bucket)
}

// To compare buckets
func (path *VolumeID) FullObjectPath() string {
	return fmt.Sprintf("%s/%s/%s", path.Cluster, path.Tenant, path.Bucket)
}

// for driver list
func (path *VolumeID) MinimalObjectPath() string {
	return fmt.Sprintf("%s/%s", path.Tenant, path.Bucket)
}

func (path *VolumeID) MountPointObjectPath() string {
	return fmt.Sprintf("%s-%s-%s", path.Cluster, path.Tenant, path.Bucket)
}
