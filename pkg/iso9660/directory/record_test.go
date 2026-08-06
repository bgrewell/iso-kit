package directory

import (
	"testing"
	"time"

	"github.com/bgrewell/iso-kit/pkg/iso9660/encoding"
	"github.com/bgrewell/iso-kit/pkg/iso9660/extensions"
	"github.com/stretchr/testify/require"
)

func makeRecord(identifier string) *DirectoryRecord {
	return &DirectoryRecord{
		LocationOfExtent:       1234,
		DataLength:             5678,
		RecordingDateAndTime:   time.Date(2022, 5, 6, 7, 8, 9, 0, time.UTC),
		FileFlags:              FileFlags{},
		VolumeSequenceNumber:   1,
		LengthOfFileIdentifier: uint8(len(identifier)),
		FileIdentifier:         identifier,
	}
}

func TestRecordRoundTrip(t *testing.T) {
	original := makeRecord("HELLO.TXT;1")
	original.SystemUse = []byte{'R', 'R', 5, 1, 0x81, 0}

	data, err := original.Marshal()
	require.NoError(t, err)
	require.Equal(t, int(data[0]), len(data), "length byte must equal record size")
	require.Zero(t, len(data)%2, "records must have even length")

	var decoded DirectoryRecord
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, uint32(1234), decoded.LocationOfExtent)
	require.Equal(t, uint32(5678), decoded.DataLength)
	require.Equal(t, "HELLO.TXT;1", decoded.FileIdentifier)
	require.Equal(t, original.SystemUse, decoded.SystemUse)
	require.True(t, decoded.RecordingDateAndTime.Equal(original.RecordingDateAndTime))
}

func TestRecordFlagsRoundTrip(t *testing.T) {
	original := makeRecord("DIR")
	original.FileFlags = FileFlags{Directory: true, Hidden: true, MultiExtent: true}

	data, err := original.Marshal()
	require.NoError(t, err)

	var decoded DirectoryRecord
	require.NoError(t, decoded.Unmarshal(data))
	require.True(t, decoded.FileFlags.Directory)
	require.True(t, decoded.FileFlags.Hidden)
	require.True(t, decoded.FileFlags.MultiExtent)
	require.False(t, decoded.FileFlags.AssociatedFile)
}

func TestRecordPaddingByte(t *testing.T) {
	// Even-length identifiers get a pad byte; odd-length ones do not.
	even, err := makeRecord("AB").Marshal()
	require.NoError(t, err)
	odd, err := makeRecord("ABC").Marshal()
	require.NoError(t, err)
	require.Equal(t, len(even), len(odd), "pad byte should equalize record sizes")

	var decoded DirectoryRecord
	require.NoError(t, decoded.Unmarshal(even))
	require.Equal(t, "AB", decoded.FileIdentifier)
}

func TestRecordJolietIdentifierDecoding(t *testing.T) {
	name := "Ünïcode Nämé"
	encoded := string(encoding.EncodeUCS2BigEndian(name))
	original := makeRecord(encoded)

	data, err := original.Marshal()
	require.NoError(t, err)

	decoded := DirectoryRecord{Joliet: true}
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, name, decoded.FileIdentifier)

	// Special identifiers stay single-byte even under Joliet.
	special := makeRecord("\x00")
	data, err = special.Marshal()
	require.NoError(t, err)
	decoded = DirectoryRecord{Joliet: true}
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, "\x00", decoded.FileIdentifier)
}

func TestRecordUnmarshalErrors(t *testing.T) {
	var decoded DirectoryRecord
	require.Error(t, decoded.Unmarshal(nil))

	// Claimed length larger than the provided data.
	require.Error(t, decoded.Unmarshal([]byte{200, 0, 0}))
}

func TestIsSpecialAndBestName(t *testing.T) {
	dot := makeRecord("\x00")
	require.True(t, dot.IsSpecial())
	require.Equal(t, ".", dot.GetBestName(false))

	dotdot := makeRecord("\x01")
	require.True(t, dotdot.IsSpecial())
	require.Equal(t, "..", dotdot.GetBestName(false))

	plain := makeRecord("FILE.TXT;1")
	require.False(t, plain.IsSpecial())
	require.Equal(t, "FILE.TXT;1", plain.GetBestName(false))

	// Rock Ridge alternate name wins when enabled.
	altName := "real-name.txt"
	plain.RockRidge = &extensions.RockRidgeExtensions{AlternateName: &altName}
	require.Equal(t, altName, plain.GetBestName(true))
	require.Equal(t, "FILE.TXT;1", plain.GetBestName(false))
}

func TestGetPermissionsDefaults(t *testing.T) {
	file := makeRecord("F;1")
	require.Equal(t, uint32(0o644), uint32(file.GetPermissions(false).Perm()))

	dir := makeRecord("D")
	dir.FileFlags.Directory = true
	require.Equal(t, uint32(0o755), uint32(dir.GetPermissions(false).Perm()))
}

func TestFileFlagsMarshalBits(t *testing.T) {
	all := FileFlags{Hidden: true, Directory: true, AssociatedFile: true, RecordFormat: true, Protection: true, MultiExtent: true}
	b := all.Marshal()
	require.Equal(t, byte(0x9F), b)

	decoded, err := UnmarshalFileFlags(b)
	require.NoError(t, err)
	require.Equal(t, all, decoded)

	// Reserved bits are tolerated on read.
	withReserved, err := UnmarshalFileFlags(0x60)
	require.NoError(t, err)
	require.Equal(t, FileFlags{}, withReserved)
}
