package layer

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// elf is the start of a 64-bit little-endian ELF file for machine.
func elf(machine uint16) string {
	header := make([]byte, 64)
	copy(header, "\x7fELF")
	header[4] = 2 // 64-bit
	header[5] = 1 // little-endian
	header[18] = byte(machine)
	header[19] = byte(machine >> 8)
	return string(header)
}

// macho is the start of a 64-bit Mach-O file for cputype.
func macho(cpu uint32) string {
	header := make([]byte, 32)
	copy(header, "\xcf\xfa\xed\xfe")
	header[4] = byte(cpu)
	header[5] = byte(cpu >> 8)
	header[6] = byte(cpu >> 16)
	header[7] = byte(cpu >> 24)
	return string(header)
}

func TestPlatformOf_ReadsThePlatformAHeaderNames(t *testing.T) {
	for _, tc := range []struct {
		name     string
		data     string
		platform string
		binary   bool
	}{
		{"an amd64 ELF", elf(0x3e), "linux/amd64", true},
		{"an arm64 ELF", elf(0xb7), "linux/arm64", true},
		{"an ELF for another machine", elf(0x28), "linux/elf-machine-0x28", true},
		{"an arm64 Mach-O", macho(0x0100000c), "darwin/arm64", true},
		{"an amd64 Mach-O", macho(0x01000007), "darwin/amd64", true},
		{"a universal Mach-O", "\xca\xfe\xba\xbe\x00\x00\x00\x02", "darwin/universal", true},
		{"a Java class file", "\xca\xfe\xba\xbe\x00\x00\x00\x34", "", false},
		{"a shell script", "#!/bin/sh\necho hi\n", "", false},
		{"a text file", "hello\n", "", false},
		{"an empty file", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// WHEN its header is read
			platform, binary := platformOf([]byte(tc.data))

			// THEN it is a binary for exactly the platform it names, or none
			require.Equal(t, tc.binary, binary)
			require.Equal(t, tc.platform, platform)
		})
	}
}

func TestWrite_RecordsTheBinariesALayerHolds(t *testing.T) {
	// GIVEN a layer of an amd64 executable and a text file
	app := writeInput(t, elf(0x3e), 0o755, time.Now())
	text := writeInput(t, "hello\n", 0o644, time.Now())

	// WHEN it is written
	_, meta := build(t, Spec{Entries: []Entry{
		{Path: "/app", Kind: KindFile, Source: app, Mode: mode(0o755)},
		{Path: "/etc/greeting", Kind: KindFile, Source: text},
	}})

	// THEN its metadata names the executable and its platform, and only it
	require.Equal(t, map[string]string{"/app": "linux/amd64"}, meta.Binaries)
}

func TestWrite_ALayerOfNoBinariesRecordsNone(t *testing.T) {
	// GIVEN a layer of a text file
	text := writeInput(t, "hello\n", 0o644, time.Now())

	// WHEN it is written
	_, meta := build(t, Spec{Entries: []Entry{{Path: "/etc/greeting", Kind: KindFile, Source: text}}})

	// THEN no binaries are recorded, so its metadata is what it always was
	require.Nil(t, meta.Binaries)
}

func TestWrite_RefusesABinaryBuiltForAnotherPlatformThanTheLayers(t *testing.T) {
	// GIVEN a linux/amd64 layer holding a darwin/arm64 executable, as a Mac's
	// host build gives
	app := writeInput(t, macho(0x0100000c), 0o755, time.Now())

	// WHEN it is written
	var out bytes.Buffer
	_, err := Write(Spec{Platform: "linux/amd64", Entries: []Entry{
		{Path: "/app", Kind: KindFile, Source: app, Mode: mode(0o755)},
	}}, &out)

	// THEN it is refused, naming the file and both platforms, and nothing is
	// written
	require.Error(t, err)
	require.Equal(t, "/app is built for darwin/arm64, but the layer is for linux/amd64; build it for linux/amd64", err.Error())
	require.Zero(t, out.Len())
}

func TestWrite_AcceptsABinaryBuiltForTheLayersPlatform(t *testing.T) {
	// GIVEN a linux/amd64 layer holding a linux/amd64 executable
	app := writeInput(t, elf(0x3e), 0o755, time.Now())

	// WHEN it is written
	_, meta := build(t, Spec{Platform: "linux/amd64", Entries: []Entry{
		{Path: "/app", Kind: KindFile, Source: app, Mode: mode(0o755)},
	}})

	// THEN it is written, recording it
	require.Equal(t, map[string]string{"/app": "linux/amd64"}, meta.Binaries)
}
