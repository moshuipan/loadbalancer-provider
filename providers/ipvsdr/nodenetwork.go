/*
Copyright 2017 Caicloud authors. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ipvsdr

import (
	"net"
	"sort"

	lbapi "github.com/caicloud/clientset/pkg/apis/loadbalance/v1alpha2"
	log "k8s.io/klog"
)

type nodeNetSelector struct {
	k8sNodeIP string
	ifaces    map[string]bool   // set<iface>
	ips       map[string]string // map[annotation] = ip
}

type allNodeNetSelector map[string]*nodeNetSelector

type allNodeIfaceNetList map[string]*lbapi.NodeStatus

type ifacePreferredNet struct {
	*lbapi.InterfaceNet
	preferredIP string
}
type ifacePreferredNetList []*ifacePreferredNet

func (nns *nodeNetSelector) selectIface(iface *net.Interface, addrs []net.Addr) *lbapi.InterfaceNet {
	_, selected := nns.ifaces[iface.Name]

	ips := []string{}
	for _, addr := range addrs {
		n := addr.(*net.IPNet)
		if n == nil || !n.IP.IsGlobalUnicast() || isMaskAllFF(n) {
			continue
		}
		ip := n.IP.String()
		ips = append(ips, ip)

		selected = selected || ip == nns.k8sNodeIP
		if !selected {
			for _, v := range nns.ips {
				if selected = v == ip; selected {
					break
				}
			}
		}
	}

	if selected {
		sort.Strings(ips)
		i := &lbapi.InterfaceNet{
			Name: iface.Name,
			Mac:  iface.HardwareAddr.String(),
			IPs:  ips,
		}
		return i
	}
	return nil
}

func (p *ifacePreferredNet) getIP(ipVersion string) string {
	if p.preferredIP != "" && getIPVersion(net.ParseIP(p.preferredIP)) == ipVersion {
		return p.preferredIP
	}
	for _, ip := range p.IPs {
		if getIPVersion(net.ParseIP(ip)) == ipVersion {
			return ip
		}
	}
	return ""
}

func getCurrentNodeIfaceIPs(nodeName string, nns *nodeNetSelector) (*lbapi.NodeStatus, error) {
	res := &lbapi.NodeStatus{Name: nodeName}
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Errorf("Failed to list ifaces error: %v", err)
		return res, err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagPointToPoint != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			log.Errorf("Failed to list address on iface %s, error: %v", iface.Name, err)
			continue
		}

		i := nns.selectIface(&iface, addrs)
		if i != nil {
			res.IfaceNetList = append(res.IfaceNetList, i)
		}
	}
	sort.Slice(res.IfaceNetList, func(a, b int) bool {
		return res.IfaceNetList[a].Name < res.IfaceNetList[b].Name
	})

	return res, nil
}

func getAllBinds(p *lbapi.IpvsdrProvider) []*lbapi.KeepalivedBind {
	binds := []*lbapi.KeepalivedBind{}
	if p.Bind != nil {
		binds = append(binds, p.Bind)
	}
	for _, kl := range p.Slaves {
		if kl.Bind != nil {
			binds = append(binds, kl.Bind)
		}
	}
	return binds
}

func getNodeNetwork(nns *nodeNetSelector, ips []*lbapi.InterfaceNet, bind *lbapi.KeepalivedBind) *ifacePreferredNet {
	bindIface := ""
	preferredIP := nns.k8sNodeIP
	if bind != nil {
		bindIface = bind.Iface
		preferredIP = nns.ips[bind.NodeIPAnnotation]
	}

	n := &ifacePreferredNet{}
	if bindIface != "" {
		for _, iface := range ips {
			if bind.Iface == iface.Name {
				n.InterfaceNet = iface
				break
			}
		}
	} else if preferredIP != "" {
		for _, iface := range ips {
			for _, ip := range iface.IPs {
				if ip == preferredIP {
					n.InterfaceNet = iface
					n.preferredIP = ip
					return n
				}
			}
		}
	}

	if n.InterfaceNet != nil {
		for _, ip := range n.InterfaceNet.IPs {
			if ip == preferredIP {
				n.preferredIP = ip
				break
			}
		}
		return n
	}

	return nil
}
