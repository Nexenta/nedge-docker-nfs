package ndnfsapi

import (
    "fmt"
    "encoding/json"
    log "github.com/Sirupsen/logrus"
    "io/ioutil"
    "errors"
    "net/http"
    "os/exec"
    "os"
    "path/filepath"
    "strings"
    "strconv"
)

const defaultSize string = "1024";
const defaultFSType string = "xfs";
const defaultChunkSize int = 32768;
const defaultMountPoint string = "/var/lib/ndvol"

var (
    DN = "ndnfsapi "
)

type Client struct {
    endpoint    string
    chunksize   int
    Config      *Config
}

type Config struct {
    Name        string // ndnfs
    Nedgerest   string // localhost
    Nedgedata   string // localhost
    Nedgeport   int16 // 8080
    Clustername string
    Tenantname  string
    Chunksize   int
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
        endpoint:       fmt.Sprintf("http://%s:%d/", conf.Nedgerest, conf.Nedgeport),
        chunksize:      conf.Chunksize,
        Config:         &conf,
    }
    return NdnfsClient, nil
}

func (c *Client) Request(method, endpoint string, data map[string]interface{}) (body []byte, err error) {
    log.Debug("Issue request to Nexenta, endpoint: ", endpoint, " data: ", data, " method: ", method)
    if c.endpoint == "" {
        log.Panic("Endpoint is not set, unable to issue requests")
        err = errors.New("Unable to issue json-rpc requests without specifying Endpoint")
        return nil, err
    }
    datajson, err := json.Marshal(data)
    if (err != nil) {
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
    resp, err := client.Do(req)
    if err != nil {
        log.Panic("Error while handling request ", err)
        return nil, err
    }
    body, err = ioutil.ReadAll(resp.Body)
    log.Debug("Got response, code: ", resp.StatusCode, ", body: ", string(body))
    c.checkError(resp)
    defer resp.Body.Close()
    if (err != nil) {
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

    if chunkSizeInt < 4096 {
        err = errors.New("chunksize must have a minimum value of 4096")
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
    data["bucketName"] = name
    url := fmt.Sprintf("clusters/%s/tenants/%s/buckets", cluster, tenant)

    body, err := c.Request("POST", url, data)
    resp := make(map[string]interface{})
    jsonerr := json.Unmarshal(body, &resp)
    if (jsonerr != nil) {
        log.Error(jsonerr)
    }
    if (resp["code"] != nil) && (resp["code"] != "RT_ERR_EXISTS") {
        err = errors.New(fmt.Sprintf("Error while handling request: %s", resp))
        return err
    }

    data = make(map[string]interface{})
    data["chunkSize"] = chunkSizeInt
    data["serve"] = filepath.Join(cluster, tenant, name)
    url = fmt.Sprintf("service/%s/serve", service)
    body, err = c.Request("PUT", url, data)
    resp = make(map[string]interface{})
    jsonerr = json.Unmarshal(body, &resp)
    if (jsonerr != nil) {
        log.Error(jsonerr)
    }
    if resp["code"] == "EINVAL" {
        err = errors.New(fmt.Sprintf("Error while handling request: %s", resp))
        return err
    }

    mnt := filepath.Join(c.Config.Mountpoint, name)
    if out, err := exec.Command("mkdir", "-p", mnt).CombinedOutput(); err != nil {
        log.Info("Error running mkdir command: ", err, "{", string(out), "}")
    }
    return err
}

func (c *Client) DeleteVolume(name string) (err error) {
    log.Debug(DN, "Deleting Volume ", name)
    nfsList, err := c.GetNfsList()
    if err != nil {
        log.Panic("Error getting nfs list", err)
    }
    path := ""
    for i := range(nfsList) {
        if strings.Contains(nfsList[i], name) {
            path = nfsList[i]
        }
    }
    var service string
    if os.Getenv("CCOW_SVCNAME") != "" {
        service = os.Getenv("CCOW_SVCNAME")
    } else {
        service = c.Config.Servicename
    }
    data := make(map[string]interface{})
    data["serve"] = path
    url := fmt.Sprintf("service/%s/serve", service)
    _, err = c.Request("DELETE", url, data)
    if err != nil {
        log.Panic("Error while handling request", err)
    }

    parts := strings.Split(path, "/")
    url = fmt.Sprintf("clusters/%s/tenants/%s/buckets/%s", parts[0], parts[1], parts[2])
    _, err = c.Request("DELETE", url, nil)

    mnt := filepath.Join(c.Config.Mountpoint, name)
    if out, err := exec.Command("rm", "-rf", mnt).CombinedOutput(); err != nil {
        log.Info("Error running rm command: ", err, "{", string(out), "}")
    }

    return err
}

func (c *Client) MountVolume(name string) (mnt string, err error) {
    log.Debug(DN, "Mounting Volume ", name)

    nfs := fmt.Sprintf("%s:/%s/%s", c.Config.Nedgedata, c.Config.Tenantname, name)
    mnt = filepath.Join(c.Config.Mountpoint, name)
    args := []string{"-t", "nfs", nfs, mnt}
    if out, err := exec.Command("mount", args...).CombinedOutput(); err != nil {
        log.Error("Error running mount command: ", err, "{", string(out), "}")
        err = errors.New(fmt.Sprintf("%s: %s", err, out))
        return mnt, err
    }
    return mnt, err
}

func (c *Client) UnmountVolume(name string) (err error) {
    log.Debug(DN, "Unmounting Volume ", name)
    nfs := fmt.Sprintf("%s:/%s", c.Config.Nedgedata, name)
    if out, err := exec.Command("umount", nfs).CombinedOutput(); err != nil {
        log.Error("Error running umount command: ", err, "{", string(out), "}")
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

func (c *Client) ListVolumes() (vmap map[string]string, err error) {
    log.Debug(DN, "ListVolumes ")
    nfsList, err := c.GetNfsList()
    vmap = make(map[string]string)
    for v := range nfsList {
        vname := strings.Split(nfsList[v], "/")[len(strings.Split(nfsList[v], "/")) - 1]
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
    if (jsonerr != nil) {
        log.Error(jsonerr)
    }
    if r["response"]["data"]["X-Service-Objects"] == nil {
        return
    }
    strList := strings.Trim((r["response"]["data"]["X-Service-Objects"].(string)), "[]")
    nfsList = strings.Split(strList, ",")
    for i := range nfsList {
        nfsList[i] = strings.Trim(nfsList[i], "\"")
    }
    return nfsList, err
}
