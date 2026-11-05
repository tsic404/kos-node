// Command kos-node is the per-node agent. It collects this host's device facts
// and writes the NodeDevice whose name is the hostname; one instance runs per
// node and it writes no other object (NODE-C-37).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/tsic404/kos-node/internal/agent"
	"github.com/tsic404/kos-node/internal/collector"
	"github.com/tsic404/kosd/api"
	"github.com/tsic404/kosd/pkg/write"
)

func main() {
	if err := run(); err != nil {
		slog.Error("kos-node stopped", "error", err)
		os.Exit(1)
	}
}

// run parses the flags and collects until the process is asked to stop.
func run() error {
	var (
		kubeconfig string
		interval   time.Duration
		once       bool
		root       string
	)
	flag.StringVar(&kubeconfig, "kubeconfig", "", "path to the kubeconfig; empty uses the in-cluster configuration")
	flag.DurationVar(&interval, "interval", agent.DefaultInterval, "pause between collection rounds")
	flag.BoolVar(&once, "once", false, "run one collection round and exit")
	flag.StringVar(&root, "sysfs", "", "sysfs mount point; empty uses /sys")
	flag.Parse()

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("read hostname: %w", err)
	}
	if hostname == "" {
		return errors.New("the host has no name")
	}
	config, err := clusterConfig(kubeconfig)
	if err != nil {
		return err
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("build kubernetes client: %w", err)
	}

	node := &collector.Collector{
		Writer: &write.Writer{Dynamic: dynamicClient, Resources: deviceResources()},
		Node:   hostname,
	}
	loop := &agent.Agent{
		Collector: node,
		Source:    collector.SysfsSource{Root: root},
		Interval:  interval,
		Logger:    slog.Default(),
	}
	if once {
		return loop.Round(context.Background())
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	slog.Info("kos-node started", "node", hostname, "interval", interval)
	return loop.Run(ctx)
}

// clusterConfig builds the client configuration: the given kubeconfig, or the
// service account the node runs under.
func clusterConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("read kubeconfig %s: %w", kubeconfig, err)
		}
		return config, nil
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("read the in-cluster configuration: %w", err)
	}
	return config, nil
}

// deviceResources resolves the one class this agent writes, so the write path
// reaches the resource the object model declares.
func deviceResources() map[schema.GroupKind]string {
	name, ok := api.ResourceName("NodeDevice")
	if !ok {
		return nil
	}
	return map[schema.GroupKind]string{{Group: api.Group, Kind: "NodeDevice"}: name}
}
