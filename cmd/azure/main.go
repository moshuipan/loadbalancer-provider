package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	cli "gopkg.in/urfave/cli.v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	log "k8s.io/klog"

	core "github.com/caicloud/loadbalancer-provider/core/provider"
	coreutil "github.com/caicloud/loadbalancer-provider/core/util"
	"github.com/caicloud/loadbalancer-provider/pkg/version"
	"github.com/caicloud/loadbalancer-provider/providers/azure"
)

// Run ...
func Run(opts *Options) error {
	info := version.Get()
	log.Infof("Provider Build Information %v", info.Pretty())

	log.Info("Provider Running with",
		"debug:", opts.Debug,
		"kubconfig:", opts.Kubeconfig,
		"lb.ns:", opts.LoadBalancerNamespace,
		"lb.name:", opts.LoadBalancerName,
		"pod.name:", opts.PodName,
		"pod.ns:", opts.PodNamespace,
	)

	log.Infof("load kubeconfig from %s", opts.Kubeconfig)
	clientset, err := coreutil.NewClientSet(opts.Kubeconfig)
	if err != nil {
		log.Fatal("Create clientset error", err)
		return err
	}

	lb, err := clientset.Custom().LoadbalanceV1alpha2().LoadBalancers(opts.LoadBalancerNamespace).Get(opts.LoadBalancerName, metav1.GetOptions{})
	if err != nil {
		log.Fatal("Can not find loadbalancer resource", "lb.ns:", opts.LoadBalancerNamespace, "lb.name:", opts.LoadBalancerName)
		return err
	}

	azure, err := azure.New(clientset, opts.LoadBalancerName, opts.LoadBalancerNamespace)
	if err != nil {
		return err
	}

	lp := core.NewLoadBalancerProvider(&core.Configuration{
		KubeClient:            clientset,
		Backend:               azure,
		LoadBalancerName:      opts.LoadBalancerName,
		LoadBalancerNamespace: opts.LoadBalancerNamespace,
		TCPConfigMap:          lb.Status.ProxyStatus.TCPConfigMap,
		UDPConfigMap:          lb.Status.ProxyStatus.UDPConfigMap,
	})

	// handle shutdown
	go handleSigterm(lp)

	lp.Start()

	// never stop until sigterm processed
	<-wait.NeverStop

	return nil
}

func main() {
	_ = flag.CommandLine.Parse([]string{})

	app := cli.NewApp()
	app.Name = "azure provider"
	app.Compiled = time.Now()
	app.Version = version.Get().Version
	// add flags to app
	opts := NewOptions()
	opts.AddFlags(app)

	app.Action = func(c *cli.Context) error {
		if err := Run(opts); err != nil {
			msg := fmt.Sprintf("running loadbalancer controller failed, with err: %v\n", err)
			return cli.NewExitError(msg, 1)
		}
		return nil
	}

	sort.Sort(cli.FlagsByName(app.Flags))

	_ = app.Run(os.Args)
}

func handleSigterm(p *core.GenericProvider) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM)
	<-signalChan
	log.Infof("Received SIGTERM, shutting down")

	exitCode := 0
	if err := p.Stop(); err != nil {
		log.Infof("Error during shutdown %v", err)
		exitCode = 1
	}

	log.Infof("Exiting with %v", exitCode)
	os.Exit(exitCode)
}
