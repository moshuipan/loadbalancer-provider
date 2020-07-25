package f5lb

import (
	"encoding/json"
	"fmt"
	"strings"

	log "k8s.io/klog"

	"github.com/caicloud/clientset/kubernetes"
	lbapi "github.com/caicloud/clientset/pkg/apis/loadbalance/v1alpha2"
	"github.com/caicloud/loadbalancer-provider/core/provider"
	core "github.com/caicloud/loadbalancer-provider/core/provider"
	"github.com/caicloud/loadbalancer-provider/pkg/version"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

/*
Ingress Status:
	annotation.specVersion: N // set by WEB
	annotation.statusVersion: M // set by provider
	annotation.statusMessage: Message // set by provider

	if N > M, then Web display "Updating"
	if N == M && Message == "ok", then Web display "Success"
	if N == M && Message != "ok", then Web display "Failed" and Provider re-sync.

LB Status:
lb.Status.providersStatuses.externallb{
	value1,
	value2,
	metadata map[string]string
}

// lb.f5.status + message
// lb.f5.status + message

#### Ingress
```
  "ingress.Metadata.Annotations": {
    "loadbalance.caicloud.io/specVersion": "int"       // updated by admin
    "loadbalance.caicloud.io/dnsInfo": "[]Record{}"    // related dns info
    "loadbalance.caicloud.io/statusVersion": "int"     // update by provider
    "loadbalance.caicloud.io/statusMessage": "string"  // update by provider
  }
```
*/

const (
	lbAnnotationDomain = "loadbalance.caicloud.io/"
	lbDNSDevicesKey    = lbAnnotationDomain + "dns"
	ingressDNSInfoKey  = lbAnnotationDomain + "dnsInfo"
)

// LBClient ...
type LBClient interface {
	SetListers(core.StoreLister)
	DeleteLB(lb *lbapi.LoadBalancer) error
	EnsureLB(lb *lbapi.LoadBalancer) error
	EnsureIngress(lb *lbapi.LoadBalancer, ing *v1beta1.Ingress, ings []*v1beta1.Ingress) error
	DeleteIngress(lb *lbapi.LoadBalancer, ing *v1beta1.Ingress, ings []*v1beta1.Ingress) error
}

// DNSClient ...
type DNSClient interface {
	EnsureIngress(ing *v1beta1.Ingress, dns *provider.Record) error
	DeleteIngress(ing *v1beta1.Ingress, dns *provider.Record) error
}

const (
	phaseUninitiaized = "Uninitiaized"
	phaseRunning      = "Running"
	phaseDeleted      = "Deleted"
)

// Provider ...
type Provider struct {
	storeLister core.StoreLister
	clientset   *kubernetes.Clientset
	client      LBClient

	dnsClients map[string]DNSClient

	loadBalancerNamespace string
	loadBalancerName      string

	phase string

	lb        *lbapi.LoadBalancer
	ingresses map[string]*v1beta1.Ingress
}

func getDevices(clientset *kubernetes.Clientset, lb *lbapi.LoadBalancer) (LBClient, map[string]DNSClient, error) {

	lbnamespace := lb.Namespace
	lbname := lb.Name

	devices := []string{lb.Spec.Providers.F5.Name}
	if s := lb.Annotations[lbDNSDevicesKey]; s != "" {
		devices = append(devices, strings.Split(s, ",")...)
	}

	cm, err := clientset.CoreV1().ConfigMaps("kube-system").Get("loadbalance-devices-cm", metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}
	var f5ltm LBClient
	dnsDevices := make(map[string]DNSClient)

	log.Infof("lb has devices: %s, %v", devices, len(devices))
	for _, d := range devices {
		value, ok := cm.Data[d]
		if !ok {
			return nil, nil, fmt.Errorf("Device %s not found in cm", d)
		}
		var device provider.Device
		err := json.Unmarshal([]byte(value), &device)
		if err != nil {
			return nil, nil, err
		}
		if device.Type == "F5" && device.SubType == "F5-LTM" {
			f5ltm, err = NewF5LTMClient(device, lbnamespace, lbname)
			if err != nil {
				return nil, nil, err
			}
		}

		if device.Type == "F5" && device.SubType == "F5-DNS" {
			c, err := newF5DNSClient(device, lbnamespace, lbname)
			if err != nil {
				return nil, nil, err
			}
			dnsDevices[device.Name] = c
		} else if device.Type == "DNS" {
			c, err := newInfobloxClient(device, lbnamespace, lbname)
			if err != nil {
				return nil, nil, err
			}
			dnsDevices[device.Name] = c
		}
	}
	return f5ltm, dnsDevices, nil
}

// New ...
func New(clientset *kubernetes.Clientset, lb *lbapi.LoadBalancer) (*Provider, error) {

	lbclient, dnsClients, err := getDevices(clientset, lb)
	if err != nil {
		log.Errorf("Failed to get devices %v", err)
		return nil, err
	}

	a := &Provider{
		clientset:             clientset,
		client:                lbclient,
		dnsClients:            dnsClients,
		loadBalancerName:      lb.Name,
		loadBalancerNamespace: lb.Namespace,
		phase:                 phaseUninitiaized,
		lb:                    nil,
		ingresses:             make(map[string]*v1beta1.Ingress),
	}

	if lb.DeletionTimestamp != nil {
		a.phase = phaseDeleted
	}

	return a, nil
}

// WatchKinds ..
func (p *Provider) WatchKinds() []core.QueueObjectKind {
	return []core.QueueObjectKind{core.QueueObjectLoadbalancer, core.QueueObjectConfigmap, core.QueueObjectNode, core.QueueObjectIngress}
}

/*
func (p *Provider) isLBChange(cur *lbapi.LoadBalancer) bool {
	old := p.lb
	if old == nil {
		return true
	}
	if old.ResourceVersion >= cur.ResourceVersion {
		return false
	}

	// ignore change of status
	if reflect.DeepEqual(old.Spec, cur.Spec) &&
		reflect.DeepEqual(old.Finalizers, cur.Finalizers) &&
		reflect.DeepEqual(old.DeletionTimestamp, cur.DeletionTimestamp) {
		return true
	}
	return false
}
*/

func (p *Provider) updatePhase(lb *lbapi.LoadBalancer) error {
	var err error
	if p.phase == phaseUninitiaized {
		if lb.DeletionTimestamp == nil {
			err = p.onUpdateLB(core.QueueObjectEventAdd, lb)
			if err != nil {
				return err
			}
		}
		p.phase = phaseRunning
	}
	if lb == nil || lb.DeletionTimestamp != nil {
		p.phase = phaseDeleted
	}
	return nil
}

// OnUpdate ...
func (p *Provider) OnUpdate(o *core.QueueObject, lb *lbapi.LoadBalancer) error {
	err := p.updatePhase(lb)
	if err != nil {
		log.Errorf("Failed to update phase %s before update: %v", p.phase, err)
		return err
	}

	if o.Kind == core.QueueObjectLoadbalancer {
		err = p.onUpdateLB(o.Event, lb)
		if err != nil {
			return err
		}
		_ = p.onUpdateLBDNS(o)
	}

	if o.Kind == core.QueueObjectIngress {
		err = p.onUpdateIngress(o, lb)
		if err != nil {
			return err
		}
		err = p.onUpdateIngressDNS(o)
		if err != nil {
			return err
		}
	}

	return nil
}

// Start ...
func (p *Provider) Start() {
	log.Infof("Startting provider ns %s name %s", p.loadBalancerNamespace, p.loadBalancerName)
}

// Stop ...
func (p *Provider) Stop() error {
	log.Infof("end provider ...")
	return nil
}

// Info ...
func (p *Provider) Info() core.Info {
	info := version.Get()
	return core.Info{
		Name:      "externallb",
		Version:   info.Version,
		GitCommit: info.GitCommit,
		GitRemote: info.GitRemote,
	}
}

// WaitForStart waits for ipvsdr fully run
func (p *Provider) WaitForStart() bool {
	return true
}

// SetListers sets the configured store listers in the generic ingress controller
func (p *Provider) SetListers(lister core.StoreLister) {
	p.storeLister = lister
	p.client.SetListers(lister)
}

func (p *Provider) onUpdateLB(e core.QueueObjectEvent, lb *lbapi.LoadBalancer) error {
	var err error
	if e == core.QueueObjectEventDelete || p.phase == phaseDeleted {
		err = p.client.DeleteLB(lb)
	} else {
		err = p.client.EnsureLB(lb)
	}
	return err
}

func (p *Provider) onUpdateIngress(o *core.QueueObject, lb *lbapi.LoadBalancer) error {

	selector := labels.Set{lbapi.LabelKeyCreatedBy: fmt.Sprintf("%s.%s", p.loadBalancerNamespace, p.loadBalancerName)}.AsSelector()
	ings, err := p.storeLister.Ingress.List(selector)
	if err != nil {
		log.Errorf("Failed to list ingress")
		return err
	}

	ing := o.Object.(*v1beta1.Ingress)
	if o.Event == core.QueueObjectEventDelete || p.phase == phaseDeleted {
		err = p.client.DeleteIngress(lb, ing, ings)
	} else {
		err = p.client.EnsureIngress(lb, ing, ings)
	}
	return err
}

// onUpdateLBDNS to delete all dns record by lb
func (p *Provider) onUpdateLBDNS(o *core.QueueObject) error {
	if o.Kind != core.QueueObjectLoadbalancer {
		return nil
	}

	if p.phase != phaseDeleted {
		return nil
	}

	selector := labels.Set{lbapi.LabelKeyCreatedBy: fmt.Sprintf("%s.%s", p.loadBalancerNamespace, p.loadBalancerName)}.AsSelector()
	ings, err := p.storeLister.Ingress.List(selector)
	if err != nil {
		log.Errorf("Failed to list ingress")
		return err
	}

	for _, ing := range ings {
		_ = p.updateOneIngressDNS(ing, false)
	}
	return nil
}

func (p *Provider) onUpdateIngressDNS(o *core.QueueObject) error {
	var err error
	//ip, user, password, prefix, vsnames := getF5GTMClientInfo()

	ing := o.Object.(*v1beta1.Ingress)

	if o.Event == core.QueueObjectEventDelete || p.phase == phaseDeleted {
		err = p.updateOneIngressDNS(ing, false)
	} else {
		err = p.updateOneIngressDNS(ing, true)
	}
	return err
}

func (p *Provider) updateOneIngressDNS(ing *v1beta1.Ingress, add bool) error {
	s := ing.Annotations[ingressDNSInfoKey]

	var dnsInfo []provider.Record
	if err := json.Unmarshal([]byte(s), &dnsInfo); err != nil {
		return err
	}

	var err error
	for _, dns := range dnsInfo {
		client, ok := p.dnsClients[dns.DNSName]
		if !ok {
			log.Errorf("dns %s not found for ingress %s", dns.DNSName, ing.Name)
			continue
		}

		if add {
			err = client.EnsureIngress(ing, &dns)
		} else {
			err = client.DeleteIngress(ing, &dns)
		}
		if err != nil {
			log.Errorf("Failed to update Ingress %s:%v", ing.Name, err)
		}
	}
	return nil
}
