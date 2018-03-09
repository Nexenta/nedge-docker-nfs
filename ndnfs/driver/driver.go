package driver

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"sync"
	"github.com/docker/go-plugins-helpers/volume"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os/exec"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"strconv"
)

const defaultChunkSize int = 32768;
const defaultMountPoint string = "/var/lib/ndnfs"

var (
	DN = "ndnfsdriver "
)

type NdnfsDriver struct {
	Scope		string
	DefaultVolSz	int64
	Mutex		*sync.Mutex
	Endpoint    string
	Config      *Config
}

type Config struct {
	Name        string
	Nedgerest   string
	Nedgeport   int16
	Nedgedata	string
	Clustername string
	Tenantname  string
	Chunksize   int
	Username	string
	Password	string
	Mountpoint  string
	Servicename string
}

func ReadParseConfig(fname string) (Config) {
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
		Scope:			"local",
		DefaultVolSz:	1024,
		Mutex:          &sync.Mutex{},
		Endpoint:       fmt.Sprintf("http://%s:%d/", conf.Nedgerest, conf.Nedgeport),
		Config:         &conf,
	}
	return d
}

func (d *NdnfsDriver) Request(method, endpoint string, data map[string]interface{}) (body []byte, err error) {
	url := d.Endpoint + endpoint
	log.Debug("Issuing request to NexentaEdge, endpoint: ",
		fmt.Sprintf("%s/%s", url, endpoint), " data: ", data, " method: ", method)
	if endpoint == "" {
		err = errors.New("Unable to issue requests without specifying Endpoint")
		log.Fatal(err.Error())
	}
	datajson, err := json.Marshal(data)
	if (err != nil) {
		log.Fatal(err)
	}

	tr := &http.Transport{}
	client := &http.Client{Transport: tr}
	req, err := http.NewRequest(method, url, nil)
	if len(data) != 0 {
		req, err = http.NewRequest(method, url, strings.NewReader(string(datajson)))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic " + basicAuth(d.Config.Username, d.Config.Password))
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("Error while handling request ", err)
	}
	body, err = ioutil.ReadAll(resp.Body)
	log.Debug("Got response, code: ", resp.StatusCode, ", body: ", string(body))
	d.checkError(resp)
	defer resp.Body.Close()
	if (err != nil) {
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

	var cluster, tenant, service string
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
	if r.Options["tenant"] == "" {
		cluster, tenant = d.Config.Clustername, d.Config.Tenantname
	} else {
		cluster, tenant = strings.Split(
			r.Options["tenant"], "/")[0], strings.Split(r.Options["tenant"], "/")[1]
	}
	if r.Options["service"] == "" {
		if os.Getenv("CCOW_SVCNAME") != "" {
			service = os.Getenv("CCOW_SVCNAME")
		} else {
			service = d.Config.Servicename
		}
	} else {
		service = r.Options["service"]
	}
	data["bucketName"] = r.Name
	url := fmt.Sprintf("clusters/%s/tenants/%s/buckets", cluster, tenant)

	body, err := d.Request("POST", url, data)
	resp := make(map[string]interface{})
	jsonerr := json.Unmarshal(body, &resp)
	if (jsonerr != nil) {
		log.Error(jsonerr)
		return err
	}
	if (resp["code"] != nil) && (resp["code"] != "RT_ERR_EXISTS") {
		err = errors.New(fmt.Sprintf("Error while handling request: %s", resp))
		return err
	}

	data = make(map[string]interface{})
	data["chunkSize"] = chunkSizeInt
	data["serve"] = filepath.Join(cluster, tenant, r.Name)
	url = fmt.Sprintf("service/%s/serve", service)
	body, err = d.Request("PUT", url, data)
	resp = make(map[string]interface{})
	jsonerr = json.Unmarshal(body, &resp)
	if (jsonerr != nil) {
		log.Error(jsonerr)
		return err
	}
	if resp["code"] == "EINVAL" {
		err = errors.New(fmt.Sprintf("Error while handling request: %s", resp))
		return err
	}
	return err
}

func (d NdnfsDriver) Get(r *volume.GetRequest) (*volume.GetResponse, error) {
	log.Debug(DN, "Get volume: ", r.Name)
	var mnt string
	nfsMap, err := d.ListVolumes()
	if err != nil {
		log.Info("Volume with name ", r.Name, " not found")
		return &volume.GetResponse{}, err
	}

	for k, v := range nfsMap {
		if k == r.Name {
			mnt = v
			break
		}
	}
	if mnt == "" {
		return &volume.GetResponse{}, err
	}

	log.Debug("Device mountpoint is: ", mnt)
	return &volume.GetResponse{Volume: &volume.Volume{Name: r.Name, Mountpoint: mnt}}, err
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

	nfs := fmt.Sprintf("%s:/%s/%s", d.Config.Nedgedata, d.Config.Tenantname, r.Name)
	mnt = filepath.Join(d.Config.Mountpoint, r.Name)
	log.Debug(DN, "Creating mountpoint folder: ", mnt)
	if out, err := exec.Command("mkdir", "-p", mnt).CombinedOutput(); err != nil {
	    log.Info("Error running mkdir command: ", err, "{", string(out), "}")
	}
	log.Debug(DN, "Mounting Volume ", r.Name)
	args := []string{"-t", "nfs", nfs, mnt}
	if out, err := exec.Command("mount", args...).CombinedOutput(); err != nil {
		err = errors.New(fmt.Sprintf("%s: %s", err, out))
		log.Panic("Error running mount command: ", err, "{", string(out), "}")
	}
	return &volume.MountResponse{Mountpoint: mnt}, err
}

func (d NdnfsDriver) Path(r *volume.PathRequest) (*volume.PathResponse, error) {
	log.Info(DN, "Path volume: ", r.Name)
	var err error
	mnt := fmt.Sprintf("%s%s", d.Config.Mountpoint, r.Name)
	return &volume.PathResponse{Mountpoint: mnt}, err
}

func (d NdnfsDriver) Remove(r *volume.RemoveRequest) error {
	log.Info(DN, "Remove volume: ", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	nfsList, err := d.GetNfsList()
	if err != nil {
		log.Info("Error getting nfs list", err)
		return err
	}
	var path, service string
	for i := range(nfsList) {
		if strings.Contains(nfsList[i], r.Name) {
			path = nfsList[i]
		}
	}
	if path == "" {
		return err
	}
	if os.Getenv("CCOW_SVCNAME") != "" {
		service = os.Getenv("CCOW_SVCNAME")
	} else {
		service = d.Config.Servicename
	}
	data := make(map[string]interface{})
	data["serve"] = path
	url := fmt.Sprintf("service/%s/serve", service)
	_, err = d.Request("DELETE", url, data)
	if err != nil {
		log.Info("Error while handling request", err)
	}

	parts := strings.Split(path, "/")
	url = fmt.Sprintf("clusters/%s/tenants/%s/buckets/%s", parts[0], parts[1], parts[2])
	_, err = d.Request("DELETE", url, nil)

	mnt := filepath.Join(d.Config.Mountpoint, r.Name)
	if out, err := exec.Command("rm", "-rf", mnt).CombinedOutput(); err != nil {
		log.Info("Error running rm command: ", err, "{", string(out), "}")
	}

	return err
}

func (d NdnfsDriver) Unmount(r *volume.UnmountRequest) (err error) {
	log.Info(DN, "Unmount volume: ", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	nfs := fmt.Sprintf("%s/%s", d.Config.Mountpoint, r.Name)
	if out, err := exec.Command("umount", nfs).CombinedOutput(); err != nil {
		log.Error("Error running umount command: ", err, "{", string(out), "}")
	}
	return err
}

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
	if (jsonerr != nil) {
		log.Error(jsonerr)
	}
	if r["response"]["data"]["X-Service-Objects"] == nil {
		return
	}
	strList := r["response"]["data"]["X-Service-Objects"].(string)
	err = json.Unmarshal([]byte(strList), &nfsList)
	if err != nil {
	    log.Fatal(err)
	}
	for i, v := range(nfsList) {
		if len(strings.Split(nfsList[i], ",")) > 1 {
			nfsList[i] = strings.Split(strings.Split(v, ",")[1], "@")[0]
		} else {
			nfsList[i] = v
		}
	}
	return nfsList, err
}

func (d NdnfsDriver) ListVolumes() (vmap map[string]string, err error) {
	log.Debug(DN, "ListVolumes ")
	nfsList, err := d.GetNfsList()
	vmap = make(map[string]string)
	for v := range nfsList {
		vname := strings.Split(nfsList[v], "/")[len(strings.Split(nfsList[v], "/")) - 1]
		vmap[vname] = fmt.Sprintf("%s/%s", d.Config.Mountpoint, vname)
	}
	log.Debug(vmap)
	return vmap, err
}

func isPowerOfTwo(x int) (res bool) {
	return (x != 0) && ((x & (x - 1)) == 0)
}
