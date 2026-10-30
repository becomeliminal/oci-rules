package layer

import (
	"encoding/binary"
	"fmt"
)

// The platforms an executable's header names, by ELF e_machine and Mach-O
// cputype. A machine not here is still a binary: it is named by its number,
// so that a check against an image's platform refuses it rather than missing
// it.
var (
	elfMachines = map[uint16]string{
		0x3e: "amd64",
		0xb7: "arm64",
	}
	machoCPUs = map[uint32]string{
		0x01000007: "amd64",
		0x0100000c: "arm64",
	}
)

// javaMajorFloor is the lowest class-file major version. A Java class file
// starts with the same four bytes as a universal Mach-O binary; the next
// four are its version, at least this, where a universal binary has its
// architecture count, which is small.
const javaMajorFloor = 45

// platformOf is the platform of the executable or shared object data holds,
// read from its header ("linux/amd64", "darwin/arm64"), and false for
// anything that is not one: a script, a text file, a timezone file.
//
// ELF is reported as linux: an image runs Linux, and a binary for another
// ELF system is not one this plugin builds.
func platformOf(data []byte) (string, bool) {
	switch {
	case len(data) >= 20 && string(data[:4]) == "\x7fELF":
		var machine uint16
		switch data[5] {
		case 1:
			machine = binary.LittleEndian.Uint16(data[18:20])
		case 2:
			machine = binary.BigEndian.Uint16(data[18:20])
		default:
			return "linux/unknown-byte-order", true
		}
		if arch, ok := elfMachines[machine]; ok {
			return "linux/" + arch, true
		}
		return fmt.Sprintf("linux/elf-machine-0x%x", machine), true
	case len(data) >= 8 && (string(data[:4]) == "\xcf\xfa\xed\xfe" || string(data[:4]) == "\xce\xfa\xed\xfe"):
		cpu := binary.LittleEndian.Uint32(data[4:8])
		if arch, ok := machoCPUs[cpu]; ok {
			return "darwin/" + arch, true
		}
		return fmt.Sprintf("darwin/cputype-0x%x", cpu), true
	case len(data) >= 8 && string(data[:4]) == "\xca\xfe\xba\xbe" && binary.BigEndian.Uint32(data[4:8]) < javaMajorFloor:
		return "darwin/universal", true
	}
	return "", false
}
