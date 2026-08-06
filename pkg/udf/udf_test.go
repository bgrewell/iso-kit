package udf

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// buildTestImage constructs a minimal but structurally valid UDF image:
// VRS, AVDP, a volume descriptor sequence, and a partition holding a
// file set with a root directory, one file, and a subdirectory with a
// nested file.
//
// Absolute sector map:
//
//	16-18   VRS: BEA01, NSR02, TEA01
//	32-35   VDS: PVD, PD (partition 0 at sector 100), LVD, TD
//	256     AVDP
//	100+    partition blocks 0..8 (FSD, ICBs, directory data, content)
func buildTestImage(t *testing.T) []byte {
	t.Helper()
	const partStart = 100
	image := make([]byte, 300*SectorSize)

	writeVSD := func(sector int, id string) {
		off := sector * SectorSize
		image[off] = 0 // structure type
		copy(image[off+1:], id)
		image[off+6] = 1 // version
	}
	writeVSD(16, "BEA01")
	writeVSD(17, "NSR02")
	writeVSD(18, "TEA01")

	// writeTag stamps a descriptor tag over payload already placed at
	// the sector.
	writeTag := func(sector int, tagID uint16, tagLocation uint32) {
		off := sector * SectorSize
		binary.LittleEndian.PutUint16(image[off:], tagID)
		binary.LittleEndian.PutUint16(image[off+2:], 2) // version
		binary.LittleEndian.PutUint32(image[off+12:], tagLocation)
		var sum byte
		for i := 0; i < 16; i++ {
			if i == 4 {
				continue
			}
			sum += image[off+i]
		}
		image[off+4] = sum
	}

	putDString := func(off int, value string, fieldLen int) {
		image[off] = 8 // compression ID: latin-1
		copy(image[off+1:], value)
		image[off+fieldLen-1] = byte(1 + len(value))
	}

	putLongAD := func(off int, length, logicalBlock uint32, partition uint16) {
		binary.LittleEndian.PutUint32(image[off:], length)
		binary.LittleEndian.PutUint32(image[off+4:], logicalBlock)
		binary.LittleEndian.PutUint16(image[off+8:], partition)
	}

	// AVDP at 256: main VDS extent = sectors 32-35.
	{
		off := 256 * SectorSize
		binary.LittleEndian.PutUint32(image[off+16:], 4*SectorSize) // length
		binary.LittleEndian.PutUint32(image[off+20:], 32)           // location
		writeTag(256, TAG_ANCHOR_VOLUME_DESCRIPTOR_PTR, 256)
	}

	// PVD at 32.
	{
		off := 32 * SectorSize
		putDString(off+24, "UDFVOL", 32)
		putDString(off+72, "UDFSET", 128)
		writeTag(32, TAG_PRIMARY_VOLUME_DESCRIPTOR, 32)
	}

	// PD at 33: partition 0 at sector 100, 50 blocks.
	{
		off := 33 * SectorSize
		binary.LittleEndian.PutUint16(image[off+22:], 0) // partition number
		binary.LittleEndian.PutUint32(image[off+188:], partStart)
		binary.LittleEndian.PutUint32(image[off+192:], 50)
		writeTag(33, TAG_PARTITION_DESCRIPTOR, 33)
	}

	// LVD at 34: block size 2048, FSD at partition block 0.
	{
		off := 34 * SectorSize
		putDString(off+84, "UDFLOGVOL", 128)
		binary.LittleEndian.PutUint32(image[off+212:], SectorSize)
		putLongAD(off+248, SectorSize, 0, 0)
		writeTag(34, TAG_LOGICAL_VOLUME_DESCRIPTOR, 34)
	}

	// TD at 35.
	writeTag(35, TAG_TERMINATING_DESCRIPTOR, 35)

	// FSD at partition block 0: root ICB at block 1.
	{
		off := (partStart + 0) * SectorSize
		putDString(off+304, "FILESET", 32)
		putLongAD(off+400, SectorSize, 1, 0)
		writeTag(partStart+0, TAG_FILE_SET_DESCRIPTOR, 0)
	}

	// writeFE places a File Entry ICB at a partition block.
	writeFE := func(block int, fileType byte, infoLength uint64, perms uint32, ads []shortAD) {
		off := (partStart + block) * SectorSize
		image[off+16+11] = fileType
		image[off+16+18] = 0                                 // short_ad allocation
		binary.LittleEndian.PutUint32(image[off+36:], 1000)  // uid
		binary.LittleEndian.PutUint32(image[off+40:], 1000)  // gid
		binary.LittleEndian.PutUint32(image[off+44:], perms) // permissions
		binary.LittleEndian.PutUint64(image[off+56:], infoLength)
		adOff := off + 176
		binary.LittleEndian.PutUint32(image[off+172:], uint32(8*len(ads)))
		for _, ad := range ads {
			binary.LittleEndian.PutUint32(image[adOff:], ad.Length)
			binary.LittleEndian.PutUint32(image[adOff+4:], ad.LogicalBlock)
			adOff += 8
		}
		writeTag(partStart+block, TAG_FILE_ENTRY, uint32(block))
	}

	// makeFID encodes a file identifier descriptor; tag stamped later
	// when the directory data lands in a sector.
	makeFID := func(name string, characteristics byte, icbBlock uint32) []byte {
		nameBytes := []byte{}
		if name != "" {
			nameBytes = append([]byte{8}, name...)
		}
		total := 38 + len(nameBytes)
		total = (total + 3) / 4 * 4
		fid := make([]byte, total)
		binary.LittleEndian.PutUint16(fid[16:], 1) // file version
		fid[18] = characteristics
		fid[19] = byte(len(nameBytes))
		binary.LittleEndian.PutUint32(fid[20:], SectorSize) // ICB length
		binary.LittleEndian.PutUint32(fid[24:], icbBlock)
		copy(fid[38:], nameBytes)
		return fid
	}

	// stampFIDTags writes directory content into a partition block and
	// stamps each FID's descriptor tag.
	writeDirContent := func(block int, fids [][]byte) int {
		off := (partStart + block) * SectorSize
		pos := off
		for _, fid := range fids {
			copy(image[pos:], fid)
			// Stamp the tag in place.
			binary.LittleEndian.PutUint16(image[pos:], TAG_FILE_IDENTIFIER_DESCRIPTOR)
			binary.LittleEndian.PutUint16(image[pos+2:], 2)
			binary.LittleEndian.PutUint32(image[pos+12:], uint32(block))
			var sum byte
			for i := 0; i < 16; i++ {
				if i == 4 {
					continue
				}
				sum += image[pos+i]
			}
			image[pos+4] = sum
			pos += len(fid)
		}
		return pos - off
	}

	const permsRW = 0x1000 | 0x0800 | 0x0080 | 0x0004 // owner rw, group r, other r

	// Root directory content at block 2.
	rootFIDs := [][]byte{
		makeFID("", FID_CHAR_PARENT|FID_CHAR_DIRECTORY, 1),
		makeFID("hello.txt", 0, 3),
		makeFID("subdir", FID_CHAR_DIRECTORY, 4),
	}
	rootDirLen := writeDirContent(2, rootFIDs)
	writeFE(1, FILE_TYPE_DIRECTORY, uint64(rootDirLen), permsRW, []shortAD{{Length: uint32(rootDirLen), LogicalBlock: 2}})

	// hello.txt: ICB at block 3, content at block 5.
	content := []byte("hello from udf\n")
	copy(image[(partStart+5)*SectorSize:], content)
	writeFE(3, FILE_TYPE_REGULAR, uint64(len(content)), permsRW, []shortAD{{Length: uint32(len(content)), LogicalBlock: 5}})

	// subdir: ICB at block 4, content at block 6, containing nested.bin
	// (ICB block 7, content block 8).
	subFIDs := [][]byte{
		makeFID("", FID_CHAR_PARENT|FID_CHAR_DIRECTORY, 1),
		makeFID("nested.bin", 0, 7),
	}
	subDirLen := writeDirContent(6, subFIDs)
	writeFE(4, FILE_TYPE_DIRECTORY, uint64(subDirLen), permsRW, []shortAD{{Length: uint32(subDirLen), LogicalBlock: 6}})

	nested := []byte{0xCA, 0xFE, 0xBA, 0xBE}
	copy(image[(partStart+8)*SectorSize:], nested)
	writeFE(7, FILE_TYPE_REGULAR, uint64(len(nested)), permsRW, []shortAD{{Length: uint32(len(nested)), LogicalBlock: 8}})

	return image
}

