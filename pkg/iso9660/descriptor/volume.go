package descriptor

import (
	"github.com/bgrewell/iso-kit/pkg/iso9660/consts"
	"github.com/bgrewell/iso-kit/pkg/iso9660/directory"
	"time"
)

// VolumeDescriptorType represents the type of volume descriptor in the ISO9660 standard.
type VolumeDescriptorType byte

const (
	// TYPE_BOOT_RECORD indicates a Boot Record (type 0).
	TYPE_BOOT_RECORD VolumeDescriptorType = 0x00

	// TYPE_PRIMARY_DESCRIPTOR indicates a Primary Volume Descriptor (type 1).
	TYPE_PRIMARY_DESCRIPTOR VolumeDescriptorType = 0x01

	// TYPE_SUPPLEMENTARY_DESCRIPTOR indicates a Supplementary Volume Descriptor (type 2).
	TYPE_SUPPLEMENTARY_DESCRIPTOR VolumeDescriptorType = 0x02

	// TYPE_PARTITION_DESCRIPTOR indicates a Partition Volume Descriptor (type 3).
	TYPE_PARTITION_DESCRIPTOR VolumeDescriptorType = 0x03

	// TYPE_TERMINATOR_DESCRIPTOR indicates the Volume Descriptor Set Terminator (type 255).
	TYPE_TERMINATOR_DESCRIPTOR VolumeDescriptorType = 0xFF
)

type VolumeDescriptor interface {
	Type() VolumeDescriptorType
	Identifier() string
	Version() uint8
	VolumeIdentifier() string
	SystemIdentifier() string
	VolumeSetIdentifier() string
	PublisherIdentifier() string
	DataPreparerIdentifier() string
	ApplicationIdentifier() string
	CopyrightFileIdentifier() string
	AbstractFileIdentifier() string
	BibliographicFileIdentifier() string
	VolumeCreationDateTime() time.Time
	VolumeModificationDateTime() time.Time
	VolumeExpirationDateTime() time.Time
	VolumeEffectiveDateTime() time.Time
	HasJoliet() bool
	HasRockRidge() bool
	RootDirectory() *directory.DirectoryRecord
	Marshal() ([consts.ISO9660_SECTOR_SIZE]byte, error)
	Unmarshal(data [consts.ISO9660_SECTOR_SIZE]byte) error
}
