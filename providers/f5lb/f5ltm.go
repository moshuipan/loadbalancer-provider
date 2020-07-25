package f5lb

import (
	"fmt"
	"sort"
	"strings"

	gobigip "github.com/hanxueluo/go-bigip"

	lbapi "github.com/caicloud/clientset/pkg/apis/loadbalance/v1alpha2"
	"github.com/caicloud/loadbalancer-provider/core/provider"
	core "github.com/caicloud/loadbalancer-provider/core/provider"
	coreutil "github.com/caicloud/loadbalancer-provider/core/util"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	log "k8s.io/klog"
)

/*

lb: kube-system/lb-198807712
F5 resource name:
	virtualServer: $preset
	irule: $preset
	node: ${ip}
	pool: P_Pool_kube-system_lb-198807712_pool

irule:
when HTTP_REQUEST {
  switch [HTTP::host] {
    "yunxiao-test.infinitus.com.cn" { pool T_Pool_yunxiao-test_80_6 }
    "ali-aegis-test.infinitus.com.cn" { pool T_Pool_yunxiao-test_80_6 }
    "paas-qjy-test.infinitus.com.cn" { pool T_Pool_qjy-test_pub_8080_6 }
    "sba-qjy-test.infinitus.com.cn" { pool T_Pool_qjy-test_pub_8080_6 }
    "config-qjy-test.infinitus.com.cn" { pool T_Pool_qjy-test_pub_8080_6 }
    "fxh-qjypoc-test.infinitus.com.cn" { pool T_Pool_fxh-qjypoc-test_7081_6 }
    "fxh-mobile-qjypoc-test.infinitus.com.cn" { pool T_Pool_fxh-mobile-qjypoc-test_7084_6 }
    "scc-test.infinitus.com.cn" { pool T_Pool_scc_pub_80_6 }
    "acs-test.infinitus.com.cn" { pool T_Pool_acs-test_8080_6 }
	}
}
*/

const defaultHTTPPort = "80"

type f5LTMClient struct {
	f5CommonClient
	storeLister core.StoreLister
	namePrefix  string

	virtualServer string
	irule         string
}

// NewF5LTMClient ...
func NewF5LTMClient(d provider.Device, lbnamespace, lbname string) (LBClient, error) {
	vsName := strings.Split(d.Config.VirtualServerList, ",")[0]
	iruleName := d.Config.IRule

	lbclient := &f5LTMClient{
		namePrefix:    fmt.Sprintf("%s_%s_", lbnamespace, lbname),
		virtualServer: vsName,
		irule:         iruleName,
	}
	lbclient.d = d
	err := refreshToken(&lbclient.f5CommonClient)
	if err != nil {
		return nil, err
	}
	log.Infof("Trying f5ltm API call:%s, user:%s, vs:%s, irule: %s", d.ManageAddr, d.Auth.User, vsName, iruleName)
	vs, err := lbclient.f5.GetVirtualAddress(vsName)
	if err != nil {
		return nil, err
	}
	irule, err := lbclient.f5.IRule(iruleName)
	if err != nil {
		return nil, err
	}
	if irule == nil || vs == nil {
		return nil, fmt.Errorf("resource not found: vs:%v,irule:%v", vs, irule)
	}

	return lbclient, nil

}

func (c *f5LTMClient) SetListers(lister core.StoreLister) {
	c.storeLister = lister
}