func TestIsUDF(t *testing.T) {
	image := buildTestImage(t)
	require.True(t, IsUDF(bytes.NewReader(image)))

	// A plain ISO 9660 style VRS without NSR is not UDF.
	notUDF := make([]byte, 20*SectorSize)
	copy(notUDF[16*SectorSize+1:], "CD001")
	require.False(t, IsUDF(bytes.NewReader(notUDF)))

	require.False(t, IsUDF(bytes.NewReader(make([]byte, 20*SectorSize))))
}

func TestOpenAndList(t *testing.T) {
	image := buildTestImage(t)

	u, err := Open(bytes.NewReader(image))
	require.NoError(t, err)

	require.Equal(t, "UDFLOGVOL", u.GetVolumeID())
	require.Equal(t, "UDFSET", u.GetVolumeSetID())

	files, err := u.ListFiles()
	require.NoError(t, err)
	require.Len(t, files, 2)

	dirs, err := u.ListDirectories()
	require.NoError(t, err)
	require.Len(t, dirs, 1)
	require.Equal(t, "/subdir", dirs[0].FullPath)

	// Ownership and permissions decoded from the ICB.
	require.NotNil(t, files[0].UID)
	require.Equal(t, uint32(1000), *files[0].UID)
	require.Equal(t, os.FileMode(0o644), files[0].Mode.Perm())
}

