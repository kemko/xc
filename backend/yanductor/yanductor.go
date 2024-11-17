package yanductor

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/viert/xc/config"
	"github.com/viert/xc/store"
)

// New creates a new instance of Yanductor backend
func New(cfg *config.XCConfig) (*Yanductor, error) {
	y := &Yanductor{
		cacheTTL:    cfg.CacheTTL,
		cacheDir:    cfg.CacheDir,
		hosts:       make([]*store.Host, 0),
		groups:      make([]*store.Group, 0),
		workgroups:  make([]*store.WorkGroup, 0),
		datacenters: make([]*store.Datacenter, 0),
		parentMap:   make(map[string]string),
	}

	options := cfg.BackendCfg.Options
	// workgroups configuration
	wgString, found := options["work_groups"]
	if !found || wgString == "" {
		return nil, fmt.Errorf("yanductor backend workgroups are not configured")
	} else {
		splitExpr := regexp.MustCompile(`\s*,\s*`)
		y.workgroupNames = splitExpr.Split(wgString, -1)
	}

	// apiURL configuration
	apiURL, found := options["url"]
	if !found {
		return nil, fmt.Errorf("yanductor backend API URL is not configured")
	}

	y.apiURL = apiURL

	// Load data to populate fields
	err := y.Load()
	if err != nil {
		return nil, fmt.Errorf("error loading data: %s", err)
	}

	return y, nil
}

// Hosts returns the list of hosts
func (y *Yanductor) Hosts() []*store.Host {
	return y.hosts
}

// Groups returns the list of groups
func (y *Yanductor) Groups() []*store.Group {
	return y.groups
}

// WorkGroups returns the list of workgroups
func (y *Yanductor) WorkGroups() []*store.WorkGroup {
	return y.workgroups
}

// Datacenters returns the list of datacenters
func (y *Yanductor) Datacenters() []*store.Datacenter {
	return y.datacenters
}

// Load tries to load data from cache unless it's expired
// In case of cache expiration or absence it triggers Reload()
func (y *Yanductor) Load() error {
	if y.cacheExpired() {
		return y.Reload()
	}
	return y.loadLocal()
}

// Reload forces reloading data from HTTP(S)
func (y *Yanductor) Reload() error {
	err := y.loadRemote()
	if err != nil {
		return y.loadLocal()
	}
	return nil
}

func (y *Yanductor) loadLocal() error {
	data, err := os.ReadFile(y.cacheFilename())
	if err != nil {
		return err
	}
	return y.parseData(data)
}

func (y *Yanductor) cacheExpired() bool {
    st, err := os.Stat(y.cacheFilename())
    if err != nil {
        return os.IsNotExist(err)
    }
    return st.ModTime().Add(y.cacheTTL).Before(time.Now())
}

func (y *Yanductor) cacheFilename() string {
    return path.Join(y.cacheDir, fmt.Sprintf("yanductor_cache_%s.json", strings.Join(y.workgroupNames, "_")))
}


func (y *Yanductor) saveCache(data []byte) error {
	err := os.MkdirAll(y.cacheDir, 0755)
	if err != nil {
		return fmt.Errorf("error creating cache dir: %s", err)
	}
	return os.WriteFile(y.cacheFilename(), data, 0644)
}

func (y *Yanductor) loadRemote() error {
	url := fmt.Sprintf("%s/api/generator/rivik.ansible-inventory?projects=%s", y.apiURL, strings.Join(y.workgroupNames, ","))
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("status code %d while fetching %s", resp.StatusCode, url)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	err = y.parseData(data)
	if err != nil {
		return err
	}

	return y.saveCache(data)
}

func (y *Yanductor) parseData(data []byte) error {
    var rawData map[string]interface{}
    err := json.Unmarshal(data, &rawData)
    if err != nil {
        return err
    }

    y.hosts = make([]*store.Host, 0)
    y.groups = make([]*store.Group, 0)
    y.datacenters = make([]*store.Datacenter, 0)
    y.parentMap = make(map[string]string)

    meta, metaOk := rawData["_meta"].(map[string]interface{})
    if !metaOk {
        return fmt.Errorf("invalid data format: missing _meta section")
    }

    hostvars, hostvarsOk := meta["hostvars"].(map[string]interface{})
    if !hostvarsOk {
        return fmt.Errorf("invalid data format: missing hostvars section")
    }

    for group, groupData := range rawData {
        if group == "_meta" {
            continue
        }

        groupMap, groupMapOk := groupData.(map[string]interface{})
        if !groupMapOk {
            continue
        }

        if children, ok := groupMap["children"].([]interface{}); ok {
            for _, child := range children {
                childStr, childStrOk := child.(string)
                if childStrOk {
                    if _, exists := y.parentMap[childStr]; !exists {
                        y.parentMap[childStr] = group
                    }
                }
            }
        }

        groupObj := &store.Group{
            Name:     group,
            ParentID: y.parentMap[group],
        }
        y.groups = append(y.groups, groupObj)

        if hosts, ok := groupMap["hosts"].([]interface{}); ok {
            for _, host := range hosts {
                hostName, hostNameOk := host.(string)
                if !hostNameOk {
                    continue
                }

                hostInfo, hostInfoOk := hostvars[hostName].(map[string]interface{})
                if !hostInfoOk {
                    continue
                }

                dc, dcOk := hostInfo["dc"].(string)
                if !dcOk {
                    dc = ""
                }

                hostObj := &store.Host{
                    FQDN:         hostName,
                    GroupID:      group,
                    DatacenterID: dc,
                }
                y.hosts = append(y.hosts, hostObj)
                groupObj.Hosts = append(groupObj.Hosts, hostObj)

                if !contains(y.datacenters, dc) {
                    y.datacenters = append(y.datacenters, &store.Datacenter{Name: dc})
                }
            }
        }
    }

    return nil
}

func contains(slice []*store.Datacenter, item string) bool {
	for _, s := range slice {
		if s.Name == item {
			return true
		}
	}
	return false
}
