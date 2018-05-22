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

type VolumeID struct {
	Cluster string
	Tenant  string
	Bucket  string
	Service string
}

type NedgeNFSVolume struct {
	VolumeID string
	Path     string
	Share    string
}

type NedgeService struct {
	Name        string
	ServiceType string
	Status      string
	Network     []string
}

func (nedgeService *NedgeService) FindNFSVolumeByVolumeID(volumeID string, nfsVolumes []NedgeNFSVolume) (resultNfsVolume NedgeNFSVolume, err error) {

	for _, nfsVolume := range nfsVolumes {
		if nfsVolume.VolumeID == volumeID {
			return nfsVolume, nil
		}
	}
	return resultNfsVolume, errors.New("Can't find NFS volume by volumeID :" + volumeID)
}

func (nedgeService *NedgeService) GetNFSVolumeAndEndpoint(volumeID string, service NedgeService, nfsVolumes []NedgeNFSVolume) (nfsVolume NedgeNFSVolume, endpoint string, err error) {
	nfsVolume, err = nedgeService.FindNFSVolumeByVolumeID(volumeID, nfsVolumes)
	if err != nil {
		return nfsVolume, "", err
	}

	return nfsVolume, fmt.Sprintf("%s:%s", service.Network[0], nfsVolume.Share), err
}

