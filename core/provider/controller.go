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

package provider

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/caicloud/clientset/informers"
	lbapi "github.com/caicloud/clientset/pkg/apis/loadbalance/v1alpha2"
	"github.com/caicloud/clientset/util/syncqueue"

	//log "github.com/zoumo/logdog"
	v1 "k8s.io/api/core/v1"
	extv1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	log "k8s.io/klog"
)

// GenericProvider holds the boilerplate code required to build an LoadBalancer Provider.
type GenericProvider struct {
	cfg *Configuration

	ingressClass string

	factory informers.SharedInformerFactory
	listers StoreLister
	queue   *syncqueue.SyncQueue

	// stopLock is used to enforce only a single call to Stop is active.
	// Needed because we allow stopping through an http endpoint and
	// allowing concurrent stoppers leads to stack traces.
	stopLock *sync.Mutex
	stopCh   chan struct{}
	shutdown bool
}

func hasKind(kinds []QueueObjectKind, kind QueueObjectKind) bool {
	for _, a := range kinds {
		if a == kind {
			return true
		}
	}
	return false
}

// NewLoadBalancerProvider returns a configured LoadBalancer controller
func NewLoadBalancerProvider(cfg *Configuration) *GenericProvider {

	kinds := cfg.Backend.WatchKinds()
	gp := &GenericProvider{
		cfg:          cfg,
		factory:      informers.NewSharedInformerFactory(cfg.KubeClient, 0),
		stopLock:     &sync.Mutex{},
		stopCh:       make(chan struct{}),
		ingressClass: fmt.Sprintf("%s.%s", cfg.LoadBalancerNamespace, cfg.LoadBalancerName),
	}

	listers := StoreLister{}

	if hasKind(kinds, QueueObjectLoadbalancer) {
		lbinformer := gp.factory.Loadbalance().V1alpha2().LoadBalancers()
		lbinformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    gp.addLoadBalancer,
			UpdateFunc: gp.updateLoadBalancer,
			DeleteFunc: gp.deleteLoadBalancer,
		})
		listers.LoadBalancer = lbinformer.Lister()
	}

	if hasKind(kinds, QueueObjectConfigmap) {
		cminformer := gp.factory.Core().V1().ConfigMaps()
		cminformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			UpdateFunc: gp.updateConfigMap,
		})
		listers.ConfigMap = cminformer.Lister()
	}

	if hasKind(kinds, QueueObjectNode) {
		// sync nodes
		nodeinformer := gp.factory.Core().V1().Nodes()
		nodeinformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{})
		listers.Node = nodeinformer.Lister()
	}

	if hasKind(kinds, QueueObjecSecret) {
		secretinformer := gp.factory.Core().V1().Secrets()
		secretinformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{})
		listers.Secret = secretinformer.Lister()
	}

	if hasKind(kinds, QueueObjectIngress) {
		ingressInformer := gp.factory.Extensions().V1beta1().Ingresses()
		ingressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    gp.addIngress,
			UpdateFunc: gp.updateIngress,
			DeleteFunc: gp.deleteIngress,
		})
		listers.Ingress = ingressInformer.Lister()
	}

	gp.cfg.Backend.SetListers(listers)

	gp.queue = syncqueue.NewPassthroughSyncQueue(&lbapi.LoadBalancer{}, gp.syncLoadBalancer) //TODO: the first param is useless
	gp.listers = listers

	return gp
}

// Start starts the LoadBalancer Provider.
func (p *GenericProvider) Start() {
	defer utilruntime.HandleCrash()
	log.Info("Startting provider")

	p.factory.Start(p.stopCh)

	// wait cache synced
	log.Info("Wait for all caches synced")
	synced := p.factory.WaitForCacheSync(p.stopCh)
	for tpy, sync := range synced {
		if !sync {
			log.Errorf("Wait for cache sync timeout %v", tpy)
			return
		}
	}
	log.Info("All caches have synced, Running LoadBalancer Controller ...")

	// start backend
	p.cfg.Backend.Start()
	if !p.cfg.Backend.WaitForStart() {
		log.Error("Wait for backend start timeout")
		return
	}

	// start worker
	p.queue.Run(1)

	<-p.stopCh

}

// Stop stops the LoadBalancer Provider.
func (p *GenericProvider) Stop() error {
	log.Info("Shutting down provider")
	p.stopLock.Lock()
	defer p.stopLock.Unlock()
	// Only try draining the workqueue if we haven't already.
	if !p.shutdown {
		p.shutdown = true
		log.Info("close channel")
		close(p.stopCh)
		// stop backend
		log.Info("stop backend")
		_ = p.cfg.Backend.Stop()
		// stop syncing
		log.Info("shutting down controller queue")
		p.queue.ShutDown()
		return nil
	}

	return fmt.Errorf("shutdown already in progress")
}

