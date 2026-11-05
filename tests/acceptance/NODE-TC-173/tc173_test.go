// Package nodetc173 verifies that a stopped collection blocks nothing: an
// application still installs and runs, and the device view marks the fact stale
// instead of failing the application (NODE-AC-88, NODE-TC-173).
package nodetc173

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/tsic404/kos-node/internal/agent"
	"github.com/tsic404/kos-node/internal/collector"
	"github.com/tsic404/kosd/api/v1beta1"
	"github.com/tsic404/kosd/pkg/app"
	"github.com/tsic404/kosd/pkg/device"
	"github.com/tsic404/kosd/tests/acceptance/harness"
)

const (
	targetNamespace = "apps"
	applicationName = "collection-free"
)

// TestTC173CollectionFailureDoesNotBlock stops collection, installs an
// application and reads the device view.
func TestTC173CollectionFailureDoesNotBlock(t *testing.T) {
	cluster := harness.Start(t)
	ctx := context.Background()
	start := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	now := start
	clock := func() time.Time { return now }

	node := &collector.Collector{Writer: cluster.Writer(), Node: "node-a", Now: clock}
	if _, err := node.Collect(ctx, collector.Report{Devices: []v1beta1.DeviceFact{{
		Name: "sda", Serial: "DISK-1", Model: "X", Kind: "disk",
		IsPresent: true, IsRecognized: true,
	}}}); err != nil {
		t.Fatalf("write the device fact: %v", err)
	}

	// Collection stops: the round fails and says so.
	loop := &agent.Agent{Collector: node, Source: failingSource{}, Interval: time.Minute}
	if err := loop.Round(ctx); err == nil {
		t.Fatal("a failed collection round was reported as success")
	}

	// An application installs and runs while collection is down.
	cluster.EnsureNamespace(t, targetNamespace)
	cluster.InstallDefinition(t, harness.Definition(t, "collection-free", nil))
	cluster.CreateApplication(t, applicationName, "collection-free", targetNamespace)
	controller := cluster.ApplicationController(t, app.Options{})
	if phase := cluster.SettleApplication(t, controller, applicationName, 6); phase != app.PhaseReady {
		t.Fatalf("phase = %q, want %q", phase, app.PhaseReady)
	}
	status := cluster.ApplicationStatusOf(t, applicationName)
	for _, condition := range status.Conditions {
		if condition.Type == app.ConditionFailed && condition.Status == metav1.ConditionTrue {
			t.Fatalf("an application failed because collection stopped: %+v", condition)
		}
	}
	configMaps := cluster.Dynamic.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace(targetNamespace)
	if _, err := configMaps.Get(ctx, applicationName, metav1.GetOptions{}); err != nil {
		t.Fatalf("the application's rendered object is missing: %v", err)
	}

	// The device view marks the fact stale rather than hiding the gap.
	now = start.Add(device.StaleAfter + time.Hour)
	view := device.BuildView(cluster.List(t, "v1beta1", "NodeDevice"), now)
	if len(view.Warnings) == 0 {
		t.Fatal("the device view does not mark the unrefreshed fact")
	}
	if len(view.Devices) != 1 || !view.Devices[0].IsStale {
		t.Fatalf("the device view does not present the fact as stale: %+v", view.Devices)
	}
}

// failingSource is collection that has stopped.
type failingSource struct{}

// Devices reports the collection failure.
func (failingSource) Devices(context.Context) ([]v1beta1.DeviceFact, error) {
	return nil, errors.New("device tree unavailable")
}
