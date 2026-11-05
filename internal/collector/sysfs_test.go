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
	hardware := linkDevice(t, root, disk, "sda")
	writeFile(t, filepath.Join(hardware, "serial"), "DISK-1\n")
	writeFile(t, filepath.Join(hardware, "model"), "Model X\n")
	linkDriver(t, root, hardware, "sd")

	unbound := deviceDir(t, root, "sdb")
	linkDevice(t, root, unbound, "sdb")

	deviceDir(t, root, "loop0")
	deviceDir(t, root, "zram0")
	optical := deviceDir(t, root, "sr0")
	linkDevice(t, root, optical, "sr0")

	// A broken link is not a device and must not fail the round.
	link(t, filepath.Join(root, "devices", "missing"), filepath.Join(root, "block", "gone"))

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

	// The fixture must keep sysfs' shape: device and driver are links the
	// source resolves, so a real directory there would hide a resolver that
	// stopped following links.
	for _, path := range []string{
		filepath.Join(disk, "device"),
		filepath.Join(disk, "device", "driver"),
		filepath.Join(unbound, "device"),
		filepath.Join(optical, "device"),
	} {
		assertLink(t, path)
	}

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
	holder(t, root, raid, "md0")
	deviceDir(t, root, "md0")

	mapped := deviceDir(t, root, "sdb")
	holder(t, root, mapped, "dm-0")
	mapper := deviceDir(t, root, "dm-0")
	writeFile(t, filepath.Join(mapper, "dm", "uuid"), "LVM-abc123\n")
	writeFile(t, filepath.Join(mapper, "dm", "name"), "vg-lv\n")

	// The unused device still carries a recognised device link, so its layer
	// facts stay readable next to the empty occupancy. Its holders directory
	// exists and holds nothing, which is the only shape sysfs gives a free
	// device.
	spare := deviceDir(t, root, "sdc")
	linkDevice(t, root, spare, "sdc")
	assertLink(t, filepath.Join(spare, "device"))
	assertEmptyDir(t, filepath.Join(spare, "holders"))

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
	assertFact(t, facts, "sdc", "disk", true, true, false)
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

// deviceDir creates one device directory with the link sysfs would expose. The
// holders directory stays empty for a device no system holds: sysfs always
// exposes it, and leaving it out would only exercise the read error instead of
// the empty listing.
func deviceDir(t *testing.T, root, name string) string {
	t.Helper()
	directory := filepath.Join(root, "devices", name)
	mkdir(t, directory)
	mkdir(t, filepath.Join(directory, "holders"))
	link(t, directory, filepath.Join(root, "block", name))
	return directory
}

// linkDevice creates the hardware directory behind a block device and links it
// in as device, the shape sysfs exposes for a recognised device.
func linkDevice(t *testing.T, root, directory, name string) string {
	t.Helper()
	hardware := filepath.Join(root, "devices", "hardware", name)
	mkdir(t, hardware)
	link(t, hardware, filepath.Join(directory, "device"))
	return hardware
}

// linkDriver creates one driver directory and links it into the device
// directory, the shape sysfs exposes for a bound driver.
func linkDriver(t *testing.T, root, hardware, name string) {
	t.Helper()
	driver := filepath.Join(root, "bus", "scsi", "drivers", name)
	mkdir(t, driver)
	link(t, driver, filepath.Join(hardware, "driver"))
}

// holder links one holding device into a device's holders directory.
func holder(t *testing.T, root, directory, name string) {
	t.Helper()
	holders := filepath.Join(directory, "holders")
	mkdir(t, holders)
	link(t, filepath.Join(root, "block", name), filepath.Join(holders, name))
}

// link points path at target with the relative link sysfs itself uses.
func link(t *testing.T, target, path string) {
	t.Helper()
	relative, err := filepath.Rel(filepath.Dir(path), target)
	if err != nil {
		t.Fatalf("relative path for %s: %v", path, err)
	}
	if err := os.Symlink(relative, path); err != nil {
		t.Fatalf("link %s: %v", path, err)
	}
}

// assertLink keeps the fixture honest: a real directory where sysfs has a link
// would let a source that stopped resolving links still pass.
func assertLink(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat %s: %v", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is a %s, want a symbolic link", path, info.Mode())
	}
}

// assertEmptyDir keeps a free device's fixture honest: the directory must
// exist and list nothing, since a missing one is a read error, not an empty
// sysfs listing.
func assertEmptyDir(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(entries) != 0 {
		t.Fatalf("%s holds %d entries, want none", path, len(entries))
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
