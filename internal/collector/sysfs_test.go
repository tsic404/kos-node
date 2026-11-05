package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tsic404/kosd/api/v1beta1"
)

// TestSysfsSourceFollowsDeviceLinks covers what a host can prove about its own
// block devices: sysfs lists every device as a symbolic link, so the source has
// to resolve the link before reading the device; each entry carries the class
// the kernel gives it and the layers the host can prove (NODE-C-37).
func TestSysfsSourceFollowsDeviceLinks(t *testing.T) {
	root := t.TempDir()
	mkdir(t, filepath.Join(root, "block"))

	disk := deviceDir(t, root, "sda")
	writeFile(t, filepath.Join(disk, "device", "serial"), "DISK-1\n")
	writeFile(t, filepath.Join(disk, "device", "model"), "Model X\n")
	mkdir(t, filepath.Join(disk, "device", "driver"))

	unbound := deviceDir(t, root, "sdb")
	mkdir(t, filepath.Join(unbound, "device"))

	deviceDir(t, root, "loop0")
	deviceDir(t, root, "zram0")
	optical := deviceDir(t, root, "sr0")
	mkdir(t, filepath.Join(optical, "device"))

	// A broken link is not a device and must not fail the round.
	if err := os.Symlink(filepath.Join("..", "devices", "missing"), filepath.Join(root, "block", "gone")); err != nil {
		t.Fatalf("link the missing device: %v", err)
	}

	facts, err := (SysfsSource{Root: root}).Devices(context.Background())
	if err != nil {
		t.Fatalf("read device facts: %v", err)
	}
	if len(facts) != 5 {
		t.Fatalf("read %d devices, want 5: %+v", len(facts), facts)
	}
	assertFact(t, facts, "sda", "disk", true, true, true)
	assertFact(t, facts, "sdb", "disk", true, true, false)
	assertFact(t, facts, "loop0", "loop", true, false, false)
	assertFact(t, facts, "zram0", "memory", true, false, false)
	assertFact(t, facts, "sr0", "optical", true, true, false)

	diskFact := factNamed(t, facts, "sda")
	if diskFact.Serial != "DISK-1" || diskFact.Model != "Model X" {
		t.Fatalf("sda identity = %+v", diskFact)
	}
	if diskFact.IsPublished {
		t.Fatalf("sda reports a publication the host cannot prove: %+v", diskFact)
	}
}

// TestSysfsSourceReportsOccupancy covers the storage systems that hold a device:
// an array member and a device-mapper member carry their holding system and the
// holding object, an unused device carries no occupancy structure (NODE-C-37,
// NODE-C-70).
func TestSysfsSourceReportsOccupancy(t *testing.T) {
	root := t.TempDir()
	mkdir(t, filepath.Join(root, "block"))

	raid := deviceDir(t, root, "sda")
	holder(t, raid, "md0")
	deviceDir(t, root, "md0")

	mapped := deviceDir(t, root, "sdb")
	holder(t, mapped, "dm-0")
	mapper := deviceDir(t, root, "dm-0")
	writeFile(t, filepath.Join(mapper, "dm", "uuid"), "LVM-abc123\n")
	writeFile(t, filepath.Join(mapper, "dm", "name"), "vg-lv\n")

	deviceDir(t, root, "sdc")

	facts, err := (SysfsSource{Root: root}).Devices(context.Background())
	if err != nil {
		t.Fatalf("read device facts: %v", err)
	}
	raidFact := factNamed(t, facts, "sda")
	if raidFact.Occupancy == nil || raidFact.Occupancy.Source != "md" || raidFact.Occupancy.Reference != "md0" {
		t.Fatalf("array member occupancy = %+v, want md/md0", raidFact.Occupancy)
	}
	mappedFact := factNamed(t, facts, "sdb")
	if mappedFact.Occupancy == nil || mappedFact.Occupancy.Source != "lvm" || mappedFact.Occupancy.Reference != "vg-lv" {
		t.Fatalf("mapped member occupancy = %+v, want lvm/vg-lv", mappedFact.Occupancy)
	}
	if free := factNamed(t, facts, "sdc"); free.Occupancy != nil {
		t.Fatalf("an unused device carries an occupancy structure: %+v", free.Occupancy)
	}
}

// assertFact checks the class and the three layers the host reports for one
// device.
func assertFact(t *testing.T, facts []v1beta1.DeviceFact, name, kind string, present, recognized, driverReady bool) {
	t.Helper()
	fact := factNamed(t, facts, name)
	if fact.Kind != kind || fact.IsPresent != present || fact.IsRecognized != recognized || fact.IsDriverReady != driverReady {
		t.Fatalf("%s = kind %q present %v recognized %v driverReady %v, want %q %v %v %v",
			name, fact.Kind, fact.IsPresent, fact.IsRecognized, fact.IsDriverReady, kind, present, recognized, driverReady)
	}
}

// deviceDir creates one device directory and the block link sysfs would expose.
func deviceDir(t *testing.T, root, name string) string {
	t.Helper()
	directory := filepath.Join(root, "devices", name)
	mkdir(t, directory)
	if err := os.Symlink(filepath.Join("..", "devices", name), filepath.Join(root, "block", name)); err != nil {
		t.Fatalf("link device %s: %v", name, err)
	}
	return directory
}

// holder links one holding device into a device's holders directory.
func holder(t *testing.T, directory, name string) {
	t.Helper()
	holders := filepath.Join(directory, "holders")
	mkdir(t, holders)
	if err := os.Symlink(filepath.Join("..", "..", "..", "block", name), filepath.Join(holders, name)); err != nil {
		t.Fatalf("link holder %s: %v", name, err)
	}
}

// factNamed finds one device fact by name.
func factNamed(t *testing.T, facts []v1beta1.DeviceFact, name string) v1beta1.DeviceFact {
	t.Helper()
	for _, fact := range facts {
		if fact.Name == name {
			return fact
		}
	}
	t.Fatalf("no fact for %s: %+v", name, facts)
	return v1beta1.DeviceFact{}
}

// writeFile creates one sysfs attribute the case reads.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// mkdir creates one sysfs directory the case reads.
func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
}
