package pathtable

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPathTableRecordRoundTrip(t *testing.T) {
	for _, littleEndian := range []bool{true, false} {
		for _, id := range []string{"A", "AB", "SUBDIR", "\x00"} {
			original := &PathTableRecord{
				LocationOfExtent:      4321,
				ParentDirectoryNumber: 7,
				DirectoryIdentifier:   id,
				littleEndian:          littleEndian,
			}

			data, err := original.Marshal()
			require.NoError(t, err)

			// Records with odd identifier lengths carry a pad byte.
			wantLen := 8 + len(id)
			if len(id)%2 != 0 {
				wantLen++
			}
			require.Len(t, data, wantLen)

			var decoded PathTableRecord
			require.NoError(t, decoded.Unmarshal(data, littleEndian))
			require.Equal(t, uint32(4321), decoded.LocationOfExtent, "endian=%v id=%q", littleEndian, id)
			require.Equal(t, uint16(7), decoded.ParentDirectoryNumber)
			require.Equal(t, id, decoded.DirectoryIdentifier)
		}
	}
}

func TestPathTableEndianDiffers(t *testing.T) {
	le := &PathTableRecord{LocationOfExtent: 0x01020304, ParentDirectoryNumber: 0x0506, DirectoryIdentifier: "X", littleEndian: true}
	be := &PathTableRecord{LocationOfExtent: 0x01020304, ParentDirectoryNumber: 0x0506, DirectoryIdentifier: "X", littleEndian: false}

	leData, err := le.Marshal()
	require.NoError(t, err)
	beData, err := be.Marshal()
	require.NoError(t, err)
	require.False(t, bytes.Equal(leData, beData), "L and M tables must differ in byte order")

	// Cross-decoding with the right endianness recovers the values.
	var decoded PathTableRecord
	require.NoError(t, decoded.Unmarshal(beData, false))
	require.Equal(t, uint32(0x01020304), decoded.LocationOfExtent)
}

func TestBuildPathTableFromRecords(t *testing.T) {
	pt := NewEmptyPathTable("Primary", true)
	pt.AddRecord("\x00", 20, 1)
	pt.AddRecord("BIN", 21, 1)
	pt.AddRecord("DOCS", 22, 1)
	pt.AddRecord("DEEP", 23, 3)
	pt.SetLocation(18, 40)

	data, err := pt.Marshal()
	require.NoError(t, err)

	// Reparse the marshaled table record-by-record.
	var records []*PathTableRecord
	offset := 0
	for offset < len(data) {
		var rec PathTableRecord
		require.NoError(t, rec.Unmarshal(data[offset:], true))
		records = append(records, &rec)
		recLen := 8 + int(rec.LengthOfDirectoryIdentifier)
		if rec.LengthOfDirectoryIdentifier%2 != 0 {
			recLen++
		}
		offset += recLen
	}

	require.Len(t, records, 4)
	require.Equal(t, "\x00", records[0].DirectoryIdentifier)
	require.Equal(t, uint16(1), records[1].ParentDirectoryNumber)
	require.Equal(t, "DEEP", records[3].DirectoryIdentifier)
	require.Equal(t, uint16(3), records[3].ParentDirectoryNumber)
	require.Equal(t, uint32(23), records[3].LocationOfExtent)

	require.Equal(t, int64(18*2048), pt.Offset())
	require.Equal(t, 40, pt.Size())
}

func TestPathTableRecordUnmarshalErrors(t *testing.T) {
	var rec PathTableRecord
	require.Error(t, rec.Unmarshal([]byte{1, 2, 3}, true))

	// Identifier length exceeding the data.
	bad := []byte{10, 0, 0, 0, 0, 0, 0, 0, 'A'}
	require.Error(t, rec.Unmarshal(bad, true))
}
