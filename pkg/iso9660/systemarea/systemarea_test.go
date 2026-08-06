package systemarea

import (
	"encoding/binary"
	"hash/crc32"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMBRRoundTrip(t *testing.T) {
	original := &MBR{DiskSignature: 0xDEADBEEF}
	copy(original.BootCode[:], "BOOTCODE")
	original.Partitions[0] = MBRPartition{Status: 0x80, Type: MBR_TYPE_ISO9660, StartLBA: 0, SizeLBA: 4096}
	original.Partitions[1] = MBRPartition{Type: MBR_TYPE_EFI_SYSTEM, StartLBA: 128, SizeLBA: 2880}

	data := original.Marshal()
	require.Len(t, data, 512)
	require.True(t, HasBootSignature(data))

	decoded := ParseMBR(data)
	require.NotNil(t, decoded)
	require.Equal(t, uint32(0xDEADBEEF), decoded.DiskSignature)
	require.Equal(t, []byte("BOOTCODE"), decoded.BootCode[:8])
	require.Equal(t, original.Partitions[0], decoded.Partitions[0])
	require.Equal(t, original.Partitions[1], decoded.Partitions[1])
	require.True(t, decoded.Partitions[2].IsEmpty())
}

func TestParseMBRRejectsMissingSignature(t *testing.T) {
	require.Nil(t, ParseMBR(make([]byte, 512)))
	require.Nil(t, ParseMBR([]byte{1, 2, 3}))
	require.False(t, HasBootSignature(make([]byte, 100)))
}

func TestCHSSaturation(t *testing.T) {
	// Small LBA: a real CHS tuple.
	small := chsFor(0)
	require.Equal(t, [3]byte{0, 1, 0}, small)

	// Beyond the 1023-cylinder limit: the conventional saturation value.
	require.Equal(t, chsBeyond, chsFor(1024*64*32))
}

func TestSetPartitionBounds(t *testing.T) {
	m := &MBR{}
	require.NoError(t, m.SetPartition(0, MBRPartition{Type: 1, SizeLBA: 1}))
	require.NoError(t, m.SetPartition(3, MBRPartition{Type: 1, SizeLBA: 1}))
	require.Error(t, m.SetPartition(4, MBRPartition{}))
	require.Error(t, m.SetPartition(-1, MBRPartition{}))
}

func TestGPTBuildInvariants(t *testing.T) {
	g := &GPT{
		Partitions: []GPTPartition{{
			TypeGUID: GUID_EFI_SYSTEM,
			FirstLBA: 100,
			LastLBA:  199,
			Name:     "EFI System Partition",
		}},
	}
	const totalLBAs = 10000
	layout, err := g.Build(totalLBAs)
	require.NoError(t, err)

	// Header basics.
	h := layout.PrimaryHeader
	require.Equal(t, []byte("EFI PART"), h[:8])
	require.Equal(t, uint64(1), binary.LittleEndian.Uint64(h[24:32]))
	require.Equal(t, uint64(totalLBAs-1), binary.LittleEndian.Uint64(h[32:40]))

	// Header CRC verifies.
	hdrLen := binary.LittleEndian.Uint32(h[12:16])
	tmp := make([]byte, hdrLen)
	copy(tmp, h[:hdrLen])
	storedCRC := binary.LittleEndian.Uint32(tmp[16:20])
	tmp[16], tmp[17], tmp[18], tmp[19] = 0, 0, 0, 0
	require.Equal(t, storedCRC, crc32.ChecksumIEEE(tmp))

	// Entries CRC matches the stored value.
	entriesCRC := binary.LittleEndian.Uint32(h[88:92])
	require.Equal(t, entriesCRC, crc32.ChecksumIEEE(layout.Entries))

	// Backup header points back at the primary.
	b := layout.BackupHeader
	require.Equal(t, uint64(totalLBAs-1), binary.LittleEndian.Uint64(b[24:32]))
	require.Equal(t, uint64(1), binary.LittleEndian.Uint64(b[32:40]))

	// First entry carries the ESP type GUID and LBA range.
	e := layout.Entries[:128]
	require.Equal(t, GUID_EFI_SYSTEM[:], e[:16])
	require.Equal(t, uint64(100), binary.LittleEndian.Uint64(e[32:40]))
	require.Equal(t, uint64(199), binary.LittleEndian.Uint64(e[40:48]))

	// Partition name is UTF-16LE.
	require.Equal(t, byte('E'), e[56])
	require.Equal(t, byte(0), e[57])
}

func TestGPTBuildErrors(t *testing.T) {
	g := &GPT{}
	_, err := g.Build(10) // far too small
	require.Error(t, err)

	tooMany := &GPT{Partitions: make([]GPTPartition, 200)}
	_, err = tooMany.Build(100000)
	require.Error(t, err)
}

func TestGPTDeterministicOutput(t *testing.T) {
	build := func() []byte {
		g := &GPT{Partitions: []GPTPartition{{TypeGUID: GUID_EFI_SYSTEM, FirstLBA: 1, LastLBA: 2, Name: "ESP"}}}
		layout, err := g.Build(10000)
		require.NoError(t, err)
		return append(append([]byte{}, layout.PrimaryHeader...), layout.Entries...)
	}
	require.Equal(t, build(), build(), "GPT output should be reproducible")
}