/*INexentaEdge interface to provide base methods */
type INexentaEdgeProvider interface {
	ListClusters() (clusters []string, err error)
	ListTenants(cluster string) (tenants []string, err error)
	ListBuckets(cluster string, tenant string) (buckets []string, err error)
	IsBucketExist(cluster string, tenant string, bucket string) bool
	CreateBucket(cluster string, tenant string, bucket string, size int, options map[string]string) error
	DeleteBucket(cluster string, tenant string, bucket string) error
	ServeBucket(service string, cluster string, tenant string, bucket string) (err error)
	UnserveBucket(service string, cluster string, tenant string, bucket string) (err error)
	SetBucketQuota(cluster string, tenant string, bucket string, quota string) (err error)
	SetServiceAclConfiguration(service string, tenant string, bucket string, value string) error
	UnsetServiceAclConfiguration(service string, tenant string, bucket string) error
	ListServices() (services []NedgeService, err error)
	GetService(serviceName string) (service NedgeService, err error)
	ListNFSVolumes(serviceName string) (nfsVolumes []NedgeNFSVolume, err error)
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

func ParseVolumeID(volumeID string) (resultObject VolumeID, err error) {
	parts := strings.Split(volumeID, "@")
	if len(parts) != 2 {
		err := errors.New("Wrong format of object path. Path must be in format service@cluster/tenant/bucket")
		return resultObject, err
	}

	pathObjects := strings.Split(parts[1], "/")
	if len(pathObjects) != 3 {
		err := errors.New("Wrong format of object path. Path must be in format service@cluster/tenant/bucket")
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

/*CheckHealth check connection to the nexentaedge cluster */
func (nedge *NexentaEdgeProvider) CheckHealth() (err error) {
	path := "system/status"
	body, err := nedge.doNedgeRequest("GET", path, nil)

	if err != nil {
		err = fmt.Errorf("Failed to send request %s, err: %s\n", path, err)
		log.Error(err)
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
		log.Debug(err)
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

func (nedge *NexentaEdgeProvider) SetBucketQuota(cluster string, tenant string, bucket string, quota string) (err error) {
	path := fmt.Sprintf("clusters/%s/tenants/%s/buckets/%s/quota", cluster, tenant, bucket)

	data := make(map[string]interface{})
	data["quota"] = quota

	log.Infof("SetBucketQuota: path: %s ", path)
	_, err = nedge.doNedgeRequest("PUT", path, data)
	return err
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

func (nedge *NexentaEdgeProvider) GetService(serviceName string) (service NedgeService, err error) {
	log.Infof("GetService : %s\n", serviceName)

	path := fmt.Sprintf("service/%s", serviceName)
	body, err := nedge.doNedgeRequest("GET", path, nil)

	r := make(map[string]map[string]interface{})
	jsonerr := json.Unmarshal(body, &r)

	if jsonerr != nil {
		log.Error(jsonerr)
		return service, jsonerr
	}

	data := r["response"]["data"]
	if data == nil {
		err = fmt.Errorf("No response.data object found for GetService request")
		log.Debug(err.Error)
		return service, err
	}

	serviceVal := data.(map[string]interface{})
	if serviceVal == nil {
		err = fmt.Errorf("No service data object found")
		log.Debug(err.Error)
		return service, err
	}

	service.Name = serviceVal["X-Service-Name"].(string)
	service.Status = serviceVal["X-Status"].(string)
	service.ServiceType = serviceVal["X-Service-Type"].(string)
	service.Network = GetServiceNetwork(serviceVal)

	return service, err
}

func GetServiceNetwork(serviceVal map[string]interface{}) (networks []string) {
	if xvip, ok := serviceVal["X-VIPS"].(string); ok {

		VIP := getVipIPFromString(xvip)
		if VIP != "" {
			//remove subnet
			subnetIndex := strings.Index(VIP, "/")
			if subnetIndex > 0 {
				VIP := VIP[:subnetIndex]
				log.Infof("X-VIP is: %s\n", VIP)
				networks = append(networks, VIP)
			}
		}
	} else {
		// gets all repetitive props
		for key, val := range serviceVal {
			if strings.HasPrefix(key, "X-Container-Network-") {
				if strings.HasPrefix(val.(string), "client-net --ip ") {
					networks = append(networks, strings.TrimPrefix(val.(string), "client-net --ip "))
					continue
				}
			}
		}

	}
	return networks
}

func GetServiceData(serviceVal map[string]interface{}) (service NedgeService, err error) {

	service.Name = serviceVal["X-Service-Name"].(string)
	service.Status = serviceVal["X-Status"].(string)
	service.ServiceType = serviceVal["X-Service-Type"].(string)

	service.Network = make([]string, 0)

	log.Debugf("Service : %+v\n", service)
	return service, err
}

/*ListServices
 */
func (nedge *NexentaEdgeProvider) ListServices() (services []NedgeService, err error) {
	log.Info("ListServices: ")

	path := "service"
	body, err := nedge.doNedgeRequest("GET", path, nil)

	if err != nil {
		log.Error(err)
		return services, err
	}

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

	if servicesJSON, ok := data.(map[string]interface{}); ok {
		for _, serviceObj := range servicesJSON {

			if serviceVal, ok := serviceObj.(map[string]interface{}); ok {

				var service NedgeService
				service.Name = serviceVal["X-Service-Name"].(string)
				service.Status = serviceVal["X-Status"].(string)
				service.ServiceType = serviceVal["X-Service-Type"].(string)
				service.Network = GetServiceNetwork(serviceVal)

				if err == nil {
					services = append(services, service)
				}
			}
		}
	}

	log.Debugf("ServiceList : %+v\n", services)
	return services, err
}

func (nedge *NexentaEdgeProvider) ListNFSVolumes(serviceName string) (nfsVolumes []NedgeNFSVolume, err error) {
	path := fmt.Sprintf("service/%s", serviceName)
	body, err := nedge.doNedgeRequest("GET", path, nil)

	r := make(map[string]map[string]interface{})
	jsonerr := json.Unmarshal(body, &r)

	if jsonerr != nil {
		log.Error(jsonerr)
		return nfsVolumes, jsonerr
	}

	data := r["response"]["data"]
	if data == nil {
		err = fmt.Errorf("No response.data object found for GetService request")
		log.Debug(err.Error)
		return nfsVolumes, err
	}

	serviceVal := data.(map[string]interface{})
	if serviceVal == nil {
		err = fmt.Errorf("No service data object found")
		log.Debug(err.Error)
		return nfsVolumes, err
	}

	if serviceVal["X-Service-Type"].(string) != "nfs" {
		return nfsVolumes, err
	}

	// Object format: "<id>,<ten/buc>@<clu/ten/buc>""
	if objects, ok := serviceVal["X-Service-Objects"].(string); ok {
		volumes, err := getXServiceObjectsFromString(serviceName, objects)
		if err == nil {
			nfsVolumes = volumes
		}
	}

	return nfsVolumes, err
}

func (nedge *NexentaEdgeProvider) ServeBucket(service string, cluster string, tenant string, bucket string) (err error) {
	path := fmt.Sprintf("service/%s/serve", service)
	serve := fmt.Sprintf("%s/%s/%s", cluster, tenant, bucket)

	//request data
	data := make(map[string]interface{})
	data["serve"] = serve

	log.Infof("ServeService: service: %s data: %+v", path, data)
	_, err = nedge.doNedgeRequest("PUT", path, data)
	return err
}

func (nedge *NexentaEdgeProvider) UnserveBucket(service string, cluster string, tenant string, bucket string) (err error) {
	path := fmt.Sprintf("service/%s/serve", service)
	serve := fmt.Sprintf("%s/%s/%s", cluster, tenant, bucket)

	//request data
	data := make(map[string]interface{})
	data["serve"] = serve

	log.Infof("UnserveService: service: %s data: %+v", path, data)
	_, err = nedge.doNedgeRequest("DELETE", path, data)
	return err
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
	//log.Debug("Got response, code: ", resp.StatusCode, ", body: ", string(body))
	nedge.checkError(resp)
	defer resp.Body.Close()
	if err != nil {
		log.Panic(err)
	}
	return body, err
}

/*
	Utility methods
*/

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

func getXServiceObjectsFromString(service string, xObjects string) (nfsVolumes []NedgeNFSVolume, err error) {

	var objects []string
	err = json.Unmarshal([]byte(xObjects), &objects)
	if err != nil {
		log.Error(err)
		return nfsVolumes, err
	}

	// Object format: "<id>,<ten/buc>@<clu/ten/buc>""
	for _, v := range objects {
		var objectParts = strings.Split(v, ",")
		if len(objectParts) == 2 {

			parts := strings.Split(objectParts[1], "@")
			if len(parts) == 2 {
				share := "/" + parts[0]
				volume := NedgeNFSVolume{VolumeID: fmt.Sprintf("%s@%s", service, parts[1]), Share: share, Path: parts[1]}
				nfsVolumes = append(nfsVolumes, volume)
			}
		}
	}
	return nfsVolumes, err
}

func getVipIPFromString(xvips string) string {
	log.Infof("X-Vips is: %s\n", xvips)
	xvipBody := []byte(xvips)
	r := make([]interface{}, 0)
	jsonerr := json.Unmarshal(xvipBody, &r)
	if jsonerr != nil {
		log.Error(jsonerr)
		return ""
	}
	log.Infof("Processed is: %s\n", r)

	if r == nil {
		return ""
	}

	for _, outerArrayItem := range r {
		innerArray := outerArrayItem.([]interface{})
		log.Infof("InnerArray is: %s\n", innerArray)

		if innerArray, ok := outerArrayItem.([]interface{}); ok {
			for _, innerArrayItem := range innerArray {
				if item, ok := innerArrayItem.(map[string]interface{}); ok {
					if ipValue, ok := item["ip"]; ok {
						log.Infof("VIP IP Found : %s\n", ipValue)
						return ipValue.(string)
					}
				}
			}
		}
	}

	return ""
}
