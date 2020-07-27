package f5lb

import (
	"fmt"
	"strings"
	"time"

	gobigip "github.com/hanxueluo/go-bigip"

	"encoding/base64"

	"github.com/caicloud/loadbalancer-provider/core/provider"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	log "k8s.io/klog"
)

type f5DNSClient struct {
	f5CommonClient
	//virtualServers [][]string
	poolPrefix string
	lbname     string
}
type f5CommonClient struct {
	f5 *gobigip.BigIP
	d  provider.Device
	t  time.Time
}

func refreshToken(c *f5CommonClient) error {
	var err error

	n := time.Now()
	// default timeout of f5 token is 1200s, but we will refresh it in ervery 120s
	if c.f5 == nil || time.Now().Add(time.Duration(-120)*time.Second).After(c.t) {
		defer func() {
			if err != nil {
				log.Errorf("Failed to refresh device %s token:%v", c.d.Name, err)
			}
		}()

		bs, err := base64.StdEncoding.DecodeString(c.d.Auth.Password)
		if err != nil {
			return err
		}

		log.Infof("gobigip.NewTokenSession for %s device: %v", c.d.Name, n)
		f5, err := gobigip.NewTokenSession(c.d.ManageAddr, c.d.Auth.User, string(bs), "", nil)
		if err != nil {
			return err
		}
		c.f5 = f5
		c.t = n
	}
	return nil
}

/*
gtm pool /Common/pool_all_3D {
    alternate-mode none
    fallback-mode global-availability
    load-balancing-mode topology
    members {
    ¦   /Common/ct01_beautrio:vs_ct01_beautrio {
    ¦   ¦   order 0
    ¦   }
    ¦   /Common/unicom01_beautrio:vs_unicom01_beautrio {
    ¦   ¦   order 1
    ¦   }
    }
    monitor /Common/gateway_icmp
}
*/

// NewF5DNSClient ...
func newF5DNSClient(d provider.Device, lbnamespace, lbname string) (DNSClient, error) {
	lbclient := &f5DNSClient{
		poolPrefix: d.Config.PoolPrefix,
		lbname:     fmt.Sprintf("%s.%s", lbname, lbnamespace),
	}
	lbclient.d = d

	err := refreshToken(&lbclient.f5CommonClient)
	if err != nil {
		return nil, err
	}

	log.Infof("Trying f5gtm list pool API call:%s, user:%s", d.ManageAddr, d.Auth.User)
	_, err = lbclient.f5.GetGTMCNamePools()
	if err != nil {
		return nil, err
	}

	return lbclient, nil
}

func (c *f5DNSClient) EnsureIngress(ing *v1beta1.Ingress, dns *provider.Record) error {
	err := refreshToken(&c.f5CommonClient)
	if err != nil {
		return err
	}
	hostName := ""
	for _, r := range ing.Spec.Rules {
		if dns.Zone != "" && strings.HasSuffix(r.Host, dns.Zone) {
			hostName = r.Host
		} else {
			log.Warningf("skip ingress host: %s, zone: %s", r.Host, dns.Zone)
		}
	}

	if hostName != "" {
		virtualServers := strings.Split(dns.Addr, ",")
		return c.ensureHost(hostName, virtualServers)
	}

	return nil
}