func TestReadFile(t *testing.T) {
	image := buildTestImage(t)

	u, err := Open(bytes.NewReader(image))
	require.NoError(t, err)

	data, err := u.ReadFile("hello.txt")
	require.NoError(t, err)
	require.Equal(t, []byte("hello from udf\n"), data)

	data, err = u.ReadFile("/subdir/nested.bin")
	require.NoError(t, err)
	require.Equal(t, []byte{0xCA, 0xFE, 0xBA, 0xBE}, data)

	_, err = u.ReadFile("missing.txt")
	require.Error(t, err)
}

func TestExtract(t *testing.T) {
	image := buildTestImage(t)

	u, err := Open(bytes.NewReader(image))
	require.NoError(t, err)

	outDir := t.TempDir()
	require.NoError(t, u.Extract(outDir))

	data, err := os.ReadFile(filepath.Join(outDir, "hello.txt"))
	require.NoError(t, err)
	require.Equal(t, []byte("hello from udf\n"), data)

	data, err = os.ReadFile(filepath.Join(outDir, "subdir", "nested.bin"))
	require.NoError(t, err)
	require.Equal(t, []byte{0xCA, 0xFE, 0xBA, 0xBE}, data)
}

func TestWriteUnsupported(t *testing.T) {
	image := buildTestImage(t)
	u, err := Open(bytes.NewReader(image))
	require.NoError(t, err)

	require.ErrorIs(t, u.AddFile("x", nil), ErrWriteUnsupported)
	require.ErrorIs(t, u.RemoveFile("x"), ErrWriteUnsupported)
	require.ErrorIs(t, u.Save(nil), ErrWriteUnsupported)
	_, err = Create("x")
	require.ErrorIs(t, err, ErrWriteUnsupported)
}
