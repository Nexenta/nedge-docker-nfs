package driver

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

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
	Endpoint     string
	Config       *Config
}

type Config struct {
	Name        string
	Nedgerest   string
	Nedgeport   int16
	Nedgedata   string
	Clustername string
	Tenantname  string
	Chunksize   int
	Username    string
	Password    string
	Mountpoint  string
	Servicename string
}

type VolumeID struct {
	Cluster string
	Tenant  string
	Bucket  string
	Service string
}

type NedgeService struct {
	Name        string
	ServiceType string
	Status      string
	Network     []string
}

type NedgeNFSVolume struct {
	VolumeID string
	Path     string
	Share    string
}

func ParseVolumeID(path string) (resultObject VolumeID, err error) {
	parts := strings.Split(path, "@")
	if len(parts) != 2 {
		err := errors.New("Wrong format of object path. Path must be in format service:/cluster/tenant/bucket")
		return resultObject, err
	}

	pathObjects := strings.Split(parts[1], "/")
	if len(pathObjects) != 3 {
		err := errors.New("Wrong format of object path. Path must be in format service:/cluster/tenant/bucket")
		return resultObject, err
	}

	resultObject.Service = parts[0]
	resultObject.Cluster = pathObjects[0]
	resultObject.Tenant = pathObjects[1]
	resultObject.Bucket = pathObjects[2]

	return resultObject, err
}

func (path *VolumeID) GetObjectPath() string {
	return fmt.Sprintf("%s@%s/%s/%s", path.Service, path.Cluster, path.Tenant, path.Bucket)
}

func ReadParseConfig(fname string) Config {
	content, err := ioutil.ReadFile(fname)
	if err != nil {
		log.Fatal(DN, "Error reading config file: ", fname, " error: ", err)
	}
	var conf Config
	err = json.Unmarshal(content, &conf)
	if err != nil {
		log.Fatal(DN, "Error parsing config file: ", fname, " error: ", err)
	}
	return conf
}

func DriverAlloc(cfgFile string) NdnfsDriver {
	conf := ReadParseConfig(cfgFile)
	if conf.Chunksize == 0 {
		conf.Chunksize = defaultChunkSize
	}
	if conf.Mountpoint == "" {
		conf.Mountpoint = defaultMountPoint
	}
	log.Info(DN, " config: ", conf)
	d := NdnfsDriver{
		Scope:        "local",
		DefaultVolSz: 1024,
		Mutex:        &sync.Mutex{},
		Endpoint:     fmt.Sprintf("http://%s:%d/", conf.Nedgerest, conf.Nedgeport),
		Config:       &conf,
	}
	return d
}

func (d *NdnfsDriver) setUpAclParams(serviceName string, tenantName string, bucketName string, value string) (err error) {

	aclName := fmt.Sprintf("X-NFS-ACL-%s/%s", tenantName, bucketName)
	return d.setupConfigRequest(serviceName, aclName, value)
}

func (d *NdnfsDriver) removeAclParam(serviceName string, tenantName string, bucketName string) (err error) {
	// to delete property just set it to ""
	return d.setUpAclParams(serviceName, tenantName, bucketName, "")
}
func (d *NdnfsDriver) setupConfigRequest(serviceName string, configParamName string, configParamValue string) (err error) {

	log.Infof("setupConfigRequest: serviceName:%s, configParamName:%s, configParamValue:%s", serviceName, configParamName, configParamValue)
	path := fmt.Sprintf("/service/%s/config", serviceName)

	data := make(map[string]interface{})
	data["param"] = configParamName
	data["value"] = configParamValue

	_, err = d.doNedgeRequest("PUT", path, data)
	return err
}

func (d *NdnfsDriver) SetBucketQuota(cluster string, tenant string, bucket string, quota string, quotaCount string) error {
	path := fmt.Sprintf("clusters/%s/tenants/%s/buckets/%s/quota", cluster, tenant, bucket)

	data := make(map[string]interface{})
	data["quota"] = quota
	if quotaCount != "" {
		data["quota_count"] = quotaCount
	}

	log.Infof("SetBucketQuota: path: %s ", path)
	_, err := d.doNedgeRequest("PUT", path, data)
	return err
}

