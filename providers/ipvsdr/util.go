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
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"

	v1 "k8s.io/api/core/v1"
)

var (
	ipv4Version = "4"
	ipv6Version = "6"
)

type stringSlice []string

// pos returns the position of a string in a slice.
// If it does not exists in the slice returns -1.
func (slice stringSlice) pos(value string) int {
	for p, v := range slice {
		if v == value {
			return p
		}
	}

	return -1
}

// getPriority returns the priority of one node using the
// IP address as key. It starts in 100
func getNodePriority(node string, nodes []string) int {
	return 100 + stringSlice(nodes).pos(node)*60
}

func checksum(filename string) (string, error) {
	var result []byte
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	hash := md5.New()
	_, err = io.Copy(hash, file)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(result)), nil
}

func getIPVersion(ip net.IP) string {
	if ip.To4() != nil {
		return ipv4Version
	}
	if ip.To16() != nil {
		return ipv6Version
	}
	return "unknown"
}

func getK8sNodeIP(node *v1.Node) (net.IP, error) {
	var ip net.IP

	addresses := node.Status.Addresses
	addressMap := make(map[v1.NodeAddressType][]v1.NodeAddress)
	for i := range addresses {
		addressMap[addresses[i].Type] = append(addressMap[addresses[i].Type], addresses[i])
	}
	if addresses, ok := addressMap[v1.NodeExternalIP]; ok {
		for _, address := range addresses {
			if ip = net.ParseIP(address.Address); ip != nil {
				return ip, nil
			}
		}
	}
	if addresses, ok := addressMap[v1.NodeInternalIP]; ok {
		for _, address := range addresses {
			if ip = net.ParseIP(address.Address); ip != nil {
				return ip, nil
			}
		}
	}
	return nil, fmt.Errorf("host IP unknown; known addresses: %v", addresses)
}

func getIPFromMap(source map[string]string, key string) net.IP {
	if len(source) == 0 {
		return nil
	}

	v, ok := source[key]
	if !ok {
		return nil
	}
	return net.ParseIP(v)
}

func getK8sNodeMetadataIP(node *v1.Node, key string) net.IP {
	ip := getIPFromMap(node.Labels, key)
	if ip != nil {
		return ip
	}
	return getIPFromMap(node.Annotations, key)
}

func isMaskAllFF(n *net.IPNet) bool {
	for _, c := range n.Mask {
		if c != 0xff {
			return false
		}
	}
	return true
}
