package nedgeprovider

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	log "github.com/Sirupsen/logrus"
)

const (
	defaultChunkSize int = 1048576
	defaultSize      int = 1024
)

type NedgeNFSVolume struct {
	VolumeID string
	Path     string
	Share    string
}

/*INexentaEdge interface to provide base methods */
type INexentaEdge interface {
	CreateBucket(cluster string, tenant string, bucket string, size int, options map[string]string) error
	DeleteBucket(cluster string, tenant string, bucket string) error
	ServeService(service string, cluster string, tenant string, bucket string) (err error)
	UnserveService(service string, cluster string, tenant string, bucket string) (err error)
	SetServiceAclConfiguration(service string, tenant string, bucket string, value string) error
	UnsetServiceAclConfiguration(service string, tenant string, bucket string) error
	GetNfsVolumes(service string) (volumes []NedgeNFSVolume, err error)
}

type NexentaEdgeProvider struct {
	endpoint string
	auth     string
}

var nexentaEdgeProviderInstance INexentaEdge = nil

func InitNexentaEdgeProvider(restip string, port int16, username string, password string) INexentaEdge {
	log.Info("GetNexentaEdgeProvider: ")
	if nexentaEdgeProviderInstance == nil {
		log.Info("InitNexentaEdgeProvider initialization")

		nexentaEdgeProviderInstance = &NexentaEdgeProvider{
			endpoint: fmt.Sprintf("http://%s:%d/", restip, port),
			auth:     basicAuth(username, password),
		}
	}

	return nexentaEdgeProviderInstance
}

/*CreateBucket creates new bucket on NexentaEdge clusters
option parameters:
	chunksize: 	chunksize in bytes
	acl: 		string with nedge acl restrictions for bucket
*/
func (nedge *NexentaEdgeProvider) CreateBucket(clusterName string, tenantName string, bucketName string, size int, options map[string]string) (err error) {

	path := fmt.Sprintf("clusters/%s/tenants/%s/buckets", clusterName, tenantName)

	data := make(map[string]interface{})
	data["bucketName"] = bucketName
	data["optionsObject"] = make(map[string]interface{})

	// chunk-size
	chunkSize := defaultChunkSize
	if val, ok := options["chunksize"]; ok {
		chunkSize, err = strconv.Atoi(val)
		if err != nil {
			err = fmt.Errorf("Can't convert chunksize: %v to Integer value", val)
			log.Error(err)
			return err
		}
	}

	if chunkSize < 4096 || chunkSize > 1048576 || !(isPowerOfTwo(chunkSize)) {
		err = errors.New("Chunksize must be in range of 4096 - 1048576 and be a power of 2")
		log.Error(err)
		return err
	}

	data["optionsObject"].(map[string]interface{})["ccow-chunkmap-chunk-size"] = chunkSize

	body, err := nedge.doNedgeRequest("POST", path, data)

	resp := make(map[string]interface{})
	json.Unmarshal(body, &resp)

	if (resp["code"] != nil) && (resp["code"] != "RT_ERR_EXISTS") {
		err = fmt.Errorf("Error while handling request: %s", resp)
	}
	return err
}

func (nedge *NexentaEdgeProvider) DeleteBucket(cluster string, tenant string, bucket string) (err error) {
	path := fmt.Sprintf("clusters/%s/tenants/%s/buckets/%s", cluster, tenant, bucket)

	log.Infof("DeleteBucket: path: %s ", path)
	_, err = nedge.doNedgeRequest("DELETE", path, nil)
	return err
}

func (nedge *NexentaEdgeProvider) SetServiceAclConfiguration(service string, tenant string, bucket string, value string) error {
	aclName := fmt.Sprintf("X-NFS-ACL-%s/%s", tenant, bucket)
	log.Infof("SetServiceAclConfiguration: serviceName:%s, path: %s/%s ", service, tenant, bucket)
	log.Infof("SetServiceAclConfiguration: %s:%s ", aclName, value)
	return nedge.setServiceConfigParam(service, aclName, value)
}

