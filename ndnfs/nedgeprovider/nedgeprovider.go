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

	log "github.com/sirupsen/logrus"
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

/*
type NedgeServiceType string

const (
	Nfs   = NedgeServiceType("nfs")
	Iscsi = NedgeServiceType("iscsi")
)
*/

type NedgeService struct {
	Name        string
	ServiceType string
	Status      string
	Network     []string
}

/*INexentaEdge interface to provide base methods */
type INexentaEdgeProvider interface {
	ListClusters() (clusters []string, err error)
	ListTenants(cluster string) (tenants []string, err error)
	ListBuckets(cluster string, tenant string) (buckets []string, err error)
	IsBucketExist(cluster string, tenant string, bucket string) bool
	CreateBucket(cluster string, tenant string, bucket string, size int, options map[string]string) error
	SetBucketQuota(cluster string, tenant string, bucket string, quota string, quotaCount string) error
	DeleteBucket(cluster string, tenant string, bucket string) error
	ServeService(service string, cluster string, tenant string, bucket string) (err error)
	UnserveService(service string, cluster string, tenant string, bucket string) (err error)
	SetServiceAclConfiguration(service string, tenant string, bucket string, value string) error
	UnsetServiceAclConfiguration(service string, tenant string, bucket string) error
	GetNfsVolumes(service string) (volumes []NedgeNFSVolume, err error)
	ListServices() (services []NedgeService, err error)
	CheckHealth() (err error)
}

type NexentaEdgeProvider struct {
	endpoint string
	auth     string
}

var nexentaEdgeProviderInstance INexentaEdgeProvider

func InitNexentaEdgeProvider(restip string, port int16, username string, password string) INexentaEdgeProvider {
	log.SetLevel(log.DebugLevel)
	log.Info("InitNexentaEdgeProvideri\n")
	loggerLevel := log.GetLevel()
	log.Infof("LOGGER LEVEL IS: %s\n", loggerLevel.String())

	if nexentaEdgeProviderInstance == nil {
		log.Info("InitNexentaEdgeProvider initialization")

		nexentaEdgeProviderInstance = &NexentaEdgeProvider{
			endpoint: fmt.Sprintf("http://%s:%d/", restip, port),
			auth:     basicAuth(username, password),
		}
	}

	return nexentaEdgeProviderInstance
}

/*CheckHealth check connection to the nexentaedge cluster */
func (nedge *NexentaEdgeProvider) CheckHealth() (err error) {
	path := "system/status"
	body, err := nedge.doNedgeRequest("GET", path, nil)

	if err != nil {
		err = fmt.Errorf("Failed to send request %s, err: %s\n", path, err.Error)
		log.Error(err.Error)
		return err
	}

	r := make(map[string]map[string]interface{})
	jsonerr := json.Unmarshal(body, &r)
	if jsonerr != nil {
		log.Error(jsonerr)
		return jsonerr
	}
	if r["response"] == nil {

		err = fmt.Errorf("No response for CheckHealth call: %s", path)
		log.Debug(err.Error)
		return err
	}

	result := r["response"]["restWorker"]
	if result != "ok" {
		err = fmt.Errorf("Wrong response of the CheckHealth call: restWorker is %s", result)
		log.Debug(err.Error)
		return
	}

	return nil
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

func (nedge *NexentaEdgeProvider) SetBucketQuota(cluster string, tenant string, bucket string, quota string, quotaCount string) error {
	path := fmt.Sprintf("clusters/%s/tenants/%s/buckets/%s/quota", cluster, tenant, bucket)

	data := make(map[string]interface{})
	data["quota"] = quota
	if quotaCount != "" {
		data["quota_count"] = quotaCount
	}

	log.Infof("SetBucketQuota: path: %s ", path)
	_, err := nedge.doNedgeRequest("PUT", path, data)
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

func (nedge *NexentaEdgeProvider) ListServices() (services []NedgeService, err error) {
	log.Info("ListServices: ")

	path := "service"
	body, err := nedge.doNedgeRequest("GET", path, nil)

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

func (nedge *NexentaEdgeProvider) IsBucketExist(cluster string, tenant string, bucket string) bool {
	log.Debugf("Check bucket existance for %s/%s/%s", cluster, tenant, bucket)
	buckets, err := nedge.ListBuckets(cluster, tenant)
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

func (nedge *NexentaEdgeProvider) ListBuckets(cluster string, tenant string) (buckets []string, err error) {
	url := fmt.Sprintf("clusters/%s/tenants/%s/buckets", cluster, tenant)
	body, err := nedge.doNedgeRequest("GET", url, nil)

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

func (nedge *NexentaEdgeProvider) ListClusters() (clusters []string, err error) {
	url := "clusters"
	body, err := nedge.doNedgeRequest("GET", url, nil)

	r := make(map[string]interface{})
	jsonerr := json.Unmarshal(body, &r)
	if jsonerr != nil {
		log.Error(jsonerr)
	}

	if r["response"] == nil {
		log.Debugf("No clusters found for NexentaEdge cluster %s", nedge.endpoint)
		return clusters, err
	}

	for _, val := range r["response"].([]interface{}) {
		clusters = append(clusters, val.(string))
	}

	log.Debugf("Cluster list for NexentaEdge cluster %s", nedge.endpoint)
	return clusters, err
}

func (nedge *NexentaEdgeProvider) ListTenants(cluster string) (tenants []string, err error) {
	url := fmt.Sprintf("clusters/%s/tenants", cluster)
	body, err := nedge.doNedgeRequest("GET", url, nil)

	r := make(map[string]interface{})
	jsonerr := json.Unmarshal(body, &r)
	if jsonerr != nil {
		log.Error(jsonerr)
	}

	if r["response"] == nil {
		log.Debugf("No tenants for %s cluster found ", cluster)
		return tenants, err
	}

	for _, val := range r["response"].([]interface{}) {
		tenants = append(tenants, val.(string))
	}

	log.Debugf("Tenant list for cluster %s", cluster)
	return tenants, err
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
				volume := NedgeNFSVolume{VolumeID: parts[1], Share: "/" + parts[0], Path: parts[1]}
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