func (c *f5DNSClient) DeleteIngress(ing *v1beta1.Ingress, dns *provider.Record) error {
	err := refreshToken(&c.f5CommonClient)
	if err != nil {
		return err
	}
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

func (c *f5DNSClient) getPoolName(hostName string) string {
	// name rule:  xxx-yyy-zzz.domain.com -> ${prefix}_zzz_yyy_xxx
	ns := strings.Split(hostName, ".")
	name := ""
	for _, s := range strings.Split(ns[0], "-") {
		name = s + "_" + name
	}
	name = strings.TrimRight(name, "_")
	return c.poolPrefix + "_" + name
}

func (c *f5DNSClient) ensurePool(name string) (*gobigip.GTMAPool, error) {
	obj, err := c.f5.GetGTMAPool(name)

	if err != nil {
		log.Errorf("Failed to get %s: %v", name, err)
		return nil, err
	}

	if obj == nil {
		config := &gobigip.GTMAPool{
			Name:              name,
			Description:       fmt.Sprintf("create by %s.", c.lbname),
			Monitor:           "/Common/gateway_icmp",
			LoadBalancingMode: "topology",
			FallbackMode:      "global-availability",
		}
		log.Infof("f5.AddGTMAPool %v", config)
		err = c.f5.AddGTMAPool(config)
		if err != nil {
			log.Errorf("Failed to create %s: %v", name, err)
			return nil, err
		}
		obj, err = c.f5.GetGTMAPool(name)
		if err != nil {
			log.Errorf("Failed to get %s after create: %v", name, err)
			return nil, err
		}
	}
	return obj, nil
}

func (c *f5DNSClient) deleteHost(hostName string) error {
	wip, err := c.f5.GetGTMWideIP(hostName)
	if err != nil {
		log.Errorf("Failed to GetGTMWideIP %s when delete: %v ", hostName, err)
		return err
	}
	if wip != nil {
		log.Infof("f5.DeleteGTMWideIP %s", hostName)
		err = c.f5.DeleteGTMWideIP(hostName)
		if err != nil {
			log.Errorf("Failed to DeleteGTMWideIP %s: %v ", hostName, err)
			return err
		}
	}

	poolName := c.getPoolName(hostName)
	pool, err := c.f5.GetGTMAPool(poolName)
	if err != nil {
		log.Errorf("Failed to GetGTMAPool %s when delete: %v ", poolName, err)
		return err
	}
	if pool != nil {
		log.Infof("f5.DeleteGTMPool %s", poolName)
		err = c.f5.DeleteGTMPool(poolName)
		if err != nil {
			log.Warningf("Failed to DeleteGTMPool %s: %v ", poolName, err)
			return err
		}
	}
	return nil
}

func (c *f5DNSClient) ensureHost(hostName string, virtualServers []string) error {
	poolName := c.getPoolName(hostName)

	// ensure pool
	_, err := c.ensurePool(poolName)
	if err != nil {
		return err
	}

	// ensure pool members
	poolMembers, err := c.f5.GetGTMAPoolMembers(poolName)
	if err != nil {
		log.Errorf("Failed to get pool member %s", poolName)
		return err
	}

	deleted := []gobigip.GTMAPoolMember{}
	var added [][]string
	for _, vsname := range virtualServers {
		sps := strings.SplitN(vsname, ":", 2)
		if len(sps) != 2 {
			log.Errorf("Incorrect vs name format: %s", vsname)
			continue
		}
		added = append(added, sps)
	}
	for j, pm := range poolMembers.GTMAPoolMembers {
		found := false
		for i, a := range added {
			fullPath := a[0] + ":" + a[1]
			if pm.FullPath == fullPath {
				// delete added[i]
				added[i] = added[len(added)-1]
				added = added[:len(added)-1]
				found = true
				break
			}
		}
		if !found {
			deleted = append(deleted, poolMembers.GTMAPoolMembers[j])
		}
	}

	log.Infof("updating pool member add: %v, delete: %v", added, deleted)
	for _, a := range added {
		log.Infof("f5.CreateGTMAPoolMember %s: %s:%s", poolName, a[0], a[1])
		err = c.f5.CreateGTMAPoolMember(poolName, a[0], a[1])
		if err != nil {
			log.Errorf("Failed to CreateGTMAPoolMember %s: %s:%s: %v ", poolName, a[0], a[1], err)
			return err
		}
	}

	for _, d := range deleted {
		log.Infof("f5.DeleteGTMAPoolMember %s: %s:%s", poolName, d.Name, d.SubPath)
		err = c.f5.DeleteGTMAPoolMember(poolName, d.Name, d.SubPath)
		if err != nil {
			log.Errorf("Failed to DeleteGTMAPoolMember %s: %s:%s: %v ", poolName, d.Name, d.SubPath, err)
			return err
		}
	}

	// ensure wide ip
	wip, err := c.f5.GetGTMWideIP(hostName)
	if err != nil {
		log.Errorf("Failed to get WIP %s: %v ", hostName, err)
	}

	pools := []gobigip.GTMWideIPPool{
		{Name: poolName},
	}
	o := gobigip.GTMWideIP{
		Name:        hostName,
		Pools:       &pools,
		Description: fmt.Sprintf("create by %s.", c.lbname),
	}
	if wip == nil {
		log.Infof("f5.AddGTMWideIP %s, %+v", hostName, pools)
		err = c.f5.AddGTMWideIP(&o)
		if err != nil {
			log.Errorf("Failed to AddGTMWideIP %s: %v ", hostName, err)
			return err
		}
	} else {
		wip.Pools = &pools
		log.Infof("f5.ModifyGTMWideIP %s, %+v", hostName, pools)
		err = c.f5.ModifyGTMWideIP(hostName, wip)
		if err != nil {
			log.Errorf("Failed to ModifyGTMWideIP %s: %v ", hostName, err)
			return err
		}
	}
	return nil
}