// DeleteLB...
func (c *f5LTMClient) DeleteLB(lb *lbapi.LoadBalancer) error {
	err := refreshToken(&c.f5CommonClient)
	if err != nil {
		return err
	}
	iruleName := c.getIRuleName()

	obj, err := c.f5.IRule(iruleName)
	if err != nil {
		log.Warningf("Failed to get irule %s when clean: %v", iruleName, err)
	}

	if obj != nil {
		content := "# " + c.namePrefix
		if obj.Rule != content {
			obj.Rule = content
			log.Infof("f5.ModifyIRule %s:\n%v", iruleName, content)
			if err := c.f5.ModifyIRule(obj.Name, obj); err != nil {
				log.Warningf("Failed to clean irule %s:%v", iruleName, err)
			}
		}
	}

	poolName := c.getPoolName()
	log.Infof("f5.DeletePool %s", poolName)
	err = c.f5.DeletePool(poolName)
	if err != nil {
		log.Warningf("Failed to delete pool %s", poolName)
	}

	/* //do not clean node
	f5nodes, err := c.f5.Nodes()
	if err != nil {
		log.Warningf("Failed to list F5 nodes %v", err)
	}

	if f5nodes != nil {
		for _, f5node := range f5nodes.Nodes {
			if c.isMyResource(f5node.Name) {
				log.Infof("f5.DeleteNode fails %v", err)
				err = c.f5.DeleteNode(f5node.Name)
				log.Errorf("Failed to list F5 nodes %v", err)
			}
		}
	}
	*/

	return nil
}

// EnsureLB...
func (c *f5LTMClient) EnsureLB(lb *lbapi.LoadBalancer) error {
	err := refreshToken(&c.f5CommonClient)
	if err != nil {
		return err
	}
	_, err = c.f5.GetVirtualServer(c.virtualServer)
	if err != nil {
		return err
	}

	poolName := c.getPoolName()
	if err := c.ensureNodeAndPool(poolName, lb); err != nil {
		return err
	}

	return nil
}

// DeleteIngresse...
func (c *f5LTMClient) DeleteIngress(lb *lbapi.LoadBalancer, ing *v1beta1.Ingress, ings []*v1beta1.Ingress) error {
	err := refreshToken(&c.f5CommonClient)
	if err != nil {
		return err
	}
	return c.EnsureIngress(lb, ing, ings)
}

// EnsureIngress...
func (c *f5LTMClient) EnsureIngress(lb *lbapi.LoadBalancer, ing *v1beta1.Ingress, ings []*v1beta1.Ingress) error {
	err := refreshToken(&c.f5CommonClient)
	if err != nil {
		return err
	}

	domainsMap := make(map[string]bool)

	for _, ing := range ings {
		if ing == nil || ing.DeletionTimestamp != nil {
			continue
		}
		for _, rule := range ing.Spec.Rules {
			domainsMap[rule.Host] = true
		}
	}
	domains := make([]string, 0, len(domainsMap))
	for h := range domainsMap {
		domains = append(domains, h)
	}
	sort.Strings(domains)

	/*
		ltm rule /Common/ruleA {
			when HTTP_REQUEST {
			  if { [HTTP::host] equals "hnet23.com" } {
			    pool nodepool23
			  } elseif { [HTTP::host] equals "hnet45.com" } {
			    pool nodepool23
			  }
		    }
		}
	*/
	// update irule content
	poolName := c.getPoolName()
	newRule := "# written by lb " + c.namePrefix
	if len(domains) > 0 {
		newRule += "\nwhen HTTP_REQUEST {\n  switch [HTTP::host] {\n"
		for _, domain := range domains {
			newRule += fmt.Sprintf("    \"%s\" { pool %s }\n", domain, poolName)
		}
		newRule += "  }\n}"
	}

	/* // if-else
	for i, domain := range domains {
		prefix := "  } elseif"
		if i == 0 {
			prefix = "  if"
		}
		newRule += prefix + " { [HTTP::host] equals \"" + domain + "\" } {\n"
		newRule += "    pool " + poolName + "\n"
	}
	if len(domains) > 0 {
		newRule += "  }\n"
	}
	newRule += "}"
	*/

	// get irule
	iruleName := c.getIRuleName()
	obj, err := c.f5.IRule(iruleName)
	if obj == nil && err == nil {
		err = fmt.Errorf("iRule %s doesn't exist on vs %s", iruleName, c.virtualServer)
	}

	if err != nil {
		log.Errorf("Failed to get irule %s: %v", iruleName, err)
		return err
	}

	if obj.Rule != newRule {
		obj.Rule = newRule
		log.Infof("f5.ModifyIRule %s:\n%v", iruleName, newRule)
		if err := c.f5.ModifyIRule(obj.Name, obj); err != nil {
			log.Errorf("Failed to modify irule %s:%v", iruleName, err)
			return err
		}
	}

	return nil
}

