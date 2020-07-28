package f5lb

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/caicloud/loadbalancer-provider/core/provider"
	ibclient "github.com/infobloxopen/infoblox-go-client"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	log "k8s.io/klog"
)

/*
1. get active view
> https://{XXX.XX.XXX.XXX}/wapi/{v2.3.1}/view?_return_fields=name&_return_fields=disable
<
[ { u'_ref': u'view/ZG5zLnZpZXckLl9kZWZhdWx0:default/true',
    u'disable': True,
    u'name': u'default'},
  { u'_ref': u'view/ZG5zLnZpZXckLjc:DR_view/false',
    u'disable': False,
    u'name': u'DR_view'},
  { u'_ref': u'view/ZG5zLnZpZXckLjEx:Default_view/false',
    u'disable': True,
    u'name': u'Default_view'},
  { u'_ref': u'view/ZG5zLnZpZXckLjEz:view_01/false',
    u'disable': True,
    u'name': u'view_01'},
  { u'_ref': u'view/ZG5zLnZpZXckLjE0:view_02/false',
    u'disable': True,
    u'name': u'view_02'},
  { u'_ref': u'view/ZG5zLnZpZXckLjE1:view_03/false',
    u'disable': True,
    u'name': u'view_03'},
  { u'_ref': u'view/ZG5zLnZpZXckLjE2:view_04/false',
    u'disable': True,
    u'name': u'view_04'},
  { u'_ref': u'view/ZG5zLnZpZXckLjE3:view_05/false',
    u'disable': True,
    u'name': u'view_05'},
  { u'_ref': u'view/ZG5zLnZpZXckLjE4:view_06/false',
    u'disable': True,
    u'name': u'view_06'},
  { u'_ref': u'view/ZG5zLnZpZXckLjE5:view_07/false',
    u'disable': True,
    u'name': u'view_07'},
  { u'_ref': u'view/ZG5zLnZpZXckLjIw:view_08/false',
    u'disable': True,
    u'name': u'view_08'}]

2. check zone
> https://XXX.XX.XXX.XXX/wapi/v2.3.1/zone_auth?view={active_view}&fqdn={zone}
<
'''
[ { u'_ref': u'zone_auth/ZG5zLnpvbmUkLjcuY29tLnRlc3QtamFja3k:test-jacky.com/DR_view',
    u'fqdn': u'test-jacky.com',
    u'view': u'DR_view'}]
'''

3. create
> https://XXX.XX.XXX.XXX/wapi/v2.3.1/record:a  # or record:cname
> A: {"name":"test1.test-jacky.com", "ipv4addr":"99.99.99.90", "comment": "测试1", "view": "aview"}
> CNAME: {"name":"test3.test-jacky.com", "canonical":"test2.test-jacky.com", "comment": "测试2", "view": "aview"}

4. get
> https://XXX.XX.XXX.XXX/wapi/v2.3.1/record:a?view=aview&name=test1.test-jacky.com
< [ { u'_ref': u'record:a/ZG5zLmJpbmRfYSQuNy5jb20udGVzdC1qYWNreSx0ZXN0NSw3Ny43Ny43Ny43MA:test5.test-jacky.com/DR_view',
    u'ipv4addr': u'77.77.77.70',
    u'name': u'test5.test-jacky.com',
    u'view': u'DR_view'}]

4. update # url is from '_ref'
> https://XXX.XX.XXX.XXX/wapi/v2.3.1/record:a/ZG5zLmJpbmRfYSQuNy5jb20udGVzdC1qYWNreSx0ZXN0NSw3Ny43Ny43Ny43MA:test5.test-jacky.com/DR_view
> {"ipv4addr":"99.99.99.90", "comment": "测试1"}

*/

//https://github.com/infobloxopen/infoblox-go-client/tree/release/v2.0.0

type infobloxDNSClient struct {
	client    *ibclient.ObjectManager
	connector *ibclient.Connector

	lbname string
}

func newInfobloxClient(d provider.Device, lbnamespace, lbname string) (DNSClient, error) {
	bs, err := base64.StdEncoding.DecodeString(d.Auth.Password)
	if err != nil {
		return nil, err
	}

	version := strings.TrimLeft(d.Config.APIVersion, "v")

	hostConfig := ibclient.HostConfig{
		Host:     d.ManageAddr,
		Version:  version,
		Username: d.Auth.User,
		Password: string(bs),
	}
	transportConfig := ibclient.NewTransportConfig("false", 20, 10)
	requestBuilder := &ibclient.WapiRequestBuilder{}
	requestor := &ibclient.WapiHttpRequestor{}
	conn, err := ibclient.NewConnector(hostConfig, transportConfig, requestBuilder, requestor)
	if err != nil {
		log.Errorf("Failed to ibclient.NewConnector ip:%s, user:%s", hostConfig.Host, hostConfig.Username)
		return nil, err
	}

	client := ibclient.NewObjectManager(conn, "myclient", "")

	c := infobloxDNSClient{
		client:    client,
		connector: conn,
		lbname:    fmt.Sprintf("%s.%s", lbnamespace, lbname),
	}

	return &c, nil
}

