package driver

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/Nexenta/nedge-docker-nfs/ndnfs/nedgeprovider"
	log "github.com/Sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
)

const defaultChunkSize int = 1048576
const defaultMountPoint string = "/var/lib/ndnfs"

var (
	DN = "ndnfsdriver "
)

type NdnfsDriver struct {
	Scope        string
	DefaultVolSz int64
	Mutex        *sync.Mutex
	Config       *Config
	Nedge        nedgeprovider.INexentaEdgeProvider
}

type Config struct {
	Name                string
	Nedgerest           string
	Nedgeport           int16
	Cluster             string
	Chunksize           int
	Username            string
	Password            string
	Mountpoint          string
	ForceBucketDeletion bool            `json:"forceBucketDeletion"`
	ServiceFilter       string          `json:"serviceFilter"`
	ServiceFilterMap    map[string]bool `json:"-"`
}

func ReadParseConfig(fname string) (config Config) {
	content, err := ioutil.ReadFile(fname)
	if err != nil {
		msg := fmt.Sprintf("Error reading config file: %s , Error: %s \n", fname, err)
		log.Fatal(DN, msg, err)
	}
	var conf Config
	err = json.Unmarshal(content, &conf)
	if err != nil {
		msg := fmt.Sprintf("Error parsing config file: %s, Error: %s \n ", fname, err)
		log.Fatal(DN, msg)
	}

	conf.ServiceFilterMap = make(map[string]bool)
	return conf
}

func DriverAlloc(cfgFile string) (driver NdnfsDriver) {
	conf := ReadParseConfig(cfgFile)
	if conf.Chunksize == 0 {
		conf.Chunksize = defaultChunkSize
	}

	if conf.Mountpoint == "" {
		conf.Mountpoint = defaultMountPoint
	}

	if conf.ServiceFilter != "" {
		services := strings.Split(conf.ServiceFilter, ",")
		for _, srvName := range services {
			conf.ServiceFilterMap[strings.TrimSpace(srvName)] = true
		}
	}

	log.Info(DN, " config: ", conf)
	driver = NdnfsDriver{
		Scope:        "local",
		DefaultVolSz: 1024,
		Mutex:        &sync.Mutex{},
		Nedge:        nedgeprovider.InitNexentaEdgeProvider(conf.Nedgerest, conf.Nedgeport, conf.Username, conf.Password),
		Config:       &conf,
	}
	return driver
}


func (d NdnfsDriver) PrepareConfigMap() map[string]string {
	configMap := make(map[string]string)

	if d.Config.Cluster != "" {
		configMap["cluster"] = d.Config.Cluster
	}

	return configMap
}

func (d NdnfsDriver) Capabilities() *volume.CapabilitiesResponse {
	log.Debug(DN, "Received Capabilities req")
	return &volume.CapabilitiesResponse{Capabilities: volume.Capability{Scope: d.Scope}}
}

// Checks only service name is missing in volume id
func IsNoServiceSpecified(missedParts map[string]bool) bool {
	if len(missedParts) == 1 {
		if _, ok := missedParts["service"]; ok {
			return true
		}
	}
	return false
}

func (d NdnfsDriver) CheckNfsServiceExists(serviceName string) error {
	nedgeService, err := d.Nedge.GetService(serviceName)
	if err != nil {
		return fmt.Errorf("No NexentaEdge service %s has been found", serviceName)
	}

	if nedgeService.ServiceType != "nfs" {
		return fmt.Errorf("Service %s is not nfs type service", nedgeService.Name)
	}

	if len(nedgeService.Network) < 1 {
		return fmt.Errorf("Service %s isn't configured, no client network assigned", nedgeService.Name)
	}

	if nedgeService.Status != "enabled" {
		return fmt.Errorf("Service %s not enabled, enable service to make it available", nedgeService.Name)
	}

	return nil
}

