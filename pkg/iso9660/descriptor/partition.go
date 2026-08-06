package descriptor

import (
	"fmt"
	"github.com/bgrewell/iso-kit/pkg/consts"
	"github.com/bgrewell/iso-kit/pkg/helpers"
	"github.com/bgrewell/iso-kit/pkg/iso9660/directory"
	"github.com/bgrewell/iso-kit/pkg/iso9660/encoding"
	"github.com/bgrewell/iso-kit/pkg/iso9660/info"
	"github.com/bgrewell/iso-kit/pkg/logging"
	"strings"
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

func (d *VolumePartitionDescriptor) DescriptorType() VolumeDescriptorType {
	return TYPE_PARTITION_DESCRIPTOR
}

func (d *VolumePartitionDescriptor) VolumeIdentifier() string {
	return ""
}

func (d *VolumePartitionDescriptor) SystemIdentifier() string {
	return ""
}

func (d *VolumePartitionDescriptor) VolumeSetIdentifier() string {
	return ""
}

func (d *VolumePartitionDescriptor) PublisherIdentifier() string {
	return ""
}

func (d *VolumePartitionDescriptor) DataPreparerIdentifier() string {
	return ""
}

func (d *VolumePartitionDescriptor) ApplicationIdentifier() string {
	return ""
}

func (d *VolumePartitionDescriptor) CopyrightFileIdentifier() string {
	return ""
}

func (d *VolumePartitionDescriptor) AbstractFileIdentifier() string {
	return ""
}

func (d *VolumePartitionDescriptor) BibliographicFileIdentifier() string {
	return ""
}

func (d *VolumePartitionDescriptor) VolumeCreationDateTime() time.Time {
	return time.Time{}
}

func (d *VolumePartitionDescriptor) VolumeModificationDateTime() time.Time {
	return time.Time{}
}

func (d *VolumePartitionDescriptor) VolumeExpirationDateTime() time.Time {
	return time.Time{}
}

func (d *VolumePartitionDescriptor) VolumeEffectiveDateTime() time.Time {
	return time.Time{}
}

func (d *VolumePartitionDescriptor) HasJoliet() bool {
	return false
}

func (d *VolumePartitionDescriptor) HasRockRidge() bool {
	return false
}

func (d *VolumePartitionDescriptor) RootDirectory() *directory.DirectoryRecord {
	return nil
}

func (d *VolumePartitionDescriptor) GetObjects() []info.ImageObject {
	return []info.ImageObject{d}
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
	// --- Fields that are not part of the ISO9660 object ---
	// Object Location (in bytes)
	ObjectLocation int64 `json:"object_location"`
	// Object Size (in bytes)
	ObjectSize uint32 `json:"object_size"`
	// Logger
	Logger *logging.Logger
}

func (v VolumePartitionDescriptorBody) Type() string {
	return "Volume Descriptor"
}

func (v VolumePartitionDescriptorBody) Name() string {
	return "Volume Partition Descriptor"
}

func (v VolumePartitionDescriptorBody) Description() string {
	return fmt.Sprintf("%s: %s", v.SystemIdentifier, v.VolumePartitionIdentifier)
}

func (v VolumePartitionDescriptorBody) Properties() map[string]interface{} {
	return map[string]interface{}{
		"VolumePartitionLocation": v.VolumePartitionLocation,
		"VolumePartitionSize":     v.VolumePartitionSize,
	}
}

func (v VolumePartitionDescriptorBody) Offset() int64 {
	return v.ObjectLocation
}

func (v VolumePartitionDescriptorBody) Size() int {
	return int(v.ObjectSize)
}

// Marshal serializes the Volume Partition Descriptor into its 2048-byte
// on-disk representation (ECMA-119 8.6).
func (d *VolumePartitionDescriptor) Marshal() ([]byte, error) {
	var buf [consts.ISO9660_SECTOR_SIZE]byte
	offset := 0

	headerBytes, err := d.VolumeDescriptorHeader.Marshal()
	if err != nil {
		return buf[:], fmt.Errorf("failed to marshal VolumeDescriptorHeader: %w", err)
	}
	copy(buf[0:7], headerBytes[:])
	offset += 7

	// Unused field: 1 byte.
	buf[offset] = d.UnusedField1
	offset++

	// System Identifier: 32 bytes, space padded.
	copy(buf[offset:offset+32], helpers.PadString(d.VolumePartitionDescriptorBody.SystemIdentifier, 32))
	offset += 32

	// Volume Partition Identifier: 32 bytes, space padded.
	copy(buf[offset:offset+32], helpers.PadString(d.VolumePartitionIdentifier, 32))
	offset += 32

	// Volume Partition Location: 8 bytes, both-byte orders.
	locBytes := encoding.MarshalBothByteOrders32(d.VolumePartitionLocation)
	copy(buf[offset:offset+8], locBytes[:])
	offset += 8

	// Volume Partition Size: 8 bytes, both-byte orders.
	sizeBytes := encoding.MarshalBothByteOrders32(d.VolumePartitionSize)
	copy(buf[offset:offset+8], sizeBytes[:])
	offset += 8

	// System Use: remaining bytes.
	copy(buf[offset:offset+PARTITION_SYSTEM_USE_SIZE], d.SystemUse[:])
	offset += PARTITION_SYSTEM_USE_SIZE

	if offset != consts.ISO9660_SECTOR_SIZE {
		return buf[:], fmt.Errorf("marshal VolumePartitionDescriptor: incorrect offset %d", offset)
	}
	return buf[:], nil
}

// Unmarshal parses a 2048-byte sector into the VolumePartitionDescriptor.
func (d *VolumePartitionDescriptor) Unmarshal(data [consts.ISO9660_SECTOR_SIZE]byte) error {
	offset := 0

	var headerBytes [7]byte
	copy(headerBytes[:], data[0:7])
	if err := d.VolumeDescriptorHeader.Unmarshal(headerBytes); err != nil {
		return fmt.Errorf("failed to unmarshal VolumeDescriptorHeader: %w", err)
	}
	offset += 7

	d.UnusedField1 = data[offset]
	offset++

	d.VolumePartitionDescriptorBody.SystemIdentifier = strings.TrimRight(string(data[offset:offset+32]), " ")
	offset += 32

	d.VolumePartitionIdentifier = strings.TrimRight(string(data[offset:offset+32]), " ")
	offset += 32

	var locBytes [8]byte
	copy(locBytes[:], data[offset:offset+8])
	location, err := encoding.UnmarshalUint32LSBMSB(locBytes)
	if err != nil {
		return fmt.Errorf("failed to unmarshal VolumePartitionLocation: %w", err)
	}
	d.VolumePartitionLocation = location
	offset += 8

	var sizeBytes [8]byte
	copy(sizeBytes[:], data[offset:offset+8])
	size, err := encoding.UnmarshalUint32LSBMSB(sizeBytes)
	if err != nil {
		return fmt.Errorf("failed to unmarshal VolumePartitionSize: %w", err)
	}
	d.VolumePartitionSize = size
	offset += 8

	copy(d.SystemUse[:], data[offset:offset+PARTITION_SYSTEM_USE_SIZE])
	offset += PARTITION_SYSTEM_USE_SIZE

	if offset != consts.ISO9660_SECTOR_SIZE {
		return fmt.Errorf("unmarshal VolumePartitionDescriptor: incorrect offset %d", offset)
	}
	return nil
}