func (nedge *NexentaEdgeProvider) UnsetServiceAclConfiguration(service string, tenant string, bucket string) error {
	aclName := fmt.Sprintf("X-NFS-ACL-%s/%s", tenant, bucket)
	log.Infof("UnsetServiceAclConfiguration: serviceName:%s, path: %s/%s ", service, tenant, bucket)
	log.Infof("UnsetServiceAclConfiguration: %s ", aclName)
	return nedge.setServiceConfigParam(service, aclName, "")
}

func (nedge *NexentaEdgeProvider) setServiceConfigParam(service string, parameter string, value string) (err error) {
	log.Infof("ConfigureService: serviceName:%s, %s:%s", service, parameter, value)
	path := fmt.Sprintf("/service/%s/config", service)

	//request data
	data := make(map[string]interface{})
	data["param"] = parameter
	data["value"] = value

	log.Infof("setServiceConfigParam: path:%s values:%+v", path, data)
	_, err = nedge.doNedgeRequest("PUT", path, data)
	return err
}

func (nedge *NexentaEdgeProvider) ServeService(service string, cluster string, tenant string, bucket string) (err error) {
	path := fmt.Sprintf("service/%s/serve", service)
	serve := fmt.Sprintf("%s/%s/%s", cluster, tenant, bucket)

	//request data
	data := make(map[string]interface{})
	data["serve"] = serve

	log.Infof("ServeService: service: %s data: %+v", path, data)
	_, err = nedge.doNedgeRequest("PUT", path, data)
	return err
}

func (nedge *NexentaEdgeProvider) UnserveService(service string, cluster string, tenant string, bucket string) (err error) {
	path := fmt.Sprintf("service/%s/serve", service)
	serve := fmt.Sprintf("%s/%s/%s", cluster, tenant, bucket)

	//request data
	data := make(map[string]interface{})
	data["serve"] = serve

	log.Infof("UnserveService: service: %s data: %+v", path, data)
	_, err = nedge.doNedgeRequest("DELETE", path, data)
	return err
}

func (nedge *NexentaEdgeProvider) GetService(service string) (body []byte, err error) {
	path := fmt.Sprintf("service/%s", service)
	return nedge.doNedgeRequest("GET", path, nil)
}

func (nedge *NexentaEdgeProvider) GetNfsVolumes(service string) (volumes []NedgeNFSVolume, err error) {

	body, err := nedge.GetService(service)
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
				volume := NedgeNFSVolume{VolumeID: v, Share: parts[0], Path: parts[1]}
				volumes = append(volumes, volume)
			}
		}
	}
	return volumes, err
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

func (nedge *NexentaEdgeProvider) doNedgeRequest(method string, path string, data map[string]interface{}) (responseBody []byte, err error) {
	body, err := nedge.Request(method, path, data)
	if err != nil {
		log.Error(err)
		return body, err
	}
	if len(body) == 0 {
		log.Error("NedgeResponse body is 0")
		return body, fmt.Errorf("Fatal %s", "NedgeResponse body is 0")
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

func (nedge *NexentaEdgeProvider) Request(method, restpath string, data map[string]interface{}) (body []byte, err error) {

	if nedge.endpoint == "" {
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
	url := nedge.endpoint + restpath
	log.Debugf("Request to NexentaEdge [%s] %s data: %+v ", method, url, data)
	req, err := http.NewRequest(method, url, nil)
	if len(data) != 0 {
		req, err = http.NewRequest(method, url, strings.NewReader(string(datajson)))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+nedge.auth)
	resp, err := client.Do(req)
	if err != nil {
		log.Panic("Error while handling request ", err)
		return nil, err
	}
	body, err = ioutil.ReadAll(resp.Body)
	log.Debug("Got response, code: ", resp.StatusCode, ", body: ", string(body))
	nedge.checkError(resp)
	defer resp.Body.Close()
	if err != nil {
		log.Panic(err)
	}
	return body, err
}

func (nedge *NexentaEdgeProvider) checkError(resp *http.Response) (err error) {
	if resp.StatusCode > 399 {
		body, err := ioutil.ReadAll(resp.Body)
		log.Error(resp.StatusCode, body, err)
		return err
	}
	return err
}

func isPowerOfTwo(x int) (res bool) {
	return (x != 0) && ((x & (x - 1)) == 0)
}
