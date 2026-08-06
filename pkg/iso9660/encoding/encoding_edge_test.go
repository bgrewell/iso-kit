package encoding

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBothByteOrders32(t *testing.T) {
	data := MarshalBothByteOrders32(0x12345678)
	require.Equal(t, [8]byte{0x78, 0x56, 0x34, 0x12, 0x12, 0x34, 0x56, 0x78}, data)

	value, err := UnmarshalUint32LSBMSB(data)
	require.NoError(t, err)
	require.Equal(t, uint32(0x12345678), value)

	// Mismatched halves are corrupt data.
	data[0] = 0xFF
	_, err = UnmarshalUint32LSBMSB(data)
	require.Error(t, err)
}

func TestBothByteOrders16(t *testing.T) {
	data := MarshalBothByteOrders16(0x1234)
	require.Equal(t, [4]byte{0x34, 0x12, 0x12, 0x34}, data)

	value, err := UnmarshalUint16LSBMSB(data)
	require.NoError(t, err)
	require.Equal(t, uint16(0x1234), value)

	data[3] = 0xFF
	_, err = UnmarshalUint16LSBMSB(data)
	require.Error(t, err)
}

func TestRecordingDateTimeRoundTrip(t *testing.T) {
	for _, tc := range []time.Time{
		time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2155, 12, 31, 23, 59, 59, 0, time.UTC),
		time.Date(2024, 6, 15, 12, 30, 45, 0, time.FixedZone("UTC+2", 2*3600)),
	} {
		data, err := MarshalRecordingDateTime(tc)
		require.NoError(t, err)
		back, err := UnmarshalRecordingDateTime(data)
		require.NoError(t, err)
		require.True(t, back.Equal(tc), "round trip mismatch: got %v want %v", back, tc)
	}
}

func TestRecordingDateTimeYearBounds(t *testing.T) {
	_, err := MarshalRecordingDateTime(time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC))
	require.Error(t, err)
	_, err = MarshalRecordingDateTime(time.Date(2156, 1, 1, 0, 0, 0, 0, time.UTC))
	require.Error(t, err)
}

func TestDateTimeOffsetRoundTrip(t *testing.T) {
	// Positive and negative timezone offsets survive the 17-byte format.
	for _, tc := range []time.Time{
		time.Date(2020, 2, 29, 6, 7, 8, 0, time.FixedZone("UTC-6", -6*3600)),
		time.Date(2020, 2, 29, 6, 7, 8, 0, time.FixedZone("UTC+5:30", 5*3600+1800)),
	} {
		data, err := MarshalDateTime(tc)
		require.NoError(t, err)
		back, err := UnmarshalDateTime(data)
		require.NoError(t, err)
		require.True(t, back.Equal(tc), "round trip mismatch: got %v want %v", back, tc)
	}
}

func TestUCS2EncodeDecodeRoundTrip(t *testing.T) {
	for _, s := range []string{
		"",
		"ASCII",
		"Ünïcödé",
		"日本語",
	} {
		encoded := EncodeUCS2BigEndian(s)
		require.Zero(t, len(encoded)%2)
		require.Equal(t, s, DecodeUCS2BigEndian(encoded), "round trip for %q", s)
	}
}

func TestDecodeUCS2OddLength(t *testing.T) {
	// Trailing odd byte is ignored rather than corrupting the result.
	encoded := EncodeUCS2BigEndian("AB")
	require.Equal(t, "AB", DecodeUCS2BigEndian(append(encoded, 0x00)))
}
