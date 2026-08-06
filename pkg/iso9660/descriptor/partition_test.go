package descriptor

import (
	"testing"

	"github.com/bgrewell/iso-kit/pkg/consts"
	"github.com/bgrewell/iso-kit/pkg/iso9660/directory"
	"github.com/stretchr/testify/require"
)

func TestVolumePartitionDescriptorRoundTrip(t *testing.T) {
	original := &VolumePartitionDescriptor{
		VolumeDescriptorHeader: VolumeDescriptorHeader{
			VolumeDescriptorType:    TYPE_PARTITION_DESCRIPTOR,
			StandardIdentifier:      consts.ISO9660_STD_IDENTIFIER,
			VolumeDescriptorVersion: consts.ISO9660_VOLUME_DESC_VERSION,
		},
		VolumePartitionDescriptorBody: VolumePartitionDescriptorBody{
			SystemIdentifier:          "TEST SYSTEM",
			VolumePartitionIdentifier: "PART1",
			VolumePartitionLocation:   1234,
			VolumePartitionSize:       5678,
		},
	}
	original.SystemUse[0] = 0xAB
	original.SystemUse[PARTITION_SYSTEM_USE_SIZE-1] = 0xCD

	data, err := original.Marshal()
	require.NoError(t, err)
	require.Len(t, data, consts.ISO9660_SECTOR_SIZE)

	var decoded VolumePartitionDescriptor
	require.NoError(t, decoded.Unmarshal([consts.ISO9660_SECTOR_SIZE]byte(data)))

	require.Equal(t, TYPE_PARTITION_DESCRIPTOR, decoded.VolumeDescriptorType)
	require.Equal(t, "TEST SYSTEM", decoded.VolumePartitionDescriptorBody.SystemIdentifier)
	require.Equal(t, "PART1", decoded.VolumePartitionIdentifier)
	require.Equal(t, uint32(1234), decoded.VolumePartitionLocation)
	require.Equal(t, uint32(5678), decoded.VolumePartitionSize)
	require.Equal(t, byte(0xAB), decoded.SystemUse[0])
	require.Equal(t, byte(0xCD), decoded.SystemUse[PARTITION_SYSTEM_USE_SIZE-1])

	// Byte-exact re-marshal.
	again, err := decoded.Marshal()
	require.NoError(t, err)
	require.Equal(t, data, again)
}

func TestVolumeDescriptorSetValidate(t *testing.T) {
	valid := func() *VolumeDescriptorSet {
		return &VolumeDescriptorSet{
			Primary: &PrimaryVolumeDescriptor{
				VolumeDescriptorHeader: VolumeDescriptorHeader{
					VolumeDescriptorType:    TYPE_PRIMARY_DESCRIPTOR,
					StandardIdentifier:      consts.ISO9660_STD_IDENTIFIER,
					VolumeDescriptorVersion: consts.ISO9660_VOLUME_DESC_VERSION,
				},
				PrimaryVolumeDescriptorBody: PrimaryVolumeDescriptorBody{
					VolumeSpaceSize:     100,
					LogicalBlockSize:    consts.ISO9660_SECTOR_SIZE,
					RootDirectoryRecord: &directory.DirectoryRecord{},
				},
			},
			Terminator: NewVolumeDescriptorSetTerminator(),
		}
	}

	require.NoError(t, valid().Validate())

	noPrimary := valid()
	noPrimary.Primary = nil
	require.Error(t, noPrimary.Validate())

	noTerminator := valid()
	noTerminator.Terminator = nil
	require.Error(t, noTerminator.Validate())

	noRoot := valid()
	noRoot.Primary.RootDirectoryRecord = nil
	require.Error(t, noRoot.Validate())

	zeroSpace := valid()
	zeroSpace.Primary.VolumeSpaceSize = 0
	require.Error(t, zeroSpace.Validate())

	badBlockSize := valid()
	badBlockSize.Primary.LogicalBlockSize = 512
	require.Error(t, badBlockSize.Validate())

	badID := valid()
	badID.Primary.StandardIdentifier = "XXXXX"
	require.Error(t, badID.Validate())
}
