package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tsic404/kos-node/internal/collector"
	"github.com/tsic404/kosd/api/v1beta1"
)

// failingSource fails every round and counts how often it was asked.
type failingSource struct {
	rounds int
}

// Devices reports the same failure every round.
func (s *failingSource) Devices(context.Context) ([]v1beta1.DeviceFact, error) {
	s.rounds++
	return nil, errors.New("device tree unavailable")
}

// TestRunKeepsGoingAfterAFailedRound covers the rule that collection failure
// never blocks another function: a failed round is reported by Round and Run
// still schedules the next one (NODE-C-37).
func TestRunKeepsGoingAfterAFailedRound(t *testing.T) {
	source := &failingSource{}
	loop := &Agent{
		Collector: &collector.Collector{Node: "node-a"},
		Source:    source,
		Interval:  time.Millisecond,
	}
	if err := loop.Round(context.Background()); err == nil {
		t.Fatal("a failed collection round was reported as success")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := loop.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}
	if source.rounds < 2 {
		t.Fatalf("ran %d rounds, want the loop to keep collecting after a failure", source.rounds)
	}
}