func (p *GenericProvider) addLoadBalancer(obj interface{}) {
	lb := obj.(*lbapi.LoadBalancer)
	if p.filterLoadBalancer(lb) {
		return
	}
	log.Info("Adding LoadBalancer ")
	p.queue.Enqueue(&QueueObject{
		Event:     QueueObjectEventAdd,
		Kind:      QueueObjectLoadbalancer,
		Namespace: lb.Namespace,
		Name:      lb.Name,
	})
}

func (p *GenericProvider) updateLoadBalancer(oldObj, curObj interface{}) {
	old := oldObj.(*lbapi.LoadBalancer)
	cur := curObj.(*lbapi.LoadBalancer)

	if old.ResourceVersion == cur.ResourceVersion {
		// Periodic resync will send update events for all known LoadBalancer.
		// Two different versions of the same LoadBalancer will always have different RVs.
		return
	}

	if p.filterLoadBalancer(cur) {
		return
	}

	// ignore change of status
	if reflect.DeepEqual(old.Spec, cur.Spec) &&
		reflect.DeepEqual(old.Finalizers, cur.Finalizers) &&
		reflect.DeepEqual(old.DeletionTimestamp, cur.DeletionTimestamp) &&
		reflect.DeepEqual(old.Status.ProxyStatus.TCPConfigMap, cur.Status.ProxyStatus.TCPConfigMap) &&
		reflect.DeepEqual(old.Status.ProxyStatus.UDPConfigMap, cur.Status.ProxyStatus.UDPConfigMap) {
		return
	}

	log.Info("Updating LoadBalancer")

	p.queue.Enqueue(&QueueObject{
		Event:     QueueObjectEventUpdate,
		Kind:      QueueObjectLoadbalancer,
		Namespace: cur.Namespace,
		Name:      cur.Name,
	})
}

func (p *GenericProvider) deleteLoadBalancer(obj interface{}) {
	lb, ok := obj.(*lbapi.LoadBalancer)

	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		lb, ok = tombstone.Obj.(*lbapi.LoadBalancer)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a LoadBalancer %#v", obj))
			return
		}
	}

	if p.filterLoadBalancer(lb) {
		return
	}

	log.Info("Deleting LoadBalancer")

	p.queue.Enqueue(&QueueObject{
		Event:     QueueObjectEventDelete,
		Kind:      QueueObjectLoadbalancer,
		Namespace: lb.Namespace,
		Name:      lb.Name,
		Object:    lb,
	})
}
func (p *GenericProvider) addIngress(obj interface{}) {
	ing := obj.(*extv1.Ingress)
	if p.filterIngress(ing) {
		return
	}
	log.Infof("Adding Ingress %s", ing.Name)
	p.queue.Enqueue(&QueueObject{
		Event:     QueueObjectEventAdd,
		Kind:      QueueObjectIngress,
		Namespace: ing.Namespace,
		Name:      ing.Name,
	})
}
func (p *GenericProvider) updateIngress(o, n interface{}) {
	old := o.(*extv1.Ingress)
	ing := n.(*extv1.Ingress)
	if p.filterIngress(ing) {
		return
	}
	if old.ResourceVersion == ing.ResourceVersion {
		// Periodic resync will send update events for all known LoadBalancer.
		// Two different versions of the same LoadBalancer will always have different RVs.
		return
	}

	log.Infof("Updating Ingress %s", ing.Name)
	p.queue.Enqueue(&QueueObject{
		Event:     QueueObjectEventUpdate,
		Kind:      QueueObjectIngress,
		Namespace: ing.Namespace,
		Name:      ing.Name,
	})
}
func (p *GenericProvider) deleteIngress(obj interface{}) {
	ing, ok := obj.(*extv1.Ingress)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
			return
		}
		ing, ok = tombstone.Obj.(*extv1.Ingress)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("Tombstone contained object that is not a LoadBalancer %#v", obj))
			return
		}
	}

	if p.filterIngress(ing) {
		return
	}
	log.Infof("Deleting Ingress %s", ing.Name)
	p.queue.Enqueue(&QueueObject{
		Event:     QueueObjectEventDelete,
		Kind:      QueueObjectIngress,
		Namespace: ing.Namespace,
		Name:      ing.Name,
		Object:    ing,
	})
}

func (p *GenericProvider) updateConfigMap(oldObj, curObj interface{}) {
	old := oldObj.(*v1.ConfigMap)
	cur := curObj.(*v1.ConfigMap)

	if old.ResourceVersion == cur.ResourceVersion {
		// Periodic resync will send update events for all known LoadBalancer.
		// Two different versions of the same LoadBalancer will always have different RVs.
		return
	}

	// namespace and name can not change, so we check one of them is enough
	if p.filterConfigMap(cur) {
		return
	}

	if reflect.DeepEqual(old.Data, cur.Data) && reflect.DeepEqual(old.Annotations, cur.Annotations) {
		// nothing changed
		return
	}

	p.queue.Enqueue(&QueueObject{
		Event:     QueueObjectEventUpdate,
		Kind:      QueueObjectConfigmap,
		Namespace: cur.Namespace,
		Name:      cur.Name,
	})
}

