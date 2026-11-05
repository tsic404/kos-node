// Package agent runs the node agent's collection loop: one round per interval,
// each round writing this node's device fact object.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/tsic404/kos-node/internal/collector"
)

// DefaultInterval is the pause between collection rounds when none is set.
const DefaultInterval = time.Minute

// Agent collects this node's device facts once per interval.
type Agent struct {
	// Collector writes the fact object of this node.
	Collector *collector.Collector
	// Source supplies the device facts of each round.
	Source collector.Source
	// Interval is the pause between rounds; zero means DefaultInterval.
	Interval time.Duration
	// Logger records round failures; nil discards them.
	Logger *slog.Logger
}

// Round runs one collection round and writes the fact object.
func (a *Agent) Round(ctx context.Context) error {
	if a.Collector == nil {
		return errors.New("agent: a collector is required")
	}
	if a.Source == nil {
		return errors.New("agent: a device source is required")
	}
	devices, err := a.Source.Devices(ctx)
	if err != nil {
		return fmt.Errorf("read device facts: %w", err)
	}
	if _, err := a.Collector.Collect(ctx, collector.Report{Devices: devices}); err != nil {
		return fmt.Errorf("write device fact: %w", err)
	}
	return nil
}

// Run collects until the context ends. A failed round is logged and the next
// round still runs: collection failure never blocks another function
// (NODE-C-37).
func (a *Agent) Run(ctx context.Context) error {
	interval := a.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := a.Round(ctx); err != nil && ctx.Err() == nil {
			a.logger().Error("collection round failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// logger is the logger rounds report through.
func (a *Agent) logger() *slog.Logger {
	if a.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return a.Logger
}
