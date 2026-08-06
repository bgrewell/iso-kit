package systemarea

import (
	"encoding/binary"
	"fmt"
)

const (
	// MBR layout offsets within the first 512 bytes of the image.
	mbrBootCodeSize       = 440
	mbrDiskSignatureStart = 440
	mbrPartitionTableOff  = 446
	mbrPartitionEntrySize = 16
	mbrPartitionCount     = 4
	mbrSignatureOff       = 510

	// Partition types commonly used in hybrid ISO images.
	MBR_TYPE_EMPTY          = 0x00
	MBR_TYPE_HIDDEN_NTFS    = 0x17 // classic isohybrid whole-image partition
	MBR_TYPE_LINUX          = 0x83
	MBR_TYPE_ISO9660        = 0xCD // used by GRUB hybrid images
	MBR_TYPE_EFI_SYSTEM     = 0xEF
	MBR_TYPE_GPT_PROTECTIVE = 0xEE
)

// MBRPartition is one entry of the MBR partition table. LBA values are in
// 512-byte units.
type MBRPartition struct {
	// Status is 0x80 for the active (bootable) partition, 0x00 otherwise.
	Status byte
	// Type is the partition type code (e.g. 0xEF for an EFI system
	// partition).
	Type byte
	// StartLBA is the first 512-byte sector of the partition.
	StartLBA uint32
	// SizeLBA is the partition length in 512-byte sectors.
	SizeLBA uint32
}

// IsEmpty reports whether the entry is unused.
func (p MBRPartition) IsEmpty() bool {
	return p.Type == MBR_TYPE_EMPTY && p.SizeLBA == 0
}

// MBR models the master boot record occupying the first 512 bytes of the
// system area of a hybrid ISO image.
type MBR struct {
	// BootCode is the x86 boot code area (up to 440 bytes, e.g.
	// syslinux's isohdpfx.bin truncated to fit).
	BootCode [mbrBootCodeSize]byte
	// DiskSignature is the 32-bit disk identifier at offset 440.
	DiskSignature uint32
	// Partitions is the four-entry partition table.
	Partitions [mbrPartitionCount]MBRPartition
}

// HasBootSignature reports whether data carries the 0x55AA MBR signature.
func HasBootSignature(data []byte) bool {
	return len(data) >= 512 && data[mbrSignatureOff] == 0x55 && data[mbrSignatureOff+1] == 0xAA
}

// ParseMBR decodes an MBR from the first 512 bytes of a system area.
// Returns nil if the boot signature is absent.
func ParseMBR(data []byte) *MBR {
	if !HasBootSignature(data) {
		return nil
	}
	mbr := &MBR{
		DiskSignature: binary.LittleEndian.Uint32(data[mbrDiskSignatureStart : mbrDiskSignatureStart+4]),
	}
	copy(mbr.BootCode[:], data[:mbrBootCodeSize])
	for i := 0; i < mbrPartitionCount; i++ {
		entry := data[mbrPartitionTableOff+i*mbrPartitionEntrySize:]
		mbr.Partitions[i] = MBRPartition{
			Status:   entry[0],
			Type:     entry[4],
			StartLBA: binary.LittleEndian.Uint32(entry[8:12]),
			SizeLBA:  binary.LittleEndian.Uint32(entry[12:16]),
		}
	}
	return mbr
}

// chsBeyond is the conventional CHS value for partitions addressed purely
// by LBA on media larger than CHS can express.
var chsBeyond = [3]byte{0xFE, 0xFF, 0xFF}

// chsFor computes a CHS tuple for an LBA using the 64-head, 32-sector
// geometry isohybrid assumes, saturating at the CHS limit.
func chsFor(lba uint32) [3]byte {
	const heads, sectors = 64, 32
	cylinder := lba / (heads * sectors)
	if cylinder > 1023 {
		return chsBeyond
	}
	head := (lba / sectors) % heads
	sector := lba%sectors + 1
	return [3]byte{
		byte(head),
		byte(sector&0x3F) | byte((cylinder>>2)&0xC0),
		byte(cylinder & 0xFF),
	}
}

// Marshal encodes the MBR into its 512-byte on-disk form.
func (m *MBR) Marshal() []byte {
	out := make([]byte, 512)
	copy(out[:mbrBootCodeSize], m.BootCode[:])
	binary.LittleEndian.PutUint32(out[mbrDiskSignatureStart:], m.DiskSignature)
	for i, p := range m.Partitions {
		if p.IsEmpty() {
			continue
		}
		entry := out[mbrPartitionTableOff+i*mbrPartitionEntrySize:]
		entry[0] = p.Status
		startCHS := chsFor(p.StartLBA)
		copy(entry[1:4], startCHS[:])
		entry[4] = p.Type
		endCHS := chsFor(p.StartLBA + p.SizeLBA - 1)
		copy(entry[5:8], endCHS[:])
		binary.LittleEndian.PutUint32(entry[8:12], p.StartLBA)
		binary.LittleEndian.PutUint32(entry[12:16], p.SizeLBA)
	}
	out[mbrSignatureOff] = 0x55
	out[mbrSignatureOff+1] = 0xAA
	return out
}

// SetPartition sets partition table entry index (0-3).
func (m *MBR) SetPartition(index int, p MBRPartition) error {
	if index < 0 || index >= mbrPartitionCount {
		return fmt.Errorf("partition index %d out of range", index)
	}
	m.Partitions[index] = p
	return nil
}
