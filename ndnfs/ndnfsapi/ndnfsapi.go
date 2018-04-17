package ndnfsapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Nexenta/nedge-docker-nfs/ndnfs/nedgeprovider"
	log "github.com/Sirupsen/logrus"
)

const defaultMountPoint string = "/var/lib/ndnfs"

var (
	DN = "ndnfsapi "
)

type Client struct {
	Nedge  nedgeprovider.INexentaEdge
	Config *Config
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

	if conf.Mountpoint == "" {
		conf.Mountpoint = defaultMountPoint
	}

	NdnfsClient := &Client{
		Nedge:  nedgeprovider.InitNexentaEdgeProvider(conf.Nedgerest, conf.Nedgeport, conf.Username, conf.Password),
		Config: &conf,
	}
	return NdnfsClient, nil
}

func (c *Client) CreateVolume(name string, options map[string]string) (err error) {
	log.Info(DN, ": Creating volume ", name)
	var service, cluster, tenant = c.Config.Servicename, c.Config.Clustername, c.Config.Tenantname

	if os.Getenv("CCOW_SVCNAME") != "" {
		service = os.Getenv("CCOW_SVCNAME")
	}

	err = c.Nedge.CreateBucket(cluster, tenant, name, 100, options)
	if err != nil {
		log.Error(err)
		return err
	}

	if acl, ok := options["acl"]; ok {
		err = c.Nedge.SetServiceAclConfiguration(service, cluster, tenant, acl)
	}

	c.Nedge.ServeService(service, cluster, tenant, name)
	if err != nil {
		log.Error(err)
		return err
	}

	return err
}

func (c *Client) DeleteVolume(name string) (err error) {
	log.Debug(DN, "Deleting Volume ", name)

	var service, cluster, tenant = c.Config.Servicename, c.Config.Clustername, c.Config.Tenantname
	if os.Getenv("CCOW_SVCNAME") != "" {
		service = os.Getenv("CCOW_SVCNAME")
	}

	// before unserve bucket we need to unset ACL property
	err = c.Nedge.UnsetServiceAclConfiguration(service, tenant, name)
	if err != nil {
		log.Errorf("Error removing acl parameters %+v", err)
		return err
	}

	err = c.Nedge.UnserveService(service, cluster, tenant, name)
	if err != nil {
		log.Errorf("Error unserve bucket %s Error: %+v", name, err)
		return err
	}

	err = c.Nedge.DeleteBucket(cluster, tenant, name)
	if err != nil {
		log.Errorf("Error deleting volume %+v", err)
		return err
	}

	return err
}

func (c *Client) MountVolume(name string) (mnt string, err error) {
	log.Debug(DN, "Mounting Volume ", name)

	mnt = filepath.Join(c.Config.Mountpoint, name)
	if out, err := exec.Command("mkdir", "-p", mnt).CombinedOutput(); err != nil {
		log.Info("Error running mkdir command: ", err, "{", string(out), "}")
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

	mnt := filepath.Join(c.Config.Mountpoint, name)
	log.Info(DN, " Mountpoint to delete : ", mnt)
	if out, err := exec.Command("rm", "-rf", mnt).CombinedOutput(); err != nil {
		log.Info("Error running rm command: ", err, "{", string(out), "}")
	}

	log.Debug(DN, "Unmounting Volume ", name)
	nfs := fmt.Sprintf("%s:/%s/%s", c.Config.Nedgedata, c.Config.Tenantname, name)
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

/* map: {bucket: mountpoint/bucket} */
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
	var service, cluster, tenant = c.Config.Servicename, c.Config.Clustername, c.Config.Tenantname
	if os.Getenv("CCOW_SVCNAME") != "" {
		service = os.Getenv("CCOW_SVCNAME")
	}

	volumes, err := c.Nedge.GetNfsVolumes(service)

	// list of shares
	for _, nedgeVolume := range volumes {
		// filter volumes related to current config cluster/tenant
		if strings.HasPrefix(nedgeVolume.Path, fmt.Sprintf("%s/%s", cluster, tenant)) {
			nfsList = append(nfsList, nedgeVolume.Share)
		}
	}
	return nfsList, err
}
