package yanductor

import (
	"time"

	"github.com/viert/xc/store"
)

// Yanductor is a backend based on Conductor API
type Yanductor struct {
    workgroupNames []string
    cacheTTL       time.Duration
    cacheDir       string
    apiURL         string
    hosts          []*store.Host
    groups         []*store.Group
    workgroups     []*store.WorkGroup
    datacenters    []*store.Datacenter
    parentMap      map[string]string
}

// Host represents a host in the inventory
type Host struct {
    Name        string `json:"name"`
    Group       string `json:"group"`
    Datacenter  string `json:"dc"`
}

// Group represents a group in the inventory
type Group struct {
    Name   string `json:"name"`
    Parent string `json:"parent"`
}
