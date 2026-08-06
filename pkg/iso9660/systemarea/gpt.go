package systemarea

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"

	"github.com/bgrewell/iso-kit/pkg/iso9660/encoding"
)

const (
	gptSignature      = "EFI PART"
	gptRevision       = 0x00010000
	gptHeaderSize     = 92
	gptEntrySize      = 128
	gptEntryCount     = 128
	gptEntriesLBAs    = gptEntryCount * gptEntrySize / 512 // 32 sectors
	gptPrimaryLBA     = 1
	gptEntriesLBA     = 2
	gptFirstUsableLBA = gptEntriesLBA + gptEntriesLBAs // 34
)

// Partition type GUIDs (mixed-endian on disk).
var (
	// GUID_EFI_SYSTEM is C12A7328-F81F-11D2-BA4B-00A0C93EC93B.
	GUID_EFI_SYSTEM = [16]byte{0x28, 0x73, 0x2A, 0xC1, 0x1F, 0xF8, 0xD2, 0x11, 0xBA, 0x4B, 0x00, 0xA0, 0xC9, 0x3E, 0xC9, 0x3B}
	// GUID_BASIC_DATA is EBD0A0A2-B9E5-4433-87C0-68B6B72699C7.
	GUID_BASIC_DATA = [16]byte{0xA2, 0xA0, 0xD0, 0xEB, 0xE5, 0xB9, 0x33, 0x44, 0x87, 0xC0, 0x68, 0xB6, 0xB7, 0x26, 0x99, 0xC7}
)

// GPTPartition is one GUID partition table entry. LBA values are in
// 512-byte units.
type GPTPartition struct {
	TypeGUID   [16]byte
	UniqueGUID [16]byte
	FirstLBA   uint64
	LastLBA    uint64 // inclusive
	Name       string // up to 36 UTF-16 units
}

// GPT models a GUID partition table for a hybrid ISO image: primary
// header at LBA 1, entry array at LBA 2-33, and a backup at the end of
// the device.
type GPT struct {
	DiskGUID   [16]byte
	Partitions []GPTPartition
}

// deterministicGUID derives a stable GUID from a seed, keeping image
// output reproducible. The variant/version bits are set to look like a
// random (v4) GUID.
func deterministicGUID(seed string) [16]byte {
	var g [16]byte
	sum := crc32.ChecksumIEEE([]byte(seed))
	for i := range g {
		g[i] = byte(sum >> (uint(i%4) * 8))
		sum = sum*1664525 + 1013904223
	}
	g[7] = g[7]&0x0F | 0x40
	g[8] = g[8]&0x3F | 0x80
	return g
}

func (g *GPT) marshalEntries() []byte {
	entries := make([]byte, gptEntryCount*gptEntrySize)
	for i, p := range g.Partitions {
		e := entries[i*gptEntrySize:]
		copy(e[0:16], p.TypeGUID[:])
		copy(e[16:32], p.UniqueGUID[:])
		binary.LittleEndian.PutUint64(e[32:40], p.FirstLBA)
		binary.LittleEndian.PutUint64(e[40:48], p.LastLBA)
		// Attributes (8 bytes) left zero.
		name := encoding.EncodeUCS2BigEndian(p.Name)
		// GPT names are UTF-16LE; swap byte order and cap at 72 bytes.
		if len(name) > 72 {
			name = name[:72]
		}
		for j := 0; j+1 < len(name); j += 2 {
			e[56+j] = name[j+1]
			e[56+j+1] = name[j]
		}
	}
	return entries
}

func (g *GPT) marshalHeader(currentLBA, backupLBA, entriesLBA, totalLBAs uint64, entriesCRC uint32) []byte {
	h := make([]byte, 512)
	copy(h[0:8], gptSignature)
	binary.LittleEndian.PutUint32(h[8:12], gptRevision)
	binary.LittleEndian.PutUint32(h[12:16], gptHeaderSize)
	// CRC (16:20) computed last.
	binary.LittleEndian.PutUint64(h[24:32], currentLBA)
	binary.LittleEndian.PutUint64(h[32:40], backupLBA)
	binary.LittleEndian.PutUint64(h[40:48], gptFirstUsableLBA)
	binary.LittleEndian.PutUint64(h[48:56], totalLBAs-gptEntriesLBAs-2) // last usable
	copy(h[56:72], g.DiskGUID[:])
	binary.LittleEndian.PutUint64(h[72:80], entriesLBA)
	binary.LittleEndian.PutUint32(h[80:84], gptEntryCount)
	binary.LittleEndian.PutUint32(h[84:88], gptEntrySize)
	binary.LittleEndian.PutUint32(h[88:92], entriesCRC)
	crc := crc32.ChecksumIEEE(h[:gptHeaderSize])
	binary.LittleEndian.PutUint32(h[16:20], crc)
	return h
}

// GPTLayout is the serialized form of a GPT for a device of totalLBAs
// 512-byte sectors: the pieces are written at fixed device offsets.
type GPTLayout struct {
	// Primary header, written at LBA 1.
	PrimaryHeader []byte
	// Entry array, written at LBA 2.
	Entries []byte
	// Backup entry array, written at totalLBAs-33.
	BackupEntries []byte
	// Backup header, written at totalLBAs-1.
	BackupHeader []byte
	// TotalLBAs is the device size the layout was computed for.
	TotalLBAs uint64
}

// Build produces the on-disk GPT structures for a device of totalLBAs
// 512-byte sectors. The backup structures occupy the final 33 LBAs.
func (g *GPT) Build(totalLBAs uint64) (*GPTLayout, error) {
	if len(g.Partitions) > gptEntryCount {
		return nil, fmt.Errorf("too many GPT partitions: %d", len(g.Partitions))
	}
	if totalLBAs < gptFirstUsableLBA+gptEntriesLBAs+2 {
		return nil, fmt.Errorf("device too small for GPT: %d LBAs", totalLBAs)
	}
	var zero [16]byte
	if g.DiskGUID == zero {
		g.DiskGUID = deterministicGUID("iso-kit-disk")
	}
	for i := range g.Partitions {
		if g.Partitions[i].UniqueGUID == zero {
			g.Partitions[i].UniqueGUID = deterministicGUID(fmt.Sprintf("iso-kit-part-%d-%s", i, g.Partitions[i].Name))
		}
	}

	entries := g.marshalEntries()
	entriesCRC := crc32.ChecksumIEEE(entries)

	backupHeaderLBA := totalLBAs - 1
	backupEntriesLBA := totalLBAs - 1 - gptEntriesLBAs

	return &GPTLayout{
		PrimaryHeader: g.marshalHeader(gptPrimaryLBA, backupHeaderLBA, gptEntriesLBA, totalLBAs, entriesCRC),
		Entries:       entries,
		BackupEntries: entries,
		BackupHeader:  g.marshalHeader(backupHeaderLBA, gptPrimaryLBA, backupEntriesLBA, totalLBAs, entriesCRC),
		TotalLBAs:     totalLBAs,
	}, nil
}
