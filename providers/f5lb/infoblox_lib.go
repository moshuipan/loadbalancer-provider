package f5lb

import (
	ibclient "github.com/infobloxopen/infoblox-go-client"
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

func (c *infobloxDNSClient) getActiveView() (string, error) {

	type View struct {
		MyIBBase `json:"-"`
		Ref      string `json:"_ref,omitempty"`
		Name     string `json:"name,omitempty"`
		Disable  bool   `json:"disable,omitempty"`
	}
	req := View{}
	req.objectType = "view"
	req.returnFields = []string{"name", "disable"}

	var res []View

	err := c.connector.GetObject(&req, "", &res)
	if err != nil {
		return "", err
	}
	viewName := ""
	for _, v := range res {
		if !v.Disable {
			viewName = v.Name
		}
	}

	return viewName, nil
}

func (c *infobloxDNSClient) getZoneAuth(view, fqdn string) ([]ibclient.ZoneAuth, error) {
	if len(fqdn) == 0 {
		return nil, nil
	}
	var res []ibclient.ZoneAuth

	zoneAuth := ibclient.NewZoneAuth(ibclient.ZoneAuth{View: view, Fqdn: fqdn})
	err := c.connector.GetObject(zoneAuth, "", &res)

	return res, err
}
