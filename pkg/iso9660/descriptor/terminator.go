package descriptor

import (
	"fmt"
	"github.com/bgrewell/iso-kit/pkg/consts"
	"github.com/bgrewell/iso-kit/pkg/iso9660/directory"
	"github.com/bgrewell/iso-kit/pkg/logging"
	"time"
)

const (
	// Terminator resrved size is the size of a sector minus 7 bytes
	TERMINATOR_RESERVED_SIZE = consts.ISO9660_SECTOR_SIZE - 7
)

// NewVolumeDescriptorSetTerminator creates a new VolumeDescriptorSetTerminator.
func NewVolumeDescriptorSetTerminator() *VolumeDescriptorSetTerminator {
	return &VolumeDescriptorSetTerminator{
		VolumeDescriptorHeader: VolumeDescriptorHeader{
			VolumeDescriptorType:    TYPE_TERMINATOR_DESCRIPTOR,
			StandardIdentifier:      consts.ISO9660_STD_IDENTIFIER,
			VolumeDescriptorVersion: consts.ISO9660_VOLUME_DESC_VERSION,
		},
		VolumeDescriptorSetTerminatorBody: VolumeDescriptorSetTerminatorBody{
			Reserved: [TERMINATOR_RESERVED_SIZE]byte{},
		},
	}
}

// VolumeDescriptorSetTerminator represents the Volume Descriptor Set Terminator (type 255).
type VolumeDescriptorSetTerminator struct {
	VolumeDescriptorHeader
	VolumeDescriptorSetTerminatorBody
}

// Marshal marshals the VolumeDescriptorSetTerminator into a byte array.
func (d *VolumeDescriptorSetTerminator) Marshal() ([consts.ISO9660_SECTOR_SIZE]byte, error) {
	var buf [consts.ISO9660_SECTOR_SIZE]byte
	offset := 0

	// 1. Marshal the VolumeDescriptorHeader (first 7 bytes).
	headerBytes, err := d.VolumeDescriptorHeader.Marshal()
	if err != nil {
		return buf, fmt.Errorf("failed to marshal VolumeDescriptorHeader: %w", err)
	}
	copy(buf[0:7], headerBytes[:])
	offset += 7

	// 2. Marshal the VolumeDescriptorSetTerminatorBody (remaining bytes).
	copy(buf[offset:offset+TERMINATOR_RESERVED_SIZE], d.VolumeDescriptorSetTerminatorBody.Reserved[:])
	offset += TERMINATOR_RESERVED_SIZE

	if offset != consts.ISO9660_SECTOR_SIZE {
		return buf, fmt.Errorf("marshal VolumeDescriptorSetTerminator: incorrect offset %d", offset)
	}

	return buf, nil
}

// Unmarshal parses a 2048-byte sector into the VolumeDescriptorSetTerminator.
func (d *VolumeDescriptorSetTerminator) Unmarshal(data [consts.ISO9660_SECTOR_SIZE]byte) error {
	offset := 0

	// 1. Unmarshal the VolumeDescriptorHeader (first 7 bytes).
	var headerBytes [7]byte
	copy(headerBytes[:], data[0:7])
	if err := d.VolumeDescriptorHeader.Unmarshal(headerBytes); err != nil {
		return fmt.Errorf("failed to unmarshal VolumeDescriptorHeader: %w", err)
	}
	offset += 7

	// 2. Unmarshal the VolumeDescriptorSetTerminatorBody (remaining bytes).
	copy(d.VolumeDescriptorSetTerminatorBody.Reserved[:], data[offset:offset+TERMINATOR_RESERVED_SIZE])

	if offset != consts.ISO9660_SECTOR_SIZE {
		return fmt.Errorf("unmarshal VolumeDescriptorSetTerminator: incorrect offset %d", offset)
	}

	return nil
}

// VolumeDescriptorSetTerminatorBody represents the body of the Volume Descriptor Set Terminator.
type VolumeDescriptorSetTerminatorBody struct {
	// Reserved for future standardization
	Reserved [TERMINATOR_RESERVED_SIZE]byte `json:"reserved"`
	// Logger
	Logger *logging.Logger
}

// VolumeIdentifier returns the volume identifier.
func (d *VolumeDescriptorSetTerminatorBody) VolumeIdentifier() string {
	return ""
}

// SystemIdentifier returns the system identifier.
func (d *VolumeDescriptorSetTerminatorBody) SystemIdentifier() string {
	return ""
}

// VolumeSetIdentifier returns the volume set identifier.
func (d *VolumeDescriptorSetTerminatorBody) VolumeSetIdentifier() string {
	return ""
}

func (d *VolumeDescriptorSetTerminatorBody) PublisherIdentifier() string {
	return ""
}

func (d *VolumeDescriptorSetTerminatorBody) DataPreparerIdentifier() string {
	return ""
}

func (d *VolumeDescriptorSetTerminatorBody) ApplicationIdentifier() string {
	return ""
}

func (d *VolumeDescriptorSetTerminatorBody) CopyrightFileIdentifier() string {
	return ""
}

func (d *VolumeDescriptorSetTerminatorBody) AbstractFileIdentifier() string {
	return ""
}

func (d *VolumeDescriptorSetTerminatorBody) BibliographicFileIdentifier() string {
	return ""
}

func (d *VolumeDescriptorSetTerminatorBody) VolumeCreationDateTime() time.Time {
	return time.Time{}
}

func (d *VolumeDescriptorSetTerminatorBody) VolumeModificationDateTime() time.Time {
	return time.Time{}
}

func (d *VolumeDescriptorSetTerminatorBody) VolumeExpirationDateTime() time.Time {
	return time.Time{}
}

func (d *VolumeDescriptorSetTerminatorBody) VolumeEffectiveDateTime() time.Time {
	return time.Time{}
}

func (d *VolumeDescriptorSetTerminatorBody) HasJoliet() bool {
	return false
}

func (d *VolumeDescriptorSetTerminatorBody) HasRockRidge() bool {
	return false
}

func (d *VolumeDescriptorSetTerminatorBody) RootDirectory() *directory.DirectoryRecord {
	return nil
}
