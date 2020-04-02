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
	"fmt"
	"os"
	"syscall"
	"text/template"
	"time"

	"github.com/caicloud/loadbalancer-provider/pkg/execd"
	log "k8s.io/klog"
)

const (
	iptablesChain  = "LOADBALANCER-IPVS-DR"
	keepalivedCfg  = "/etc/keepalived/keepalived.conf"
	keepalivedTmpl = "/root/keepalived.tmpl"

	acceptMark = 1
	dropMark   = 0
	mask       = "0x00000001"
)

type vrrpInstance struct {
	Name      string
	State     string
	Vrid      int
	Priority  int
	Interface string
	MyIP      string
	AllIPs    []string
	VIP       string
}

type virtualServer struct {
	AcceptMark int
	VIP        string
	Scheduler  string
	RealServer []string
}

type keepalived struct {
	cmd  *execd.D
	tmpl *template.Template
}

func (k *keepalived) UpdateConfig(vis []*vrrpInstance, vss []*virtualServer, httpPort int) error {
	w, err := os.Create(keepalivedCfg)
	if err != nil {
		return err
	}
	defer func() { _ = w.Close() }()
	log.Infof("Updating keealived config")
	// save vips for release when shutting down

	conf := make(map[string]interface{})
	conf["iptablesChain"] = iptablesChain
	conf["instances"] = vis
	conf["vss"] = vss
	conf["httpPort"] = httpPort

	return k.tmpl.Execute(w, conf)
}

// Start starts a keepalived process in foreground.
// In case of any error it will terminate the execution with a fatal error
func (k *keepalived) Start() {
	go k.run()
}

func (k *keepalived) isRunning() bool {
	return k.cmd.IsRunning()
}

func (k *keepalived) run() {
	k.cmd = execd.Daemon("keepalived",
		"--dont-fork",
		"--log-console",
		"--release-vips",
		"--pid", "/keepalived.pid")
	// put keepalived in another process group to prevent it
	// to receive signals meant for the controller
	// k.cmd.SysProcAttr = &syscall.SysProcAttr{
	// 	Setpgid: true,
	// 	Pgid:    0,
	// }
	k.cmd.Stdout = os.Stdout
	k.cmd.Stderr = os.Stderr

	k.cmd.SetGracePeriod(1 * time.Second)

	if err := k.cmd.RunForever(); err != nil {
		panic(fmt.Sprintf("can not run keepalived, %v", err))
	}
}

// Reload sends SIGHUP to keepalived to reload the configuration.
func (k *keepalived) Reload() error {
	log.Info("reloading keepalived")
	err := k.cmd.Signal(syscall.SIGHUP)
	if err == execd.ErrNotRunning {
		log.Warning("keepalived is not running, skip the reload")
		return nil
	}
	if err != nil {
		return fmt.Errorf("error reloading keepalived: %v", err)
	}

	return nil
}

// Stop stop keepalived process
func (k *keepalived) Stop() {
	log.Info("stop keepalived process")
	err := k.cmd.Stop()
	if err != nil {
		log.Errorf("error stopping keepalived: %v", err)
	}
}

func (k *keepalived) loadTemplate() error {
	tmpl, err := template.ParseFiles(keepalivedTmpl)
	if err != nil {
		return err
	}
	k.tmpl = tmpl
	return nil
}