func (p *GenericProvider) filterLoadBalancer(lb *lbapi.LoadBalancer) bool {
	if lb.Namespace == p.cfg.LoadBalancerNamespace && lb.Name == p.cfg.LoadBalancerName {
		return false
	}

	return true
}

func (p *GenericProvider) filterIngress(ing *extv1.Ingress) bool {
	const key string = "kubernetes.io/ingress.class"
	if ing.Annotations != nil {
		v := ing.Annotations[key]
		if v == p.ingressClass {
			return false
		}
	}
	return true
}

func (p *GenericProvider) filterConfigMap(cm *v1.ConfigMap) bool {
	if cm.Namespace == p.cfg.LoadBalancerNamespace && (cm.Name == p.cfg.TCPConfigMap || cm.Name == p.cfg.UDPConfigMap) {
		return false
	}
	return true
}

func (p *GenericProvider) syncLoadBalancer(obj interface{}) error {
	if p.listers.LoadBalancer == nil {
		return nil
	}

	queueObject := obj.(*QueueObject)
	if queueObject.Kind == QueueObjectIngress {
		return p.syncIngress(queueObject)
	}

	namespace := p.cfg.LoadBalancerNamespace
	name := p.cfg.LoadBalancerName

	lb, err := p.listers.LoadBalancer.LoadBalancers(namespace).Get(name)
	if errors.IsNotFound(err) {
		log.Warningf("LoadBalancer %s has been deleted", name)
		lb, _ = queueObject.Object.(*lbapi.LoadBalancer)
		err = nil
	}

	if err != nil || lb == nil {
		utilruntime.HandleError(fmt.Errorf("Unable to retrieve LoadBalancer %v from store: %v", name, err))
		return err
	}

	if err := lbapi.ValidateLoadBalancer(lb); err != nil {
		log.Infof("invalid loadbalancer scheme: %v", err)
		return err
	}

	// make sure the right ConfigMaps cache
	if p.cfg.TCPConfigMap == "" {
		p.cfg.TCPConfigMap = lb.Status.ProxyStatus.TCPConfigMap
	}
	if p.cfg.UDPConfigMap == "" {
		p.cfg.UDPConfigMap = lb.Status.ProxyStatus.UDPConfigMap
	}

	queueObject.Object = lb

	// workaroud: to simple the code, pass the cm
	var tcpCM, udpCM *v1.ConfigMap
	if p.listers.ConfigMap != nil {
		tcpCM, err = p.listers.ConfigMap.ConfigMaps(lb.Namespace).Get(lb.Status.ProxyStatus.TCPConfigMap)
		if err != nil {
			log.Warningf("Failed to get tcp configmap, error: %v", err)
		}
		udpCM, err = p.listers.ConfigMap.ConfigMaps(lb.Namespace).Get(lb.Status.ProxyStatus.UDPConfigMap)
		if err != nil {
			log.Warningf("Failed to get udp configmap, error: %v", err)
		}
	}
	// we do not use handler for other kind, so consider all other events as if lb update
	if queueObject.Kind != QueueObjectLoadbalancer {
		queueObject.Kind = QueueObjectLoadbalancer
		queueObject.Event = QueueObjectEventUpdate
	}

	log.Infof("OnUpdate %+v ......", queueObject)
	return p.cfg.Backend.OnUpdate(queueObject, lb, tcpCM, udpCM)
}

func (p *GenericProvider) syncIngress(queueObject *QueueObject) error {

	namespace := p.cfg.LoadBalancerNamespace
	name := p.cfg.LoadBalancerName

	lb, err := p.listers.LoadBalancer.LoadBalancers(namespace).Get(name)
	if errors.IsNotFound(err) {
		log.Warningf("LoadBalancer %s has been deleted", name)
		lb, _ = queueObject.Object.(*lbapi.LoadBalancer)
		err = nil
	}

	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Unable to retrieve LoadBalancer %v from store: %v", name, err))
		return err
	}

	ing, err := p.listers.Ingress.Ingresses(queueObject.Namespace).Get(queueObject.Name)
	if errors.IsNotFound(err) {
		log.Warningf("Ingress %s has been deleted", queueObject.Name)
		ing = queueObject.Object.(*extv1.Ingress)
		if ing != nil {
			err = nil
		}
	}
	if err != nil {
		log.Errorf("Failed to get ingress %v", queueObject.Name)
		return err
	}
	queueObject.Object = ing

	log.Infof("OnUpdate %+v ......", queueObject)
	return p.cfg.Backend.OnUpdate(queueObject, lb, nil, nil) // lb may be nil, but we still need to handle ingress deleting
}
