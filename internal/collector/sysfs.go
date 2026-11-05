package collector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tsic404/kosd/api/v1beta1"
)

// sysfsRoot is where the running kernel exposes its device tree.
const sysfsRoot = "/sys"

// SysfsSource reads the block devices the running kernel exposes. It reports
// the layers the host itself can prove: the device is present, the kernel
// recognised a hardware device behind it, a driver is bound and a storage
// system holds it (NODE-C-37).
//
// Publication as a schedulable resource is not a fact of the host: no publisher
// exists in this stage, so the layer stays unset rather than being inferred
// (NODE-C-37).
type SysfsSource struct {
	// Root is the sysfs mount point; empty means /sys.
	Root string
}

// Devices returns one entry per block device of this host.
func (s SysfsSource) Devices(ctx context.Context) ([]v1beta1.DeviceFact, error) {
	root := s.root()
	block := filepath.Join(root, "block")
	entries, err := os.ReadDir(block)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", block, err)
	}
	facts := make([]v1beta1.DeviceFact, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		directory := filepath.Join(block, name)
		// sysfs exposes every block device as a symbolic link; the link is
		// resolved before the entry counts as a device directory.
		info, err := os.Stat(directory)
		if err != nil || !info.IsDir() {
			continue
		}
		facts = append(facts, blockFact(block, directory, name))
	}
	return facts, nil
}

// blockFact reads one block device's facts from its sysfs directory.
func blockFact(block, directory, name string) v1beta1.DeviceFact {
	device := filepath.Join(directory, "device")
	return v1beta1.DeviceFact{
		Name:   name,
		Serial: firstValue(filepath.Join(device, "serial"), filepath.Join(directory, "serial")),
		Model:  firstValue(filepath.Join(device, "model")),
		Kind:   kindOf(name),
		// The kernel lists the device, so it exists.
		IsPresent: true,
		// A device behind the block node is one the kernel recognised; a
		// driver directory means a driver is bound to it. Neither implies
		// the other (NODE-C-37).
		IsRecognized:  isDirectory(device),
		IsDriverReady: isDirectory(filepath.Join(device, "driver")),
		Occupancy:     occupancy(block, directory),
	}
}

// kindOf classifies a block device by the class the kernel gives it. A device
// name is the kernel's own classification: loop and zram are not disks, and a
// mapped or array device is not the disk below it.
func kindOf(name string) string {
	switch {
	case strings.HasPrefix(name, "loop"):
		return "loop"
	case strings.HasPrefix(name, "zram"):
		return "memory"
	case strings.HasPrefix(name, "sr"):
		return "optical"
	case strings.HasPrefix(name, "md"):
		return "raid"
	case strings.HasPrefix(name, "dm-"):
		return "mapped"
	case strings.HasPrefix(name, "nbd"), strings.HasPrefix(name, "rbd"):
		return "network"
	default:
		return "disk"
	}
}

// occupancy reports the storage system that holds this device, read from the
// kernel's holder links. A device no system holds carries no occupancy
// structure (NODE-C-70).
func occupancy(block, directory string) *v1beta1.DeviceOccupancy {
	holders, err := os.ReadDir(filepath.Join(directory, "holders"))
	if err != nil || len(holders) == 0 {
		return nil
	}
	name := holders[0].Name()
	holder := filepath.Join(block, name)
	return &v1beta1.DeviceOccupancy{
		Source:    holdingSystem(holder, name),
		Reference: holdingObject(holder, name),
	}
}

// holdingSystem names the subsystem that holds the device: the device mapper
// records its kind in the uuid, an array device names itself, and anything else
// is reported as another block device holding it.
func holdingSystem(directory, name string) string {
	switch uuid := firstValue(filepath.Join(directory, "dm", "uuid")); {
	case strings.HasPrefix(uuid, "LVM-"):
		return "lvm"
	case strings.HasPrefix(uuid, "CRYPT-"):
		return "crypt"
	case strings.HasPrefix(uuid, "mpath-"):
		return "multipath"
	}
	if strings.HasPrefix(name, "md") {
		return "md"
	}
	return "block"
}

// holdingObject names the holding object inside that subsystem: the mapper name
// when the device mapper holds the device, the holder's own name otherwise.
func holdingObject(directory, name string) string {
	if mapped := firstValue(filepath.Join(directory, "dm", "name")); mapped != "" {
		return mapped
	}
	return name
}

// root is the sysfs mount point this source reads.
func (s SysfsSource) root() string {
	if s.Root == "" {
		return sysfsRoot
	}
	return s.Root
}

// firstValue returns the trimmed content of the first readable path, so a
// device that keeps its serial in either location still reports it.
func firstValue(paths ...string) string {
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if value := strings.TrimSpace(string(content)); value != "" {
			return value
		}
	}
	return ""
}

// isDirectory reports whether the path resolves to a directory, which is how
// sysfs exposes a bound driver and a recognised device.
func isDirectory(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
