package f5lb

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/caicloud/loadbalancer-provider/core/provider"
	ibclient "github.com/infobloxopen/infoblox-go-client"
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

	lbname    string
	rule2Host map[string]string
}

func newInfobloxClient(d provider.Device, lbnamespace, lbname string) (DNSClient, error) {
	bs, err := base64.StdEncoding.DecodeString(d.Auth.Password)
	if err != nil {
		return nil, err
	}

	version := strings.TrimLeft(d.Config.APIVersion, "v")

	// static assert for manual change in ./vendor.
	// if you meet 'ModificationReminder undefined' error here, that means you may revert the code change in vendor.
	// You should:
	//  1. review pr #143 and make the change again
	//  2. or, if infobloxopen/infoblox-go-client supports return an empty getBasicEA(), this assert code can be removed.
	if ibclient.NewObjectManager(nil, "", "").ModificationReminder() != 0 {
		log.Fatalf("no ea should be set, otherwise the CreateARecord will fail")
	}

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
		rule2Host: make(map[string]string),
	}

	return &c, nil
}

func (c *infobloxDNSClient) cacheRuleHost(dns *dnsInfo, hostName string) {
	for _, r := range dns.rules {
		if hostName == "" {
			delete(c.rule2Host, r)
			continue
		}
		c.rule2Host[r] = hostName
	}
}

func (c *infobloxDNSClient) loadRuleHost(ruleName string) string {
	return c.rule2Host[ruleName]
}

func (c *infobloxDNSClient) EnsureIngress(dns *dnsInfo) error {
	var err error
	oldHostName := c.loadRuleHost(dns.rules[0])

	// handle hostName change
	if oldHostName != "" && oldHostName != dns.hostName {
		log.Warningf("host name change")
		err = c.deleteHost(oldHostName)
		if err != nil {
			log.Warningf("Failed to delete old hostName %s:%v", oldHostName, err)
		}
		c.cacheRuleHost(dns, "")
	}

	if dns.hostName != "" {
		err = c.ensureHost(dns.hostName, dns)
		if err != nil {
			return err
		}
		c.cacheRuleHost(dns, dns.hostName)
	}

	return nil
}

func (c *infobloxDNSClient) DeleteIngress(dns *dnsInfo) error {
	var err error
	if dns.hostName != "" {
		err = c.deleteHost(dns.hostName)
		if err != nil {
			return err
		}
		c.cacheRuleHost(dns, "")
	}

	return nil
}

func (c *infobloxDNSClient) ensureHost(hostName string, d *dnsInfo) error {
	var err error

	if d.Zone == "" || !strings.HasSuffix(d.hostName, d.Zone) {
		log.Errorf("DNS record has an invalid zone: %v", d)
		return fmt.Errorf("invalid zone")
	}

	view, err := c.getActiveView()
	if err != nil {
		log.Errorf("Failed to get active view for %s, err:%v", hostName, err)
		return err
	}
	comment := serializeMetadata(c.lbname, d)
	rec, _ := c.getRecord(hostName, view)
	if rec != nil {
		thisLB, _ := c.checkOwner(rec.Comment)
		if !thisLB || rec.Ipv4Addr != d.Addr {
			log.Errorf("Failed to ensure conflict host %s on f5, rec.ip %s, desire %s", hostName, rec.Ipv4Addr, d.Addr)
			return fmt.Errorf("conflict domain name")
		}

		if rec.Ipv4Addr == d.Addr && rec.Comment == comment {
			return nil
		}

		log.Infof("ibclient.UpdateARecord view:%s, hostName:%s, ip:%s->%s, comment:%s->%s, ref:%s",
			view, hostName, rec.Ipv4Addr, d.Addr, rec.Comment, comment, rec.Ref)
		_, err = c.client.UpdateARecord(ibclient.RecordA{Ref: rec.Ref, Ipv4Addr: d.Addr, Comment: comment})
		if err != nil {
			log.Errorf("Failed to udpate record %s, err:%v", hostName, err)
		}
		return err
	}

	_, err = c.getZoneAuth(view, d.Zone)
	if err != nil {
		log.Errorf("Failed to get zone %s in view %s for %s, err:%v", d.Zone, view, hostName, err)
		return err
	}

	log.Infof("ibclient.CreateARecord view:%s, hostName:%s, ip:%s, comment: %s", view, hostName, d.Addr, comment)
	_, err = c.client.CreateARecord(ibclient.RecordA{Name: hostName, Ipv4Addr: d.Addr, Comment: comment, View: view})
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
		thisLB, _ := c.checkOwner(rec.Comment)
		if !thisLB {
			log.Errorf("Failed to ensure host because of conflict domain name %s on f5", hostName)
			return nil
		}
		//TODO

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

	if len(*recs) == 1 {
		return &((*recs)[0]), nil
	}

	return nil, fmt.Errorf("Too Many records for %s: num=%v", hostName, len(*recs))
}

func (c *infobloxDNSClient) EnsureDNSRecords(dnsInfos *dnsInfoList, l47 string) error {
	var err error
	log.Infof("EnsureDNSRecords %s dl: %v", l47, dnsInfos)

	for _, d := range *dnsInfos {
		if d.l47 != l47 {
			log.Warningf("skip dnsinfo %+v for infodns, because l47 not match", d)
			continue
		}
		if d.status == "" {
			err = c.ensureHost(d.hostName, d)
			if err != nil {
				d.status = err.Error()
				continue
			} else {
				c.cacheRuleHost(d, d.hostName)
				d.status = statusOK
			}
		}
	}

	view, err := c.getActiveView()
	if err != nil {
		log.Errorf("Failed to get active view for err:%v", err)
		return err
	}
	recs, err := c.client.GetARecord(ibclient.RecordA{View: view})
	if err != nil {
		return err
	}
	for _, rec := range *recs {
		thisLB, d := c.checkOwner(rec.Comment)
		if !thisLB || d.l47 != l47 {
			continue
		}
		found := false
		for _, d := range *dnsInfos {
			if d.hostName == rec.Name {
				found = true
				d.status = statusOK
				break
			}
		}
		if !found {
			log.Warningf("Has orphan record %v, try to delete it", rec)
			_ = c.deleteHost(rec.Name)
		}
	}
	return nil
}

func (c *infobloxDNSClient) checkOwner(info string) (bool, *dnsInfo) {
	lb, d := unserializeMetadata(info)
	if lb != c.lbname {
		return false, d
	}
	return true, d
}