func (d NdnfsDriver) Create(r *volume.CreateRequest) (err error) {
	log.Debugf("Create volume %s using %s with options: %s", r.Name, DN, r.Options)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	configMap := d.PrepareConfigMap()
	volID, missedPathParts, err := nedgeprovider.ParseVolumeID(r.Name, configMap)
	if err != nil {

		// Only service missed in path notation, we should select appropriate service for new volume
		if IsNoServiceSpecified(missedPathParts) {
			log.Infof("No service cpecified!")
			// get all services information to find service by path
			clusterData, err := d.GetClusterData()
			if err != nil {
				return err
			}

			// find service to serve
			appropriateServiceData, err := clusterData.FindApropriateServiceData()
			log.Infof("Appropriate service is : %+v\n", appropriateServiceData)
			if err != nil {
				log.Infof("Appropriate service selection failed : %s\n", err)
				return err
			}

			// assign aprppriate service name to VolumeID
			volID.Service = appropriateServiceData.Service.Name

		} else {
			log.Errorf("ParseVolumeID error : %s\n", err)
			return err
		}
	}

	log.Infof("VolumeID : %+v", volID)
	err = d.CheckNfsServiceExists(volID.Service)
	if err != nil {
		log.Error(err)
		return err
	}

	// check for cluster name existance
	if !d.IsClusterExists(volID.Cluster) {
		return fmt.Errorf("No cluster name %s found", volID.Cluster)
	}

	// check for tenant name existance
	if !d.IsTenantExists(volID.Cluster, volID.Tenant) {
		return fmt.Errorf("No cluster/tenant name %s/%s found", volID.Cluster, volID.Tenant)
	}

	log.Info("Creating bucket")
	if !d.Nedge.IsBucketExist(volID.Cluster, volID.Tenant, volID.Bucket) {
		log.Info("Bucket doesnt exist")

		err := d.Nedge.CreateBucket(volID.Cluster, volID.Tenant, volID.Bucket, 0, r.Options)
		if err != nil {
			log.Error(err)
			return err
		}
	}

	// setup quota configuration
	if quota, ok := r.Options["size"]; ok {
		err = d.Nedge.SetBucketQuota(volID.Cluster, volID.Tenant, volID.Bucket, quota)
		if err != nil {
			log.Error(err)
			return err
		}
	}

	//setup service configuration
	if r.Options["acl"] != "" {
		err := d.Nedge.SetServiceAclConfiguration(volID.Service, volID.Tenant, volID.Bucket, r.Options["acl"])
		if err != nil {
			log.Error(err)
		}
	}

	err = d.Nedge.ServeBucket(volID.Service, volID.Cluster, volID.Tenant, volID.Bucket)
	if err != nil {
		log.Error(err)
	}

	return err
}

func (d NdnfsDriver) Get(r *volume.GetRequest) (*volume.GetResponse, error) {
	log.Debug(DN, "Get volume: ", r.Name)

	var clusterData ClusterData
	configMap := d.PrepareConfigMap()
	volID, missedPathParts, err := nedgeprovider.ParseVolumeID(r.Name, configMap)
	if err != nil {
		// Only service missed in path notation, we should select appropriate service for new volume
		if IsNoServiceSpecified(missedPathParts) {
			log.Infof("No service specified!")
			// get all services information to find service by path
			clusterData, err = d.GetClusterData()
			if err != nil {
				return &volume.GetResponse{}, err
			}
		}
	} else {
		clusterData, err = d.GetClusterData(volID.Service)
		if err != nil {
			return &volume.GetResponse{}, err
		}
	}

	// find service to serve
	serviceData, err := clusterData.FindServiceDataByVolumeID(volID)
	log.Infof("Service is : %+v\n", serviceData)
	if err != nil {
		return &volume.GetResponse{}, err
	}

	// assign service name to VolumeID
	volID.Service = serviceData.Service.Name

	nfsVolume, nfsEndpoint, err := serviceData.GetNFSVolumeAndEndpoint(volID)
	if err != nil {
		return &volume.GetResponse{}, err
	}

	log.Debugf("Device %s nfs endpoint is %s\n", r.Name, nfsEndpoint)
	return &volume.GetResponse{Volume: &volume.Volume{Name: nfsVolume.VolumeID.FullObjectPath(), Mountpoint: nfsEndpoint}}, err
}

