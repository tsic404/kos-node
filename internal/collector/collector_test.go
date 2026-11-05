package collector

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/tsic404/kosd/api/v1beta1"
)

// TestKeepLatestKeepsNewestSerial covers the duplicate rule of NODE-C-70: one
// entry per serial number, the one observed last.
func TestKeepLatestKeepsNewestSerial(t *testing.T) {
	older := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	kept := keepLatest([]v1beta1.DeviceFact{
		{Name: "old", Serial: "S-1", Model: "A", ObservedAt: &metav1.Time{Time: older}},
		{Name: "new", Serial: "S-1", Model: "B", ObservedAt: &metav1.Time{Time: newer}},
		{Name: "other", Serial: "S-2", Model: "A", ObservedAt: &metav1.Time{Time: older}},
		{Name: "serial-less", ObservedAt: &metav1.Time{Time: older}},
	})
	if len(kept) != 3 {
		t.Fatalf("kept %d entries, want 3: %+v", len(kept), kept)
	}
	if kept[0].Name != "new" || kept[0].Model != "B" {
		t.Fatalf("duplicate serial kept %+v, want the newest entry", kept[0])
	}
	if kept[1].Name != "other" || kept[2].Name != "serial-less" {
		t.Fatalf("unrelated entries changed: %+v", kept)
	}
}
