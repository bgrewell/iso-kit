package descriptor

import (
	"fmt"

	"github.com/bgrewell/iso-kit/pkg/consts"
)

type VolumeDescriptorSet struct {
	Boot          *BootRecordDescriptor
	Primary       *PrimaryVolumeDescriptor
	Partition     []*VolumePartitionDescriptor
	Supplementary []*SupplementaryVolumeDescriptor
	Terminator    *VolumeDescriptorSetTerminator
}

// Validate checks the structural invariants of the descriptor set: a
// primary descriptor and terminator must be present, the PVD must carry a
// root directory record and coherent sizes, and every descriptor must
// bear the ISO 9660 standard identifier.
func (s *VolumeDescriptorSet) Validate() error {
	if s.Primary == nil {
		return fmt.Errorf("volume descriptor set has no primary volume descriptor")
	}
	if s.Terminator == nil {
		return fmt.Errorf("volume descriptor set has no terminator")
	}
	if s.Primary.StandardIdentifier != consts.ISO9660_STD_IDENTIFIER {
		return fmt.Errorf("primary volume descriptor has invalid standard identifier %q", s.Primary.StandardIdentifier)
	}
	if s.Primary.RootDirectoryRecord == nil {
		return fmt.Errorf("primary volume descriptor has no root directory record")
	}
	if s.Primary.VolumeSpaceSize == 0 {
		return fmt.Errorf("primary volume descriptor has zero volume space size")
	}
	if s.Primary.LogicalBlockSize != consts.ISO9660_SECTOR_SIZE {
		return fmt.Errorf("unsupported logical block size %d", s.Primary.LogicalBlockSize)
	}
	for i, svd := range s.Supplementary {
		if svd.StandardIdentifier != consts.ISO9660_STD_IDENTIFIER {
			return fmt.Errorf("supplementary volume descriptor %d has invalid standard identifier %q", i, svd.StandardIdentifier)
		}
		if svd.RootDirectoryRecord == nil {
			return fmt.Errorf("supplementary volume descriptor %d has no root directory record", i)
		}
	}
	return nil
}
