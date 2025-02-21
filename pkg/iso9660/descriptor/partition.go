package descriptor

import (
	"github.com/bgrewell/iso-kit/pkg/consts"
	"github.com/bgrewell/iso-kit/pkg/iso9660/directory"
	"github.com/bgrewell/iso-kit/pkg/logging"
	"time"
)

const (
	// Partition System Use Size is the size of a sector minus 88 bytes
	PARTITION_SYSTEM_USE_SIZE = consts.ISO9660_SECTOR_SIZE - 88
)

type VolumePartitionDescriptor struct {
	VolumeDescriptorHeader
	VolumePartitionDescriptorBody
}

func (d *VolumePartitionDescriptor) VolumeIdentifier() string {
	//TODO implement me
	panic("implement me")
}

func (d *VolumePartitionDescriptor) SystemIdentifier() string {
	//TODO implement me
	panic("implement me")
}

func (d *VolumePartitionDescriptor) VolumeSetIdentifier() string {
	//TODO implement me
	panic("implement me")
}

func (d *VolumePartitionDescriptor) PublisherIdentifier() string {
	//TODO implement me
	panic("implement me")
}

func (d *VolumePartitionDescriptor) DataPreparerIdentifier() string {
	//TODO implement me
	panic("implement me")
}

func (d *VolumePartitionDescriptor) ApplicationIdentifier() string {
	//TODO implement me
	panic("implement me")
}

func (d *VolumePartitionDescriptor) CopyrightFileIdentifier() string {
	//TODO implement me
	panic("implement me")
}

func (d *VolumePartitionDescriptor) AbstractFileIdentifier() string {
	//TODO implement me
	panic("implement me")
}

func (d *VolumePartitionDescriptor) BibliographicFileIdentifier() string {
	//TODO implement me
	panic("implement me")
}

func (d *VolumePartitionDescriptor) VolumeCreationDateTime() time.Time {
	//TODO implement me
	panic("implement me")
}

func (d *VolumePartitionDescriptor) VolumeModificationDateTime() time.Time {
	//TODO implement me
	panic("implement me")
}

func (d *VolumePartitionDescriptor) VolumeExpirationDateTime() time.Time {
	//TODO implement me
	panic("implement me")
}

func (d *VolumePartitionDescriptor) VolumeEffectiveDateTime() time.Time {
	//TODO implement me
	panic("implement me")
}

func (d *VolumePartitionDescriptor) HasJoliet() bool {
	//TODO implement me
	panic("implement me")
}

func (d *VolumePartitionDescriptor) HasRockRidge() bool {
	//TODO implement me
	panic("implement me")
}

func (d *VolumePartitionDescriptor) RootDirectory() *directory.DirectoryRecord {
	//TODO implement me
	panic("implement me")
}

type VolumePartitionDescriptorBody struct {
	// Unused field should always be 0x00
	UnusedField1 byte `json:"unusedField1"`
	// System Identifier specifies a system which can recognize and act upon the content of the Logical Sectors within
	// logical Sector Numbers 0 to 15 of the volume.
	//  | (a-characters)
	SystemIdentifier string `json:"system_identifier"`
	// Volume Partition Identifier specifies an identification of the Volume Partition.
	//  | (d-characters)
	VolumePartitionIdentifier string `json:"volume_partition_identifier"`
	// Volume Partition Location specifies the number of Logical Block Number of the first Logical Block allocated to
	// the Volume Partition
	//  | Encoding: BothByteOrder
	VolumePartitionLocation uint32 `json:"volume_partition_location"`
	// Volume Partition Size specifies the number of Logical Blocks in which the Volume Partition is recorded.
	//  | Encoding: BothByteOrder
	VolumePartitionSize uint32 `json:"volume_partition_size"`
	// System Use Area
	SystemUse [PARTITION_SYSTEM_USE_SIZE]byte `json:"system_use"`
	// Logger
	Logger *logging.Logger
}

func (d *VolumePartitionDescriptor) Marshal() ([consts.ISO9660_SECTOR_SIZE]byte, error) {
	return [consts.ISO9660_SECTOR_SIZE]byte{}, nil
}

func (d *VolumePartitionDescriptor) Unmarshal(data [consts.ISO9660_SECTOR_SIZE]byte) error {
	return nil
}