func (c *f5LTMClient) getIRuleName() string {
	return c.irule
}

func (c *f5LTMClient) getPoolName() string {
	return "P_Pool_" + c.namePrefix + "pool"
}

func (c *f5LTMClient) ensurePool(name string) error {
	obj, err := c.f5.GetPool(name)

	if err != nil {
		log.Errorf("Failed to get %s: %v", name, err)
		return err
	}

	if obj == nil {
		config := &gobigip.Pool{
			Name:    name,
			Monitor: "/Common/http ", // f5 default http monitor
		}
		log.Infof("f5.AddPool %v", config)
		err = c.f5.AddPool(config)
		if err != nil {
			log.Errorf("Failed to create %s: %v", name, err)
			return err
		}
	}
	return nil
}

func (c *f5LTMClient) ensureNodeAndPool(poolName string, lb *lbapi.LoadBalancer) error {
	ips := make(map[string]bool)

	for _, n := range lb.Spec.Nodes.Names {
		ip := coreutil.GetNodeIP(c.storeLister.Node, n)
		if ip == "" {
			log.Warningf("Failed to get ip for node %s", n)
			continue
		}
		ips[ip] = false
	}

	f5nodes, err := c.f5.Nodes()
	if err != nil {
		log.Errorf("Failed to list F5 nodes %v", err)
		return err
	}

	for _, f5node := range f5nodes.Nodes {
		if f5node.Name != f5node.Address {
			log.Warningf("node name %s != addr %s in f5", f5node.Name, f5node.Address)
			continue
		}
		if _, ok := ips[f5node.Name]; ok {
			ips[f5node.Name] = true
		}
	}

	for ip, exist := range ips {
		if exist {
			continue
		}
		log.Infof("f5.CreateNode %v:%v", ip, exist)
		err := c.f5.CreateNode(ip, ip)
		if err != nil {
			log.Errorf("Failed to create F5 node %s, %v", ip, err)
			return err
		}
	}
	err = c.ensurePool(poolName)
	if err != nil {
		return err
	}

	poolMembers, err := c.f5.PoolMembers(poolName)
	if err != nil {
		log.Errorf("Failed to poolMember for pool %s: %v", poolName, err)
		return err
	}

	foundCount := 0
	newPoolMembers := []gobigip.PoolMember{}
	for ip := range ips {
		memberName := ip + ":" + defaultHTTPPort
		var pm *gobigip.PoolMember

		for i := range poolMembers.PoolMembers {
			if poolMembers.PoolMembers[i].Name == memberName {
				foundCount++
				break
			}
		}
		pm = &gobigip.PoolMember{
			Name: memberName,
		}
		newPoolMembers = append(newPoolMembers, *pm)
	}

	if foundCount != len(newPoolMembers) {
		log.Infof("f5.UpdatePoolMembers ips: %v, found: %v, newPool: %v", ips, foundCount, newPoolMembers)
		err = c.f5.UpdatePoolMembers(poolName, &newPoolMembers)
		if err != nil {
			log.Errorf("Failed to update pool member %s: %v", poolName, err)
			return err
		}
	}

	/* // not clean node
	for k := range removed {
		log.Infof("f5.DeleteNode %v", k)
		err := c.f5.DeleteNode(k)
		if err != nil {
			log.Warningf("ignore failure of deleting node %s from f5", k)
		}
	}
	*/

	return nil
}
