// Package nodetc172 verifies that the four device layers are reported
// independently: a layer that does not hold changes no other layer, and the
// device view shows each layer apart (NODE-AC-84, NODE-TC-172).
package nodetc172

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/tsic404/kos-node/internal/collector"
	"github.com/tsic404/kosd/api/v1beta1"
	"github.com/tsic404/kosd/pkg/device"
	"github.com/tsic404/kosd/tests/acceptance/harness"
)

// TestTC172LayersAreIndependent writes three devices whose layers differ.
func TestTC172LayersAreIndependent(t *testing.T) {
	cluster := harness.Start(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := (&collector.Collector{
		Writer: cluster.Writer(), Node: "node-a", Now: func() time.Time { return now },
	}).Collect(ctx, collector.Report{Devices: []v1beta1.DeviceFact{
		// Present and recognised, driver not ready, not published.
		{
			Name: "gpu0", Serial: "GPU-1", Model: "A100", Kind: "gpu",
			IsPresent: true, IsRecognized: true,
		},
		// Every layer holds except publication.
		{
			Name: "gpu1", Serial: "GPU-2", Model: "A100", Kind: "gpu",
			IsPresent: true, IsRecognized: true, IsDriverReady: true,
		},
		// Every layer holds.
		{
			Name: "gpu2", Serial: "GPU-3", Model: "A100", Kind: "gpu",
			IsPresent: true, IsRecognized: true, IsDriverReady: true, IsPublished: true,
		},
	}}); err != nil {
		t.Fatalf("write the device facts: %v", err)
	}

	stored := cluster.Get(t, "v1beta1", "NodeDevice", "node-a")
	entries, found, err := unstructured.NestedSlice(stored.Object, "spec", "devices")
	if err != nil || !found {
		t.Fatalf("read the stored entries: found=%v err=%v", found, err)
	}
	want := map[string][4]bool{
		"gpu0": {true, true, false, false},
		"gpu1": {true, true, true, false},
		"gpu2": {true, true, true, true},
	}
	if len(entries) != len(want) {
		t.Fatalf("the object keeps %d entries, want %d: %v", len(entries), len(want), entries)
	}
	for _, raw := range entries {
		entry, _ := raw.(map[string]any)
		name, _ := entry["name"].(string)
		expected, known := want[name]
		if !known {
			t.Fatalf("unexpected device entry %q: %v", name, entry)
		}
		got := [4]bool{
			layerField(entry, "present"),
			layerField(entry, "recognized"),
			layerField(entry, "driverReady"),
			layerField(entry, "published"),
		}
		if got != expected {
			t.Fatalf("%s layers = %v, want %v", name, got, expected)
		}
	}

	view := device.BuildView(cluster.List(t, "v1beta1", "NodeDevice"), now)
	if len(view.Devices) != len(want) {
		t.Fatalf("the device view shows %d devices, want %d", len(view.Devices), len(want))
	}
	for _, presented := range view.Devices {
		expected := want[presented.Name]
		got := [4]bool{presented.IsPresent, presented.IsRecognized, presented.IsDriverReady, presented.IsPublished}
		if got != expected {
			t.Fatalf("the view presents %s layers as %v, want %v", presented.Name, got, expected)
		}
	}
	// Only the device whose four layers all hold is presented as free.
	allocatable := view.Allocatable()
	if len(allocatable) != 1 || allocatable[0].Name != "gpu2" {
		t.Fatalf("scheduling derivation = %+v, want only gpu2", allocatable)
	}
}

// layerField reads one stored layer. A layer that does not hold is left out of
// the wire form, so a missing field reads as false rather than as unreported
// (NODE-C-70).
func layerField(entry map[string]any, field string) bool {
	value, _ := entry[field].(bool)
	return value
}
