package shadow

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// catalogSource is a verbatim copy of clown's capability catalog contract
// (clown internal/0/catalogcontract/catalog.json, RFC 0018, copied from clown
// master 59954b9411d4). It is the source of the field typing; do not edit it
// here. Refresh it by copying the file again. A manifest key that replaces
// the copy is RFC 0018 section 9.4 and is not decided.
//
//go:embed catalog.json
var catalogSource string

type catalogField struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Resolve    string `json:"resolve"`
	Op         string `json:"op"`
	Split      string `json:"split"`
	RelativeTo string `json:"relative_to"`
}

type catalogTool struct {
	Plugin   string         `json:"plugin"`
	Server   string         `json:"server"`
	Tool     string         `json:"tool"`
	Kind     string         `json:"kind"`
	Presents string         `json:"presents"`
	Fields   []catalogField `json:"fields"`
}

func (t catalogTool) field(name string) (catalogField, bool) {
	for _, f := range t.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return catalogField{}, false
}

type schemeOwner struct {
	Scheme string `json:"scheme"`
	Plugin string `json:"plugin"`
	Server string `json:"server"`
}

type catalog struct {
	Schemes []schemeOwner `json:"schemes"`
	Tools   []catalogTool `json:"tools"`
}

func loadCatalog() (*catalog, error) {
	var c catalog
	if err := json.Unmarshal([]byte(catalogSource), &c); err != nil {
		return nil, fmt.Errorf("shadow: embedded catalog.json: %w", err)
	}
	return &c, nil
}

func (c *catalog) tool(plugin, server, tool string) (catalogTool, bool) {
	for _, t := range c.Tools {
		if t.Plugin == plugin && t.Server == server && t.Tool == tool {
			return t, true
		}
	}
	return catalogTool{}, false
}

func (c *catalog) schemeOwner(scheme string) (schemeOwner, bool) {
	for _, s := range c.Schemes {
		if s.Scheme == scheme {
			return s, true
		}
	}
	return schemeOwner{}, false
}