func (d NdnfsDriver) List() (*volume.ListResponse, error) {
	log.Debug(DN, "List volume")
	vmap, err := d.ListVolumes()
	var vols []*volume.Volume
	if err != nil {
		log.Panic("Failed to retrieve volume list", err)
	}
	log.Debug(DN, "Nedge response: ", vmap)
	for name, mnt := range vmap {
		if name != "" {
			vols = append(vols, &volume.Volume{Name: name, Mountpoint: mnt})
		}
	}
	return &volume.ListResponse{Volumes: vols}, err
}

/*GetClusterData if serviceName specified we will get data from the one service only */
func (d NdnfsDriver) GetClusterData(serviceName ...string) (ClusterData, error) {

	clusterData := ClusterData{nfsServicesData: []NfsServiceData{}}
	var err error
	services := []nedgeprovider.NedgeService{}
	if len(serviceName) > 0 {
		service, retError := d.Nedge.GetService(serviceName[0])
		if retError != nil {
			log.Error("Failed to retrieve service by name ", serviceName[0])
			return clusterData, err
		}
		services = append(services, service)
	} else {
		services, err = d.Nedge.ListServices()
	}

	if err != nil {
		log.Panic("Failed to retrieve service list", err)
		return clusterData, err
	}

	for _, service := range services {

		//if ServiceFilter not empty, skip every service not presented in list(map)
		if len(d.Config.ServiceFilter) > 0 {
			if _, ok := d.Config.ServiceFilterMap[service.Name]; !ok {
				continue
			}
		}

		if service.ServiceType == "nfs" && service.Status == "enabled" && len(service.Network) > 0 {

			nfsVolumes, err := d.Nedge.ListNFSVolumes(service.Name)
			if err == nil {

				nfsServiceData := NfsServiceData{Service: service, NfsVolumes: []nedgeprovider.NedgeNFSVolume{}}
				for _, volume := range nfsVolumes {
					nfsServiceData.NfsVolumes = append(nfsServiceData.NfsVolumes, volume)
				}
				clusterData.nfsServicesData = append(clusterData.nfsServicesData, nfsServiceData)
			}
		}

	}
	return clusterData, nil
}

func (d NdnfsDriver) ListVolumes() (vmap map[string]string, err error) {
	log.Debug(DN, "ListVolumes ")

	vmap = make(map[string]string)

	clusterData, err := d.GetClusterData()
	if err != nil {
		log.Panic("Failed to retrieve services data", err)
		return vmap, err
	}

	clusterData.FillNfsVolumes(vmap, d.Config.Cluster)

	log.Debug(vmap)
	return vmap, err
}

