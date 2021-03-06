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

package options

import cli "gopkg.in/urfave/cli.v1"

// Options contains common controller options
type Options struct {
	Debug                 bool
	Kubeconfig            string
	LoadBalancerNamespace string
	LoadBalancerName      string
	PodNamespace          string
	PodName               string
	NodeIPLabel           string
	NodeIPAnnotation      string
}

// AddFlags add flags to app
func (opts *Options) AddFlags(app *cli.App) {

	flags := []cli.Flag{
		cli.StringFlag{
			Name:        "kubeconfig",
			Usage:       "Path to a kube config. Only required if out-of-cluster.",
			Destination: &opts.Kubeconfig,
		},
		cli.BoolFlag{
			Name:        "debug",
			Usage:       "run with debug mode",
			Destination: &opts.Debug,
		},
		cli.StringFlag{
			Name:        "loadbalancer-namespace",
			EnvVar:      "LOADBALANCER_NAMESPACE",
			Usage:       "specify loadbalancer resource namespace",
			Destination: &opts.LoadBalancerNamespace,
		},
		cli.StringFlag{
			Name:        "loadbalancer-name",
			EnvVar:      "LOADBALANCER_NAME",
			Usage:       "specify loadbalancer resource name",
			Destination: &opts.LoadBalancerName,
		},
		cli.StringFlag{
			Name:        "pod-namespace",
			EnvVar:      "POD_NAMESPACE",
			Usage:       "specify pod namespace",
			Destination: &opts.PodNamespace,
		},
		cli.StringFlag{
			Name:        "pod-name",
			EnvVar:      "POD_NAME",
			Usage:       "specify pod name",
			Destination: &opts.PodName,
		},
		cli.StringFlag{
			Name:        "nodeip-label",
			EnvVar:      "NODEIP_LABEL",
			Usage:       "tell provider which label of node stores node ip",
			Destination: &opts.NodeIPLabel,
		},
		cli.StringFlag{
			Name:        "nodeip-annotation",
			EnvVar:      "NODEIP_ANNOTATION",
			Usage:       "tell provider which annotation of node stores node ip",
			Destination: &opts.NodeIPAnnotation,
		},
	}

	app.Flags = append(app.Flags, flags...)
}
