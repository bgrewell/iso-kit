package boot

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogRoundTripSingleEntry(t *testing.T) {
	et := &ElTorito{
		Entries: []*ElToritoEntry{{
			Platform:    BIOS,
			Emulation:   NoEmulation,
			Bootable:    true,
			LoadSegment: 0x7C0,
		}},
	}
	et.Entries[0].SetExtent(500, 24*1024)

	data, err := et.Marshal()
	require.NoError(t, err)
	require.Len(t, data, 2048)

	decoded := &ElTorito{}
	require.NoError(t, decoded.UnmarshalBinary(data))
	require.Equal(t, BIOS, decoded.Platform)
	require.Len(t, decoded.Entries, 1)

	e := decoded.Entries[0]
	require.True(t, e.Bootable)
	require.Equal(t, BIOS, e.Platform)
	require.Equal(t, NoEmulation, e.Emulation)
	require.Equal(t, uint16(0x7C0), e.LoadSegment)
	require.Equal(t, uint32(500), e.Location())
	require.Equal(t, uint16(48), e.SectorCount()) // 24 KiB / 512
}

func TestCatalogRoundTripMultiPlatformSections(t *testing.T) {
	et := &ElTorito{
		Entries: []*ElToritoEntry{
			{Platform: BIOS, Emulation: NoEmulation, Bootable: true},
			{Platform: EFI, Emulation: NoEmulation, Bootable: true},
			{Platform: EFI, Emulation: NoEmulation, Bootable: false},
		},
	}
	et.Entries[0].SetExtent(100, 2048)
	et.Entries[1].SetExtent(200, 1440*1024)
	et.Entries[2].SetExtent(300, 4096)

	data, err := et.Marshal()
	require.NoError(t, err)

	// Section header for the EFI group: 0x91 (final), platform EFI,
	// two entries.
	require.Equal(t, byte(0x91), data[64])
	require.Equal(t, byte(EFI), data[65])
	require.Equal(t, byte(2), data[66])

	decoded := &ElTorito{}
	require.NoError(t, decoded.UnmarshalBinary(data))
	require.Len(t, decoded.Entries, 3)
	require.Equal(t, BIOS, decoded.Entries[0].Platform)
	require.Equal(t, EFI, decoded.Entries[1].Platform)
	require.Equal(t, EFI, decoded.Entries[2].Platform)
	require.True(t, decoded.Entries[1].Bootable)
	require.False(t, decoded.Entries[2].Bootable)
	require.Equal(t, uint32(200), decoded.Entries[1].Location())
}

func TestValidationEntryChecksum(t *testing.T) {
	et := &ElTorito{Entries: []*ElToritoEntry{{Platform: BIOS, Bootable: true}}}
	et.Entries[0].SetExtent(1, 512)
	data, err := et.Marshal()
	require.NoError(t, err)

	// A corrupted validation entry must be rejected.
	corrupt := bytes.Clone(data)
	corrupt[10] ^= 0xFF
	require.Error(t, (&ElTorito{}).UnmarshalBinary(corrupt))

	// Missing 0x55AA key bytes must be rejected.
	noKey := bytes.Clone(data)
	noKey[0x1E] = 0
	require.Error(t, (&ElTorito{}).UnmarshalBinary(noKey))

	// Truncated catalogs must be rejected.
	require.Error(t, (&ElTorito{}).UnmarshalBinary(data[:16]))
}

func TestMarshalEmptyCatalog(t *testing.T) {
	_, err := (&ElTorito{}).Marshal()
	require.Error(t, err)
}

func TestSetExtentLoadSizeSemantics(t *testing.T) {
	// Explicit LoadSize wins.
	e := &ElToritoEntry{LoadSize: 4}
	e.SetExtent(10, 1<<20)
	require.Equal(t, uint16(4), e.SectorCount())

	// Auto: rounded-up 512-byte blocks.
	e = &ElToritoEntry{}
	e.SetExtent(10, 1000)
	require.Equal(t, uint16(2), e.SectorCount())

	// Zero-size images still load one sector.
	e = &ElToritoEntry{}
	e.SetExtent(10, 0)
	require.Equal(t, uint16(1), e.SectorCount())

	// Saturates at the 16-bit ceiling.
	e = &ElToritoEntry{}
	e.SetExtent(10, 64*1024*1024)
	require.Equal(t, uint16(0xFFFF), e.SectorCount())
}

func TestBuildBootImageEntriesSizes(t *testing.T) {
	et := &ElTorito{
		Entries: []*ElToritoEntry{
			{Platform: BIOS, Bootable: true},
			{Platform: BIOS}, // location 0: skipped
		},
	}
	// A >128 KiB image: the size math must not truncate at 16 bits.
	et.Entries[0].SetExtent(700, 1440*1024)

	entries, err := et.BuildBootImageEntries()
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, uint64(1440*1024), entries[0].Size)
	require.Equal(t, uint32(700), entries[0].Location)
}

func TestExtractBootImages(t *testing.T) {
	// Backing image: boot content at sector 10.
	backing := make([]byte, 20*2048)
	payload := []byte("BOOTPAYLOAD")
	copy(backing[10*2048:], payload)

	et := &ElTorito{Entries: []*ElToritoEntry{{Platform: BIOS, Emulation: NoEmulation, Bootable: true}}}
	et.Entries[0].SetExtent(10, 512)

	outDir := t.TempDir()
	require.NoError(t, et.ExtractBootImages(bytes.NewReader(backing), outDir))

	files, err := os.ReadDir(outDir)
	require.NoError(t, err)
	require.Len(t, files, 1)

	data, err := os.ReadFile(filepath.Join(outDir, files[0].Name()))
	require.NoError(t, err)
	require.Len(t, data, 512)
	require.Equal(t, payload, data[:len(payload)])
}

func TestIsElTorito(t *testing.T) {
	require.True(t, IsElTorito("EL TORITO SPECIFICATION"))
	require.True(t, IsElTorito("EL TORITO SPECIFICATION\x00\x00"))
	require.False(t, IsElTorito("NOT A BOOT SYSTEM"))
	require.False(t, IsElTorito(""))
}

func TestEnumStrings(t *testing.T) {
	require.Equal(t, "BIOS", BIOS.String())
	require.Equal(t, "EFI", EFI.String())
	require.Equal(t, "Unknown", Platform(0x42).String())

	require.Equal(t, "NoEmul", NoEmulation.String())
	require.Equal(t, "HardDisk", HardDiskEmulation.String())
	require.Equal(t, "Unknown", Emulation(0x42).String())

	require.Equal(t, "EFI System", EFISystem.String())
	require.Equal(t, "Unknown", PartitionType(0x42).String())
}