func (d *NdnfsDriver) doNedgeRequest(method string, path string, data map[string]interface{}) (responseBody []byte, err error) {
	body, err := d.Request(method, path, data)
	if err != nil {
		log.Error(err)
		return body, err
	}
	if len(body) == 0 {
		log.Error("NedgeResponse body is 0")
		return body, fmt.Errorf("Fatal error %s", "NedgeResponse body is 0")
	}

	resp := make(map[string]interface{})
	jsonerr := json.Unmarshal(body, &resp)
	if jsonerr != nil {
		log.Error(jsonerr)
		return body, err
	}
	if resp["code"] == "EINVAL" {
		err = fmt.Errorf("Error while handling request: %s", resp)
	}
	return body, err
}

func (d *NdnfsDriver) Request(method, endpoint string, data map[string]interface{}) (body []byte, err error) {
	url := d.Endpoint + endpoint
	log.Debug("Issuing request to NexentaEdge, endpoint: ",
		url, " data: ", data, " method: ", method)
	if endpoint == "" {
		err = errors.New("Unable to issue requests without specifying Endpoint")
		log.Fatal(err.Error())
	}
	datajson, err := json.Marshal(data)
	if err != nil {
		log.Fatal(err)
	}

	tr := &http.Transport{}
	client := &http.Client{Transport: tr}
	req, err := http.NewRequest(method, url, nil)
	if len(data) != 0 {
		req, err = http.NewRequest(method, url, strings.NewReader(string(datajson)))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+basicAuth(d.Config.Username, d.Config.Password))
	resp, err := client.Do(req)
	log.Debug("Response :", resp, " and error: ", err)
	if err != nil {
		log.Fatal("Error while handling request ", err)
	}
	body, err = ioutil.ReadAll(resp.Body)
	log.Debug("Got response, code: ", resp.StatusCode, ", body: ", string(body))
	d.checkError(resp)
	defer resp.Body.Close()
	if err != nil {
		log.Fatal(err)
	}
	return body, err
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

func (d *NdnfsDriver) checkError(resp *http.Response) (err error) {
	if resp.StatusCode > 399 {
		body, err := ioutil.ReadAll(resp.Body)
		log.Error(resp.StatusCode, body, err)
		return err
	}
	return err
}

func (d NdnfsDriver) Capabilities() *volume.CapabilitiesResponse {
	log.Debug(DN, "Received Capabilities req")
	return &volume.CapabilitiesResponse{Capabilities: volume.Capability{Scope: d.Scope}}
}

func (d NdnfsDriver) Create(r *volume.CreateRequest) (err error) {
	log.Debugf(fmt.Sprintf("Create volume %s using %s with options: %s", r.Name, DN, r.Options))
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	volID, err := ParseVolumeID(r.Name)
	if err != nil {
		return err
	}
	log.Infof("Parsed volume: %+v", volID)

	var chunkSizeInt int
	if r.Options["chunksize"] != "" {
		chunkSizeInt, _ = strconv.Atoi(r.Options["chunksize"])
	} else {
		chunkSizeInt = d.Config.Chunksize
	}

	if chunkSizeInt < 4096 || chunkSizeInt > 1048576 || !(isPowerOfTwo(chunkSizeInt)) {
		err = errors.New("Chunksize must be in range of 4096 - 1048576 and be a power of 2")
		return err
	}

	data := make(map[string]interface{})

	log.Info("Creating bucket")
	if !d.IsBucketExist(volID.Cluster, volID.Tenant, volID.Bucket) {
		log.Info("Bucket doesnt exist")
		data["bucketName"] = volID.Bucket
		data["optionsObject"] = map[string]int{"ccow-chunkmap-chunk-size": chunkSizeInt}
		url := fmt.Sprintf("clusters/%s/tenants/%s/buckets", volID.Cluster, volID.Tenant)

		body, err := d.Request("POST", url, data)
		resp := make(map[string]interface{})
		log.Info("Bucket creation response: %+v", resp)
		jsonerr := json.Unmarshal(body, &resp)
		if len(body) > 0 {
			if jsonerr != nil {
				log.Panic(jsonerr)
				return err
			}
			if (resp["code"] != nil) && (resp["code"] != "RT_ERR_EXISTS") {
				err = errors.New(fmt.Sprintf("Error while handling request: %s", resp))
				log.Panic(err)
			}
		}
	}

	// setup quota configuration
	if quota, ok := r.Options["size"]; ok {
		err = d.SetBucketQuota(volID.Cluster, volID.Tenant, volID.Bucket, quota, r.Options["quota_count"])
		if err != nil {
			log.Error(err)
			return err
		}
	}

	//setup service configuration
	if r.Options["acl"] != "" {
		err := d.setUpAclParams(volID.Service, volID.Tenant, volID.Bucket, r.Options["acl"])
		if err != nil {
			log.Error(err)
		}
	}

	data = make(map[string]interface{})
	data["serve"] = filepath.Join(volID.Cluster, volID.Tenant, volID.Bucket)
	url := fmt.Sprintf("service/%s/serve", volID.Service)
	body, err := d.Request("PUT", url, data)
	resp := make(map[string]interface{})
	jsonerr := json.Unmarshal(body, &resp)
	if len(body) > 0 {
		if jsonerr != nil {
			log.Error(jsonerr)
			return err
		}
		if resp["code"] == "EINVAL" {
			err = errors.New(fmt.Sprintf("Error while handling request: %s", resp))
			return err
		}
	}
	return err
}

func (d NdnfsDriver) Get(r *volume.GetRequest) (*volume.GetResponse, error) {
	log.Debug(DN, "Get volume: ", r.Name)
	volumeInstance, mountpoint, err := d.GetVolumeByID(r.Name)
	if err != nil {
		log.Info("Volume with ID ", r.Name, " not found")
		return &volume.GetResponse{}, err
	}

	log.Debugf("Device %s mountpoint is %s\n", volumeInstance.VolumeID, mountpoint)
	return &volume.GetResponse{Volume: &volume.Volume{Name: volumeInstance.VolumeID, Mountpoint: mountpoint}}, err
}

func (d NdnfsDriver) GetVolumeByID(volumeID string) (nfsVolume NedgeNFSVolume, mountpoint string, err error) {
	log.Debug(DN, "Get volume by ID : ", volumeID)

	volID, err := ParseVolumeID(volumeID)
	if err != nil {
		return nfsVolume, mountpoint, err
	}

	service, err := d.GetServiceInstance(volID.Service)
	if err != nil {
		return nfsVolume, mountpoint, err
	}

	nfsMap, err := d.GetNfsVolumes(volID.Service)
	if err != nil {
		log.Info("Can't get  GetNfsVolumes")
		return nfsVolume, mountpoint, err
	}

	for _, v := range nfsMap {
		if v.VolumeID == volumeID {
			if len(service.Network) > 0 {
				mountpoint = fmt.Sprintf("%s:%s", service.Network[0], v.Share)
				return v, mountpoint, err
			}
		}
	}
	return nfsVolume, mountpoint, errors.New("Can't find volume by ID:" + volumeID + "\n")
}

func (d NdnfsDriver) IsBucketExist(cluster string, tenant string, bucket string) bool {
	log.Debugf("Check bucket existance for %s/%s/%s", cluster, tenant, bucket)
	buckets, err := d.ListBuckets(cluster, tenant)
	if err != nil {
		return false
	}

	for _, value := range buckets {
		if bucket == value {
			log.Debugf("Bucket %s/%s/%s already exist", cluster, tenant, bucket)
			return true
		}
	}
	log.Debugf("No bucket %s/%s/%s found", cluster, tenant, bucket)
	return false
}

func (d NdnfsDriver) ListBuckets(cluster string, tenant string) (buckets []string, err error) {
	url := fmt.Sprintf("clusters/%s/tenants/%s/buckets", cluster, tenant)
	body, err := d.Request("GET", url, nil)

	r := make(map[string]interface{})
	jsonerr := json.Unmarshal(body, &r)
	if jsonerr != nil {
		log.Error(jsonerr)
	}
	if r["response"] == nil {
		log.Debugf("No buckets found for %s/%s", cluster, tenant)
		return buckets, err
	}

	for _, val := range r["response"].([]interface{}) {
		buckets = append(buckets, val.(string))
	}

	log.Debugf("Bucket list for %s/%s : %+v", cluster, tenant, buckets)
	return buckets, err
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

func (d NdnfsDriver) Mount(r *volume.MountRequest) (*volume.MountResponse, error) {
	log.Info(DN, "Mount volume: ", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	var mnt string
	var err error

	volID, err := ParseVolumeID(r.Name)
	if err != nil {
		return nil, err
	}

	// no need mountpoint yet
	nfsVolume, _, err := d.GetVolumeByID(r.Name)
	if err != nil {
		return nil, err
	}

	//nfs := fmt.Sprintf("%s:/%s/%s", d.Config.Nedgedata, volID.Tenant, volID.Bucket)
	nfs := fmt.Sprintf("%s:%s", d.Config.Nedgedata, nfsVolume.Share)
	mnt = filepath.Join(d.Config.Mountpoint, volID.GetObjectPath())
	log.Infof(DN, "Creating mountpoint folder:%s to remote share %s ", mnt, nfs)
	if out, err := exec.Command("mkdir", "-p", mnt).CombinedOutput(); err != nil {
		log.Info("Error running mkdir command: ", err, "{", string(out), "}")
	}
	log.Debug(DN, "Checking if volume is mounted ", r.Name)
	out, err := exec.Command("mount").CombinedOutput()
	if !strings.Contains(string(out), mnt) {
		log.Debug(DN, "Mounting Volume ", r.Name)
		args := []string{"-t", "nfs", nfs, mnt}
		if out, err := exec.Command("mount", args...).CombinedOutput(); err != nil {
			err = errors.New(fmt.Sprintf("%s: %s", err, out))
			log.Panic("Error running mount command: ", err, "{", string(out), "}")
		}
	}
	return &volume.MountResponse{Mountpoint: mnt}, err
}

func (d NdnfsDriver) Path(r *volume.PathRequest) (*volume.PathResponse, error) {
	log.Info(DN, "Path volume: ", r.Name)
	var err error
	mnt := fmt.Sprintf("%s/%s", d.Config.Mountpoint, r.Name)
	return &volume.PathResponse{Mountpoint: mnt}, err
}

func (d NdnfsDriver) Remove(r *volume.RemoveRequest) error {
	log.Info(DN, "Remove volume: ", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	volID, err := ParseVolumeID(r.Name)
	if err != nil {
		return err
	}

	// no need mountpoint yet
	_, _, err = d.GetVolumeByID(r.Name)
	if err != nil {
		return err
	}

	// before unserve bucket we need to unset ACL property
	d.removeAclParam(volID.Service, volID.Tenant, volID.Bucket)

	data := make(map[string]interface{})
	data["serve"] = filepath.Join(volID.Cluster, volID.Tenant, volID.Bucket)
	url := fmt.Sprintf("service/%s/serve", volID.Service)
	_, err = d.Request("DELETE", url, data)
	if err != nil {
		log.Info("Error while handling request", err)
	}

	if d.IsBucketExist(volID.Cluster, volID.Tenant, volID.Bucket) {
		url = fmt.Sprintf("clusters/%s/tenants/%s/buckets/%s", volID.Cluster, volID.Tenant, volID.Bucket)
		_, err = d.Request("DELETE", url, nil)
	}

	return err
}

func (d NdnfsDriver) Unmount(r *volume.UnmountRequest) (err error) {
	log.Info(DN, "Unmount volume: ", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	volID, err := ParseVolumeID(r.Name)
	if err != nil {
		return err
	}

	// no need mountpoint yet
	nfsVolume, _, err := d.GetVolumeByID(r.Name)
	if err != nil {
		return err
	}

	mnt := filepath.Join(d.Config.Mountpoint, volID.GetObjectPath())
	if out, err := exec.Command("rm", "-rf", mnt).CombinedOutput(); err != nil {
		log.Info("Error running rm command: ", err, "{", string(out), "}")
	}

	nfs := fmt.Sprintf("%s:%s", d.Config.Nedgedata, nfsVolume.Share)
	if out, err := exec.Command("umount", nfs).CombinedOutput(); err != nil {
		log.Error("Error running umount command: ", err, "{", string(out), "}")
	}
	return err
}

/*
func (d NdnfsDriver) GetNfsList() (nfsList []string, err error) {
	var body []byte
	if os.Getenv("CCOW_SVCNAME") != "" {
		body, err = d.Request(
			"GET", fmt.Sprintf("service/%s", os.Getenv("CCOW_SVCNAME")), nil)
	} else {
		body, err = d.Request(
			"GET", fmt.Sprintf("service/%s", d.Config.Servicename), nil)
	}

	r := make(map[string]map[string]map[string]interface{})
	jsonerr := json.Unmarshal(body, &r)
	if jsonerr != nil {
		log.Error(jsonerr)
	}
	if r["response"]["data"]["X-Service-Objects"] == nil {
		return
	}
	var exports []string
	strList := r["response"]["data"]["X-Service-Objects"].(string)
	err = json.Unmarshal([]byte(strList), &exports)
	if err != nil {
		log.Fatal(err)
	}
	for i, v := range exports {
		if len(strings.Split(v, ",")) > 1 {
			var service = strings.Split(v, ",")[1]
			var parts = strings.Split(service, "@")
			if strings.HasPrefix(parts[1], fmt.Sprintf("%s/%s", d.Config.Clustername, d.Config.Tenantname)) {
				nfsList = append(nfsList, parts[0])
			}
		} else {
			nfsList[i] = v
		}
	}
	return nfsList, err
}
*/

func (d NdnfsDriver) ListVolumes() (vmap map[string]string, err error) {
	log.Debug(DN, "ListVolumes ")

	vmap = make(map[string]string)
	services, err := d.ListServices()
	if err != nil {
		log.Infof("Failed during ListServices : %+v\n", err)
		return nil, err
	}

	log.Infof("Services: %+v", services)
	for _, service := range services {
		if service.ServiceType == "nfs" && service.Status == "enabled" {
			volumes, err := d.GetNfsVolumes(service.Name)
			if err != nil {
				log.Fatal("ListVolumes failed Error: ", err)
				return nil, err
			}

			log.Infof("NFS Volumes: %+v\n", volumes)

			for _, v := range volumes {
				vname := v.VolumeID
				log.Infof("Network len is %d value: %+v\n", len(service.Network), service.Network)
				log.Infof("vname is %s\n", vname)
				log.Infof("vmap is %s\n", vmap)
				if len(service.Network) > 0 {
					vmap[vname] = fmt.Sprintf("%s:%s", service.Network[0], v.Share)
				}
			}
		}
	}

	log.Debug(vmap)
	return vmap, err
}

func isPowerOfTwo(x int) (res bool) {
	return (x != 0) && ((x & (x - 1)) == 0)
}

func (d NdnfsDriver) GetServiceInstance(name string) (serviceInstance NedgeService, err error) {
	services, err := d.ListServices()

	for _, service := range services {
		if service.Name == name {
			return service, err
		}
	}

	return serviceInstance, errors.New("Service " + name + " not found")
}

func (d NdnfsDriver) ListServices() (services []NedgeService, err error) {
	log.Info("ListServices: ")

	path := "service"
	body, err := d.doNedgeRequest("GET", path, nil)

	//response.data.<service name>.<prop>.value
	r := make(map[string]map[string]interface{})
	jsonerr := json.Unmarshal(body, &r)
	if jsonerr != nil {
		log.Error(jsonerr)
		return services, jsonerr
	}

	data := r["response"]["data"]
	if data == nil {
		err = fmt.Errorf("No response.data object found for ListService request")
		log.Debug(err.Error)
		return services, err
	}

	for srvName, serviceObj := range data.(map[string]interface{}) {

		serviceVal := serviceObj.(map[string]interface{})
		status := serviceVal["X-Status"].(string)
		serviceType := serviceVal["X-Service-Type"].(string)

		service := NedgeService{Name: srvName, ServiceType: serviceType, Status: status, Network: make([]string, 0)}
		// gets all repetitive props
		for key, val := range serviceVal {
			if strings.HasPrefix(key, "X-Container-Network-") {
				//
				if strings.HasPrefix(val.(string), "client-net --ip ") {
					service.Network = append(service.Network, strings.TrimPrefix(val.(string), "client-net --ip "))
					continue
				}
			}
		}

		services = append(services, service)
	}

	log.Debugf("ServiceList : %+v\n", services)
	return services, err
}

func (d NdnfsDriver) GetService(service string) (body []byte, err error) {
	path := fmt.Sprintf("service/%s", service)
	return d.doNedgeRequest("GET", path, nil)
}

func (d NdnfsDriver) GetNfsVolumes(service string) (volumes []NedgeNFSVolume, err error) {

	body, err := d.GetService(service)
	if err != nil {
		log.Errorf("Can't get service by name %s %+v", service, err)
		return volumes, err
	}

	r := make(map[string]map[string]map[string]interface{})
	jsonerr := json.Unmarshal(body, &r)

	if jsonerr != nil {
		log.Error(jsonerr)
		return volumes, err
	}
	if r["response"]["data"]["X-Service-Objects"] == nil {
		log.Errorf("No NFS volumes found for service %s", service)
		return volumes, err
	}

	var objects []string
	strList := r["response"]["data"]["X-Service-Objects"].(string)
	err = json.Unmarshal([]byte(strList), &objects)
	if err != nil {
		log.Error(err)
		return volumes, err
	}

	// Object format: "<id>,<ten/buc>@<clu/ten/buc>""
	for _, v := range objects {
		var objectParts = strings.Split(v, ",")
		if len(objectParts) > 1 {

			parts := strings.Split(objectParts[1], "@")
			if len(parts) > 1 {
				share := "/" + parts[0]
				volume := NedgeNFSVolume{VolumeID: fmt.Sprintf("%s@%s", service, parts[1]), Share: share, Path: parts[1]}
				volumes = append(volumes, volume)
			}
		}
	}
	return volumes, err
}
