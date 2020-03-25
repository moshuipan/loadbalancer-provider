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

package util

import (
	"os"
	"strconv"

	"github.com/caicloud/clientset/kubernetes"
	gocommonclient "github.com/caicloud/go-common/kubernetes/client"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// NewClientSet create a new clientset
func NewClientSet(kubeconfig string) (kubernetes.Interface, error) {
	// build config
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}

	config = setupConfigQPS(config)
	// create clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return clientset, nil
}

func setupConfigQPS(c *restclient.Config) *restclient.Config {
	const EnvKubeClientQPS = "ENV_KUBE_CLIENT_QPS"
	const EnvKubeClientBurst = "ENV_KUBE_CLIENT_BURST"
	getQPSEnvInt := func(key string, min int) int {
		v := 0
		s := os.Getenv(key)
		if s != "" {
			v, _ = strconv.Atoi(s)
		}
		if v < min {
			v = min
		}
		return v
	}

	qps := getQPSEnvInt(EnvKubeClientQPS, gocommonclient.DefaultQPS)
	burst := getQPSEnvInt(EnvKubeClientBurst, gocommonclient.DefaultBurst)
	c.QPS = float32(qps)
	c.Burst = burst
	return c
}