func (d NdnfsDriver) Mount(r *volume.MountRequest) (*volume.MountResponse, error) {
	log.Info(DN, "Mount volume: ", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	var clusterData ClusterData
	var err error

	configMap := d.PrepareConfigMap()
	volID, missedPathParts, err := nedgeprovider.ParseVolumeID(r.Name, configMap)
	if err != nil {
		// Only service missed in path notation, we should select appropriate service for new volume
		if IsNoServiceSpecified(missedPathParts) {
			log.Infof("No service specified!")
			// get all services information to find service by path
			clusterData, err = d.GetClusterData()
			if err != nil {
				return &volume.MountResponse{}, err
			}
		}
	} else {
		clusterData, err = d.GetClusterData(volID.Service)
		if err != nil {
			return &volume.MountResponse{}, err
		}
	}

	// find service to serve
	serviceData, err := clusterData.FindServiceDataByVolumeID(volID)
	log.Infof("Service is : %+v\n", serviceData)
	if err != nil {
		return &volume.MountResponse{}, err
	}

	// assign service name to VolumeID
	//volID.Service = serviceData.Service.Name

	nfsVolume, nfsEndpoint, err := serviceData.GetNFSVolumeAndEndpoint(volID)
	if err != nil {
		return &volume.MountResponse{}, err
	}

	log.Infof("Nfs volume: %+v NfsEndpoint %s\n", nfsVolume, nfsEndpoint)

	mnt := filepath.Join(d.Config.Mountpoint, nfsVolume.VolumeID.MountPointObjectPath())
	log.Infof("Creating mountpoint folder: %s to remote share %s \n", mnt, nfsEndpoint)
	if out, err := exec.Command("mkdir", "-p", mnt).CombinedOutput(); err != nil {
		log.Info("Error running mkdir command: ", err, "{", string(out), "}")
	}
	log.Debug("Checking if volume is mounted ", nfsVolume.VolumeID.MountPointObjectPath())
	out, err := exec.Command("mount").CombinedOutput()
	if !strings.Contains(string(out), mnt) {
		log.Infof("Mounting Volume %s on %s\n", nfsEndpoint, nfsVolume.VolumeID.MountPointObjectPath())
		args := []string{"-t", "nfs", nfsEndpoint, mnt}
		if out, err := exec.Command("mount", args...).CombinedOutput(); err != nil {
			err = fmt.Errorf("%s: %s", err, out)
			log.Panic("Error running mount command: ", err, "{", string(out), "}")
		}
	}
	return &volume.MountResponse{Mountpoint: mnt}, err
}

func (d NdnfsDriver) Path(r *volume.PathRequest) (*volume.PathResponse, error) {
	log.Infof(DN, "Path volume: %s\n", r.Name)
	var clusterData ClusterData
	var err error

	configMap := d.PrepareConfigMap()
	volID, missedPathParts, err := nedgeprovider.ParseVolumeID(r.Name, configMap)
	if err != nil {
		// Only service missed in path notation, we should select appropriate service for new volume
		if IsNoServiceSpecified(missedPathParts) {
			log.Infof("No service specified!")
			// get all services information to find service by path
			clusterData, err = d.GetClusterData()
			if err != nil {
				return &volume.PathResponse{}, err
			}
		}
	} else {
		clusterData, err = d.GetClusterData(volID.Service)
		if err != nil {
			return &volume.PathResponse{}, err
		}
	}

	// find service to serve
	serviceData, err := clusterData.FindServiceDataByVolumeID(volID)
	log.Infof("Service is : %+v\n", serviceData)
	if err != nil {
		return &volume.PathResponse{}, err
	}

	// find nfs volume in service information
	nfsVolume, err := serviceData.FindNFSVolumeByVolumeID(volID)
	if err != nil {
		return &volume.PathResponse{}, err
	}

	mnt := fmt.Sprintf("%s/%s", d.Config.Mountpoint, nfsVolume.VolumeID.MountPointObjectPath())
	return &volume.PathResponse{Mountpoint: mnt}, err
}

func (d NdnfsDriver) Remove(r *volume.RemoveRequest) error {
	log.Info(DN, "Remove volume: ", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	var clusterData ClusterData
	var err error

	configMap := d.PrepareConfigMap()
	volID, missedPathParts, err := nedgeprovider.ParseVolumeID(r.Name, configMap)
	if err != nil {
		// Only service missed in path notation, we should select appropriate service for new volume
		if IsNoServiceSpecified(missedPathParts) {
			log.Infof("No service cpecified!")
			// get all services information to find service by path
			clusterData, err = d.GetClusterData()
			if err != nil {
				return err
			}
		}
	} else {
		clusterData, err = d.GetClusterData(volID.Service)
		if err != nil {
			return err
		}
	}

	// find service to serve
	serviceData, err := clusterData.FindServiceDataByVolumeID(volID)
	log.Infof("Service is : %+v\n", serviceData)
	if err != nil {
		return err
	}

	// find nfs volume in service information
	nfsVolume, err := serviceData.FindNFSVolumeByVolumeID(volID)
	if err != nil {
		return err
	}

	// before unserve bucket we need to unset ACL property
	d.Nedge.SetServiceAclConfiguration(nfsVolume.VolumeID.Service, nfsVolume.VolumeID.Tenant, nfsVolume.VolumeID.Bucket, "")

	d.Nedge.UnserveBucket(nfsVolume.VolumeID.Service, nfsVolume.VolumeID.Cluster, nfsVolume.VolumeID.Tenant, nfsVolume.VolumeID.Bucket)

	if d.Nedge.IsBucketExist(nfsVolume.VolumeID.Cluster, nfsVolume.VolumeID.Tenant, nfsVolume.VolumeID.Bucket) {
		d.Nedge.DeleteBucket(nfsVolume.VolumeID.Cluster, nfsVolume.VolumeID.Tenant, nfsVolume.VolumeID.Bucket, d.Config.ForceBucketDeletion)
	}

	return err
}

func IsNfsMountExist(mount string) bool {
	cmd := fmt.Sprintf("mount | grep -w %s", strings.TrimRight(mount, "/"))
	if out, err := exec.Command("sh", "-c", cmd).CombinedOutput(); err != nil {

		if msg, ok := err.(*exec.ExitError); ok { // there is error code
			exitStatus := msg.Sys().(syscall.WaitStatus).ExitStatus()
			// log errors only when something goes wrong, not errorCode==1
			if exitStatus != 1 {
				log.Error("Error running mount command: ", err, "{", string(out), "}")
			}
		}
	} else {
		return true
	}
	return false
}

func (d NdnfsDriver) Unmount(r *volume.UnmountRequest) (err error) {
	log.Info(DN, "Unmount volume: ", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	var clusterData ClusterData

	configMap := d.PrepareConfigMap()
	volID, missedPathParts, err := nedgeprovider.ParseVolumeID(r.Name, configMap)
	if err != nil {
		// Only service missed in path notation, we should select appropriate service for new volume
		if IsNoServiceSpecified(missedPathParts) {
			log.Infof("No service cpecified!")
			// get all services information to find service by path
			clusterData, err = d.GetClusterData()
			if err != nil {
				return err
			}
		}
	} else {
		clusterData, err = d.GetClusterData(volID.Service)
		if err != nil {
			return err
		}
	}

	// find service to serve
	serviceData, err := clusterData.FindServiceDataByVolumeID(volID)
	log.Infof("Service is : %+v\n", serviceData)
	if err != nil {
		return err
	}

	// find nfs volume in service information
	nfsVolume, err := serviceData.FindNFSVolumeByVolumeID(volID)
	if err != nil {
		return err
	}

	mnt := filepath.Join(d.Config.Mountpoint, nfsVolume.VolumeID.MountPointObjectPath())
	if IsNfsMountExist(mnt) {
		if out, err := exec.Command("umount", mnt).CombinedOutput(); err != nil {
			log.Error("Error running umount command: ", err, "{", string(out), "}")
		}

		if out, err := exec.Command("rmdir", mnt).CombinedOutput(); err != nil {
			log.Info("Error running rm command: ", err, "{", string(out), "}")
		}
	}
	return err
}

func (d NdnfsDriver) IsClusterExists(clusterName string) bool {
	clusters, err := d.Nedge.ListClusters()
	if err != nil {
		return false
	}

	for _, cluster := range clusters {
		if cluster == clusterName {
			return true
		}
	}
	return false
}

func (d NdnfsDriver) IsTenantExists(clusterName string, tenantName string) bool {
	tenants, err := d.Nedge.ListTenants(clusterName)
	if err != nil {
		return false
	}

	for _, tenant := range tenants {
		if tenant == tenantName {
			return true
		}
	}
	return false
}
