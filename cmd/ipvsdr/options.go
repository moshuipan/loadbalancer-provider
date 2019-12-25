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

package main

import (
	"github.com/caicloud/loadbalancer-provider/core/options"
	cli "gopkg.in/urfave/cli.v1"
)

// Options contains controller options
type Options struct {
	*options.Options
	Unicast bool // deprecated
}

// NewOptions reutrns a new Options
func NewOptions() *Options {
	return &Options{
		Options: &options.Options{},
	}
}

// AddFlags add flags to app
func (opts *Options) AddFlags(app *cli.App) {

	opts.Options.AddFlags(app)

	flags := []cli.Flag{
		cli.BoolTFlag{
			Name:        "unicast",
			Usage:       "use unicast instead of multicast for communication with other keepalived instances",
			Destination: &opts.Unicast,
		},
	}

	app.Flags = append(app.Flags, flags...)
}
