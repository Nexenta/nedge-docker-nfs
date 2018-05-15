package ndnfsapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	log "github.com/Sirupsen/logrus"
)

const defaultSize string = "1024"
const defaultFSType string = "xfs"
const defaultChunkSize int = 1048576
const defaultMountPoint string = "/var/lib/ndnfs"

var (
	DN = "ndnfsapi "
)

type Client struct {
	endpoint  string
	chunksize int
	Config    *Config
}

type Config struct {
	Name        string // ndnfs
	Nedgerest   string // localhost
	Nedgedata   string // localhost
	Nedgeport   int16  // 8080
	Clustername string
	Tenantname  string
	Chunksize   int
	Username    string
	Password    string
	Mountpoint  string
	Servicename string
}

func ReadParseConfig(fname string) (Config, error) {
	content, err := ioutil.ReadFile(fname)
	if err != nil {
		log.Fatal(DN, "Error reading config file: ", fname, " error: ", err)
	}
	var conf Config
	err = json.Unmarshal(content, &conf)
	if err != nil {
		log.Fatal(DN, "Error parsing config file: ", fname, " error: ", err)
	}
	return conf, nil
}

func ClientAlloc(configFile string) (c *Client, err error) {
	conf, err := ReadParseConfig(configFile)
	if err != nil {
		log.Fatal(DN, "Error initializing client from Config file: ", configFile, " error: ", err)
	}
	if conf.Chunksize == 0 {
		conf.Chunksize = defaultChunkSize
	}
	if conf.Mountpoint == "" {
		conf.Mountpoint = defaultMountPoint
	}
	NdnfsClient := &Client{
		endpoint:  fmt.Sprintf("http://%s:%d/", conf.Nedgerest, conf.Nedgeport),
		chunksize: conf.Chunksize,
		Config:    &conf,
	}
	return NdnfsClient, nil
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

func (c *Client) Request(method, endpoint string, data map[string]interface{}) (body []byte, err error) {
	log.Debug("Issue request to Nexenta, endpoint: ", endpoint, " data: ", data, " method: ", method)
	if c.endpoint == "" {
		log.Panic("Endpoint is not set, unable to issue requests")
		err = errors.New("Unable to issue json-rpc requests without specifying Endpoint")
		return nil, err
	}
	datajson, err := json.Marshal(data)
	if err != nil {
		log.Panic(err)
	}

	tr := &http.Transport{}
	client := &http.Client{Transport: tr}
	url := c.endpoint + endpoint
	req, err := http.NewRequest(method, url, nil)
	if len(data) != 0 {
		req, err = http.NewRequest(method, url, strings.NewReader(string(datajson)))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+basicAuth(c.Config.Username, c.Config.Password))
	resp, err := client.Do(req)
	if err != nil {
		log.Panic("Error while handling request ", err)
		return nil, err
	}
	body, err = ioutil.ReadAll(resp.Body)
	log.Debug("Got response, code: ", resp.StatusCode, ", body: ", string(body))
	c.checkError(resp)
	defer resp.Body.Close()
	if err != nil {
		log.Panic(err)
	}
	return body, err
}

func (c *Client) checkError(resp *http.Response) (err error) {
	if resp.StatusCode > 399 {
		body, err := ioutil.ReadAll(resp.Body)
		log.Error(resp.StatusCode, body, err)
		return err
	}
	return err
}

func (c *Client) CreateVolume(name string, options map[string]string) (err error) {
	log.Info(DN, ": Creating volume ", name)
	var cluster, tenant, service string
	var chunkSizeInt int
	if options["chunksize"] != "" {
		chunkSizeInt, _ = strconv.Atoi(options["chunksize"])
	} else {
		chunkSizeInt = c.chunksize
	}

	if chunkSizeInt < 4096 || chunkSizeInt > 1048576 || !(isPowerOfTwo(chunkSizeInt)) {
		err = errors.New("Chunksize must be in range of 4096 - 1048576 and be a power of 2")
		log.Error(err)
		return err
	}
	data := make(map[string]interface{})
	if options["tenant"] == "" {
		cluster, tenant = c.Config.Clustername, c.Config.Tenantname
	} else {
		cluster, tenant = strings.Split(
			options["tenant"], "/")[0], strings.Split(options["tenant"], "/")[1]
	}
	if options["service"] == "" {
		if os.Getenv("CCOW_SVCNAME") != "" {
			service = os.Getenv("CCOW_SVCNAME")
		} else {
			service = c.Config.Servicename
		}
	} else {
		service = options["service"]
	}

	if !c.IsBucketExist(cluster, tenant, name) {

		data["bucketName"] = name
		data["optionsObject"] = map[string]int{"ccow-chunkmap-chunk-size": chunkSizeInt}
		url := fmt.Sprintf("clusters/%s/tenants/%s/buckets", cluster, tenant)

		body, err := c.Request("POST", url, data)
		resp := make(map[string]interface{})
		jsonerr := json.Unmarshal(body, &resp)
		if len(body) > 0 {
			if jsonerr != nil {
				log.Error(jsonerr)
			}

			if (resp["code"] != nil) && (resp["code"] != "RT_ERR_EXISTS") {
				err = errors.New(fmt.Sprintf("Error while handling request: %s", resp))
				return err
			}
		}
	}

	//setup bucket quota
	if quota, ok := options["size"]; ok {
		err = c.SetBucketQuota(cluster, tenant, name, quota)
		if err != nil {
			log.Error(err)
			return err
		}
	}

	//setup service configuration
	if options["acl"] != "" {
		err := c.setUpAclParams(service, tenant, name, options["acl"])
		if err != nil {
			log.Error(err)
		}
	}

	data = make(map[string]interface{})
	data["serve"] = filepath.Join(cluster, tenant, name)
	url := fmt.Sprintf("service/%s/serve", service)
	body, err := c.Request("PUT", url, data)
	resp := make(map[string]interface{})
	jsonerr := json.Unmarshal(body, &resp)
	if jsonerr != nil {
		log.Error(jsonerr)
	}
	if resp["code"] == "EINVAL" {
		return fmt.Errorf("Error while handling request: %s", resp)
	}

	return err
}

func (c *Client) DeleteVolume(name string) (err error) {
	log.Debug(DN, "Deleting Volume ", name)
	var service string
	if os.Getenv("CCOW_SVCNAME") != "" {
		service = os.Getenv("CCOW_SVCNAME")
	} else {
		service = c.Config.Servicename
	}

	// before unserve bucket we need to unset ACL property
	c.removeAclParam(service, c.Config.Tenantname, name)

	data := make(map[string]interface{})
	data["serve"] = filepath.Join(c.Config.Clustername, c.Config.Tenantname, name)
	url := fmt.Sprintf("service/%s/serve", service)
	_, err = c.Request("DELETE", url, data)
	if err != nil {
		log.Panic("Error while handling request", err)
	}

	if c.IsBucketExist(c.Config.Clustername, c.Config.Tenantname, name) {
		url = fmt.Sprintf("clusters/%s/tenants/%s/buckets/%s", c.Config.Clustername, c.Config.Tenantname, name)
		_, err = c.Request("DELETE", url, nil)
	}

	return err
}

func (c *Client) setUpAclParams(serviceName string, tenantName string, bucketName string, value string) (err error) {

	aclName := fmt.Sprintf("X-NFS-ACL-%s/%s", tenantName, bucketName)
	return c.setupConfigRequest(serviceName, aclName, value)
}

func (c *Client) removeAclParam(serviceName string, tenantName string, bucketName string) (err error) {

	aclName := fmt.Sprintf("X-NFS-ACL-%s/%s", tenantName, bucketName)
	return c.setupConfigRequest(serviceName, aclName, "")
}

func (c *Client) SetBucketQuota(cluster string, tenant string, bucket string, quota string) error {
	path := fmt.Sprintf("clusters/%s/tenants/%s/buckets/%s/quota", cluster, tenant, bucket)

	data := make(map[string]interface{})
	data["quota"] = quota

	log.Infof("SetBucketQuota: path: %s ", path)
	_, err := c.Request("PUT", path, data)
	return err
}

func (c *Client) setupConfigRequest(serviceName string, configParamName string, configParamValue string) (err error) {

	log.Infof("setupConfigRequest: serviceName:%s, configParamName:%s, configParamValue:%s", serviceName, configParamName, configParamValue)
	configUrl := fmt.Sprintf("/service/%s/config", serviceName)

	data := make(map[string]interface{})
	data["param"] = configParamName
	data["value"] = configParamValue

	body, err := c.Request("PUT", configUrl, data)
	resp := make(map[string]interface{})
	jsonerr := json.Unmarshal(body, &resp)
	if jsonerr != nil {
		log.Error(jsonerr)
	}
	if resp["code"] == "EINVAL" {
		err = fmt.Errorf("Error while handling request: %s", resp)
	}
	return err
}

func (c *Client) MountVolume(name string) (mnt string, err error) {
	log.Debug(DN, "Mounting Volume ", name)

	mnt = filepath.Join(c.Config.Mountpoint, name)
	if out, err := exec.Command("mkdir", "-p", mnt).CombinedOutput(); err != nil {
		log.Info("Error running mkdir command: ", err, "{", string(out), "}")
		return "", err
	}

	nfs := fmt.Sprintf("%s:/%s/%s", c.Config.Nedgedata, c.Config.Tenantname, name)
	mnt = filepath.Join(c.Config.Mountpoint, name)
	args := []string{"-t", "nfs", nfs, mnt}
	log.Debug(DN, "Checking if volume is mounted ", name)
	out, err := exec.Command("mount").CombinedOutput()
	if !strings.Contains(string(out), mnt) {
		log.Debug(DN, "Running mount cmd: mount ", args)
		if out, err := exec.Command("mount", args...).CombinedOutput(); err != nil {
			log.Error("Error running mount command: ", err, "{", string(out), "}")
			err = errors.New(fmt.Sprintf("%s: %s", err, out))
			return mnt, err
		}
	}
	return mnt, err
}

func (c *Client) UnmountVolume(name string) (err error) {
	log.Debug(DN, "Unmounting Volume ", name)
	nfs := fmt.Sprintf("%s:/%s/%s", c.Config.Nedgedata, c.Config.Tenantname, name)
	if out, err := exec.Command("umount", nfs).CombinedOutput(); err != nil {
		log.Error("Error running umount command: ", err, "{", string(out), "}")
	}

	url := fmt.Sprintf("clusters/%s/tenants/%s/buckets/%s", c.Config.Clustername, c.Config.Tenantname, name)
	_, err = c.Request("DELETE", url, nil)
	mnt := filepath.Join(c.Config.Mountpoint, name)
	log.Info(DN, " Mountpoint to delete : ", mnt)
	if out, err := exec.Command("rm", "-rf", mnt).CombinedOutput(); err != nil {
		log.Info("Error running rm command: ", err, "{", string(out), "}")
	}

	return err
}

func (c *Client) GetVolume(name string) (vname, mnt string, err error) {
	log.Debug(DN, "GetVolume ", name)
	nfsMap, err := c.ListVolumes()
	for k, v := range nfsMap {
		if k == name {
			return k, v, err
		}
	}
	return vname, mnt, err
}

func (c *Client) IsBucketExist(cluster string, tenant string, bucket string) bool {
	log.Debugf("Check bucket existance for %s/%s/%s", cluster, tenant, bucket)
	buckets, err := c.ListBuckets(cluster, tenant)
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

func (c *Client) ListBuckets(cluster string, tenant string) (buckets []string, err error) {
	url := fmt.Sprintf("clusters/%s/tenants/%s/buckets", cluster, tenant)
	body, err := c.Request("GET", url, nil)

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

func (c *Client) ListVolumes() (vmap map[string]string, err error) {
	log.Debug(DN, "ListVolumes ")
	nfsList, err := c.GetNfsList()
	vmap = make(map[string]string)
	for v := range nfsList {
		vname := strings.Split(nfsList[v], "/")[len(strings.Split(nfsList[v], "/"))-1]
		vmap[vname] = fmt.Sprintf("%s/%s", c.Config.Mountpoint, vname)
	}
	log.Debug(vmap)
	return vmap, err
}

func (c *Client) GetNfsList() (nfsList []string, err error) {
	var body []byte
	if os.Getenv("CCOW_SVCNAME") != "" {
		body, err = c.Request(
			"GET", fmt.Sprintf("service/%s", os.Getenv("CCOW_SVCNAME")), nil)
	} else {
		body, err = c.Request(
			"GET", fmt.Sprintf("service/%s", c.Config.Servicename), nil)
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
			if strings.HasPrefix(parts[1], fmt.Sprintf("%s/%s", c.Config.Clustername, c.Config.Tenantname)) {
				nfsList = append(nfsList, parts[0])
			}
		} else {
			nfsList[i] = v
		}
	}
	return nfsList, err
}

func isPowerOfTwo(x int) (res bool) {
	return (x != 0) && ((x & (x - 1)) == 0)
}