func (c *infobloxDNSClient) EnsureIngress(ing *v1beta1.Ingress, dns *provider.Record) error {
	hostName := ""
	for _, r := range ing.Spec.Rules {
		if dns.Zone != "" && strings.HasSuffix(r.Host, dns.Zone) {
			hostName = r.Host
		} else {
			log.Warningf("skip ingress host: %s, zone: %s", r.Host, dns.Zone)
		}
	}

	var err error
	if hostName != "" {
		err = c.ensureHost(hostName, dns.Addr, dns.Zone)
	}

	return err
}

func (c *infobloxDNSClient) DeleteIngress(ing *v1beta1.Ingress, dns *provider.Record) error {
	hostName := ""
	for _, r := range ing.Spec.Rules {
		if dns.Zone != "" && strings.HasSuffix(r.Host, dns.Zone) {
			hostName = r.Host
		} else {
			log.Warningf("skip ingress host: %s, zone: %s", r.Host, dns.Zone)
		}
	}
	if hostName != "" {
		return c.deleteHost(hostName)
	}
	return nil
}

func (c *infobloxDNSClient) getKeyCommnet() string {
	return fmt.Sprintf("# updated by =%s=", c.lbname)
}

func (c *infobloxDNSClient) ensureHost(hostName string, addr string, zone string) error {
	var err error
	view, err := c.getActiveView()
	if err != nil {
		log.Errorf("Failed to get active view for %s, err:%v", hostName, err)
		return err
	}
	comment := c.getKeyCommnet()
	rec, _ := c.getRecord(hostName, view)
	if rec != nil {
		if rec.Ipv4Addr == addr {
			return nil
		}

		log.Infof("ibclient.UpdateARecord view:%s, hostName:%s, ip:%s->%s, ref:%s", view, hostName, rec.Ipv4Addr, addr, rec.Ref)
		_, err = c.client.UpdateARecord(ibclient.RecordA{Ref: rec.Ref, Ipv4Addr: addr, Comment: comment})
		if err != nil {
			log.Errorf("Failed to udpate record %s, err:%v", hostName, err)
		}
		return err
	}

	_, err = c.getZoneAuth(view, zone)
	if err != nil {
		log.Errorf("Failed to get zone %s in view %s for %s, err:%v", zone, view, hostName, err)
		return err
	}

	log.Infof("ibclient.CreateARecord view:%s, hostName:%s, ip:%s, comment: %s", view, hostName, addr, comment)
	_, err = c.client.CreateARecord(ibclient.RecordA{Name: hostName, Ipv4Addr: addr, Comment: comment, View: view})
	if err != nil {
		log.Errorf("Failed to create record %s, err:%v", hostName, err)
	}
	return err
}

func (c *infobloxDNSClient) deleteHost(hostName string) error {
	view, err := c.getActiveView()
	if err != nil {
		log.Errorf("Failed to get active view for %s, err:%v", hostName, err)
		return err
	}
	rec, err := c.getRecord(hostName, view)
	if err != nil {
		log.Warningf("Failed to get record %s in view %s to delete: %v", hostName, view, err)
		if isNotFoundError(err) {
			err = nil
		}
		return err
	}

	if rec != nil {
		log.Infof("ibclient.DeleteARecord view:%s, hostName:%s, ip:%s, ref:%s", view, hostName, rec.Ipv4Addr, rec.Ref)
		_, err = c.client.DeleteARecord(ibclient.RecordA{Ref: rec.Ref})
		if err != nil {
			log.Errorf("Failed to delete record %s, err:%v", hostName, err)
			return err
		}
	}
	return nil
}

const notFoundError = "No Record Found"

func isNotFoundError(e error) bool {
	return strings.HasPrefix(e.Error(), notFoundError)
}

func (c *infobloxDNSClient) getRecord(hostName, view string) (*ibclient.RecordA, error) {
	recs, err := c.client.GetARecord(ibclient.RecordA{Name: hostName, View: view})
	if err != nil {
		return nil, err
	}
	if recs == nil {
		return nil, fmt.Errorf("%s for %s", notFoundError, hostName)
	}
	comment := c.getKeyCommnet()
	for i, rec := range *recs {
		if strings.Contains(rec.Comment, comment) {
			return &((*recs)[i]), nil
		}
	}

	if len(*recs) == 1 {
		log.Warningf("An unmatched comment record returned %+v", (*recs)[0])
		return &((*recs)[0]), nil
	}

	return nil, fmt.Errorf("Too Many records for %s: num=%v", hostName, len(*recs))
}
