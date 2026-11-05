// Package collector writes the device fact object of one node: it reads the
// facts of this round, keeps the newest entry per serial number and applies the
// object whose name is the collector's hostname (NODE-C-37, NODE-C-70).
//
// A round that addresses any other object is refused, and the object it
// addressed is marked invalid through the control plane's validator, so the
// boundary rule has one implementation (KOSD-C-25).
package collector

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/tsic404/kosd/api"
	"github.com/tsic404/kosd/api/v1beta1"
	"github.com/tsic404/kosd/pkg/device"
	"github.com/tsic404/kosd/pkg/markers"
	"github.com/tsic404/kosd/pkg/write"
)

// Source supplies the device facts of one collection round.
type Source interface {
	// Devices returns what the host reports for this round.
	Devices(ctx context.Context) ([]v1beta1.DeviceFact, error)
}

// Collector writes the device fact object of one node. Node is the
// collector's hostname: the object it writes carries that name and must name
// that node in its node field. Identity is recorded in the object and defaults
// to Node.
type Collector struct {
	// Writer executes the write through the control plane's write path.
	Writer *write.Writer
	// Node is the collector's hostname and the name of the object it owns.
	Node string
	// Identity is the collection identity recorded in the object.
	Identity string
	// Now supplies the observation time; nil means the wall clock.
	Now func() time.Time
}

// Report is one collection round: the target object, the node the object names
// and the device entries observed. A legal round has Object and Node equal to
// the collector's hostname.
type Report struct {
	// Object is the name of the NodeDevice the round writes.
	Object string
	// Node is the value of the fact's node field.
	Node string
	// Devices are the hardware facts observed in this round.
	Devices []v1beta1.DeviceFact
}

// OutOfBounds reports a write outside the collector's own hostname (KOSD-C-25).
type OutOfBounds struct {
	// Collector is the hostname the attempt was made under.
	Collector string
	// Object is the object the attempt addressed.
	Object string
	// Node is the node field the attempt carried.
	Node string
}

// Error names both sides of the boundary violation.
func (e *OutOfBounds) Error() string {
	return fmt.Sprintf("collector %q cannot write device fact %q for node %q", e.Collector, e.Object, e.Node)
}

// Collect writes one round of device facts. A round outside the collector's
// hostname is refused and the object it addressed, when present, is marked
// invalid; its facts no longer take part in scheduling derivation (NODE-C-37).
func (c *Collector) Collect(ctx context.Context, report Report) (*unstructured.Unstructured, error) {
	if c.Writer == nil {
		return nil, errors.New("collector: a writer is required")
	}
	if c.Node == "" {
		return nil, errors.New("collector: the collector has no hostname")
	}
	if report.Object == "" {
		report.Object = c.Node
	}
	if report.Node == "" {
		report.Node = report.Object
	}
	if report.Object != c.Node || report.Node != report.Object {
		outOfBounds := &OutOfBounds{Collector: c.Node, Object: report.Object, Node: report.Node}
		if err := c.markInvalid(ctx, report.Object); err != nil {
			return nil, errors.Join(outOfBounds, err)
		}
		return nil, outOfBounds
	}

	now := c.now()
	devices := keepLatest(report.Devices)
	for index := range devices {
		if devices[index].ObservedAt == nil {
			devices[index].ObservedAt = &metav1.Time{Time: now}
		}
	}
	spec := v1beta1.NodeDeviceSpec{
		Node:       c.Node,
		Collector:  c.identity(),
		ObservedAt: &metav1.Time{Time: now},
		Devices:    devices,
	}
	value, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&spec)
	if err != nil {
		return nil, fmt.Errorf("encode device fact of %s: %w", c.Node, err)
	}
	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": api.Group + "/" + v1beta1.GroupVersion.Version,
		"kind":       "NodeDevice",
		"metadata": map[string]any{
			"name":   c.Node,
			"labels": map[string]any{markers.ManagedBy: write.ManagedByValue},
		},
		"spec": value,
	}}
	return c.Writer.Apply(ctx, write.SubjectDiscovery, write.Session{}, write.PartSpec, object)
}

// markInvalid marks an object outside the boundary invalid through the control
// plane's validator; a missing object is not an error because the refused write
// never created it (KOSD-C-25).
func (c *Collector) markInvalid(ctx context.Context, name string) error {
	validator := &device.Validator{Writer: c.Writer}
	err := validator.MarkInvalid(ctx, write.Ref{
		Group:   api.Group,
		Version: v1beta1.GroupVersion.Version,
		Kind:    "NodeDevice",
		Name:    name,
	})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// keepLatest keeps one entry per serial number: the one observed last. Entries
// without a serial number cannot be compared and are kept as they are
// (NODE-C-70).
func keepLatest(devices []v1beta1.DeviceFact) []v1beta1.DeviceFact {
	positions := map[string]int{}
	kept := make([]v1beta1.DeviceFact, 0, len(devices))
	for _, fact := range devices {
		if fact.Serial == "" {
			kept = append(kept, fact)
			continue
		}
		index, seen := positions[fact.Serial]
		if !seen {
			positions[fact.Serial] = len(kept)
			kept = append(kept, fact)
			continue
		}
		if observedAt(fact).After(observedAt(kept[index])) {
			kept[index] = fact
		}
	}
	return kept
}

// observedAt reads one entry's observation time; a missing value is the zero
// time, so it loses to every stamped entry.
func observedAt(fact v1beta1.DeviceFact) time.Time {
	if fact.ObservedAt == nil {
		return time.Time{}
	}
	return fact.ObservedAt.Time
}

// identity is the collection identity recorded in the object.
func (c *Collector) identity() string {
	if c.Identity == "" {
		return c.Node
	}
	return c.Identity
}

// now returns the observation time of this round.
func (c *Collector) now() time.Time {
	if c.Now == nil {
		return time.Now().UTC()
	}
	return c.Now().UTC()
}
