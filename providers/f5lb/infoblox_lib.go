package f5lb

import (
	"fmt"

	ibclient "github.com/hanxueluo/infoblox-go-client"
)

type MyIBBase struct {
	objectType   string
	returnFields []string
	eaSearch     ibclient.EASearch
}

func (obj *MyIBBase) ObjectType() string {
	return obj.objectType
}

func (obj *MyIBBase) ReturnFields() []string {
	return obj.returnFields
}

func (obj *MyIBBase) EaSearch() ibclient.EASearch {
	return obj.eaSearch
}

// View ...
type View struct {
	MyIBBase `json:"-"`
	Ref      string `json:"_ref,omitempty"`
	Name     string `json:"name,omitempty"`
	Disable  bool   `json:"disable,omitempty"`
}

func listViews(conn *ibclient.Connector) ([]View, error) {
	var res []View

	req := View{}
	req.objectType = "view"
	req.returnFields = []string{"name", "disable"}

	err := conn.GetObject(&req, "", &res)
	return res, err
}

func (c *infobloxDNSClient) getActiveView() (string, error) {
	res, err := listViews(c.connector)
	if err != nil {
		return "", err
	}
	viewName := ""
	for _, v := range res {
		if !v.Disable {
			viewName = v.Name
		}
	}
	if viewName == "" {
		err = fmt.Errorf("no active view found")
	}

	return viewName, err
}

func (c *infobloxDNSClient) getZoneAuth(view, fqdn string) ([]ibclient.ZoneAuth, error) {
	if len(fqdn) == 0 {
		return nil, nil
	}
	var res []ibclient.ZoneAuth

	zoneAuth := ibclient.NewZoneAuth(ibclient.ZoneAuth{View: view, Fqdn: fqdn})
	err := c.connector.GetObject(zoneAuth, "", &res)

	if err == nil && len(res) == 0 {
		err = fmt.Errorf("zone %s not found", fqdn)
	}

	return res, err
}

func validateConnector(conn *ibclient.Connector) error {
	_, err := listViews(conn)
	return err
}
