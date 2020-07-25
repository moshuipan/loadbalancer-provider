package f5lb

import (
	"fmt"
	"strings"

	gobigip "github.com/hanxueluo/go-bigip"

	"github.com/caicloud/loadbalancer-provider/core/provider"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	log "k8s.io/klog"
)

type f5DNSClient struct {
	f5 *gobigip.BigIP
	//virtualServers [][]string
	poolPrefix string
	lbname     string
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
	f5, err := gobigip.NewTokenSession(d.ManageAddr, d.Auth.User, d.Auth.Password, lbname, nil)
	if err != nil {
		return nil, err
	}

	log.Infof("Trying f5gtm list pool API call:%s, user:%s", d.ManageAddr, d.Auth.User)
	_, err = f5.GetGTMCNamePools()
	if err != nil {
		return nil, err
	}

	lbclient := &f5DNSClient{
		f5:         f5,
		poolPrefix: d.Config.PoolPrefix,
		lbname:     fmt.Sprintf("%s.%s", lbname, lbnamespace),
	}

	return lbclient, nil
}

func (c *f5DNSClient) EnsureIngress(ing *v1beta1.Ingress, dns *provider.Record) error {
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
		_ = c.ensureHost(hostName, virtualServers)
	}

	return nil
}

func (c *f5DNSClient) DeleteIngress(ing *v1beta1.Ingress, dns *provider.Record) error {
	hostName := ""
	for _, r := range ing.Spec.Rules {
		if dns.Zone != "" && strings.HasSuffix(r.Host, dns.Zone) {
			hostName = r.Host
		} else {
			log.Warningf("skip ingress host: %s, zone: %s", r.Host, dns.Zone)
		}
	}
	if hostName != "" {
		c.deleteHost(hostName)
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

func (c *f5DNSClient) deleteHost(hostName string) {
	log.Infof("f5.DeleteGTMWideIP %s", hostName)
	err := c.f5.DeleteGTMWideIP(hostName)
	if err != nil {
		log.Warningf("Failed to DeleteGTMWideIP %s: %v ", hostName, err)
	}

	poolName := c.getPoolName(hostName)
	log.Infof("f5.DeleteGTMPool %s", poolName)
	err = c.f5.DeleteGTMPool(poolName)
	if err != nil {
		log.Warningf("Failed to DeleteGTMPool %s: %v ", poolName, err)
	}
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
