package encoding

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
)

// MarshalBothByteOrders32 converts a uint32 value into an 8-byte field that
// encodes the value in both little‑endian and big‑endian orders.
// The resulting byte order is: (yz, wx, uv, st, st, uv, wx, yz),
// where (st uv wx yz) is the hexadecimal representation of the value.
func MarshalBothByteOrders32(val uint32) [8]byte {
	var data [8]byte
	// First four bytes: little-endian representation.
	binary.LittleEndian.PutUint32(data[0:4], val)
	// Last four bytes: big-endian representation.
	binary.BigEndian.PutUint32(data[4:8], val)
	return data
}

// UnmarshalUint32LSBMSB converts an 8-byte field encoded in both little‑
// and big‑endian orders back to a uint32 value. It verifies that both halves
// are equal. If they are not, it returns an error.
func UnmarshalUint32LSBMSB(data [8]byte) (uint32, error) {
	// Decode little-endian value from the first four bytes.
	little := binary.LittleEndian.Uint32(data[0:4])
	// Decode big-endian value from the last four bytes.
	big := binary.BigEndian.Uint32(data[4:8])
	if little != big {
		return 0, fmt.Errorf("mismatched both-byte orders: little-endian value %d != big-endian value %d", little, big)
	}
	return little, nil
}

// MarshalBothByteOrders16 converts a uint16 value into a 4-byte field that
// encodes the value in both little‑endian and big‑endian orders.
// The resulting field has the layout: (yz, wx, wx, yz), where (wx, yz) is the
// hexadecimal representation of the value.
// For example, for the value 0x1234, it returns [0x34, 0x12, 0x12, 0x34].
func MarshalBothByteOrders16(val uint16) [4]byte {
	var data [4]byte
	// First two bytes: little-endian representation.
	binary.LittleEndian.PutUint16(data[0:2], val)
	// Next two bytes: big-endian representation.
	binary.BigEndian.PutUint16(data[2:4], val)
	return data
}

// UnmarshalUint16LSBMSB converts a 4-byte field encoded in both little‑
// and big‑endian orders back to a uint16 value. It verifies that both halves
// match; if they do not, it returns an error.
func UnmarshalUint16LSBMSB(data [4]byte) (uint16, error) {
	// Read the little-endian value from the first two bytes.
	little := binary.LittleEndian.Uint16(data[0:2])
	// Read the big-endian value from the last two bytes.
	big := binary.BigEndian.Uint16(data[2:4])
	if little != big {
		return 0, fmt.Errorf("mismatched both-byte orders: little-endian value %d != big-endian value %d", little, big)
	}
	return little, nil
}

// MarshalDateTime converts a time.Time into a 17-byte field following ISO9660 8.4.26.1.
// The first 16 bytes contain ASCII digits in the format:
//
//	YYYY MM DD hh mm ss cc
//
// and the 17th byte is the time zone offset (in 15-minute intervals) as a signed integer.
// Note: This format is used in Volume Descriptors
func MarshalDateTime(t time.Time) ([17]byte, error) {
	var out [17]byte

	// Zero time => all binary zeros (unspecified). Real ISOs use 0x00, not ASCII '0'.
	if t.IsZero() {
		return out, nil
	}

	// Convert hundredths
	y, m, d := t.Date()
	hh, mm, ss := t.Clock()
	hundredths := t.Nanosecond() / 10_000_000

	// Format "YYYYMMDDhhmmsscc" (16 digits total)
	s := fmt.Sprintf("%04d%02d%02d%02d%02d%02d%02d",
		y, int(m), d, hh, mm, ss, hundredths)
	copy(out[:16], s)

	// Get offset in 15‑minute increments
	_, offsetSec := t.Zone()
	offset15 := int8(offsetSec / 900) // 900 = 15*60
	if offset15 < -48 || offset15 > 52 {
		return [17]byte{}, fmt.Errorf("offset %d out of ISO9660 bounds", offset15)
	}

	out[16] = byte(offset15)
	return out, nil
}

// UnmarshalDateTime converts a 17-byte ISO9660 date/time field into a time.Time.
// It expects the first 16 bytes to be ASCII digits representing
// YYYY MM DD hh mm ss cc, and the 17th byte as the offset in 15-minute intervals.
// Note: This format is used in Volume Descriptors
func UnmarshalDateTime(b [17]byte) (time.Time, error) {
	// Detect "unspecified" => all binary zeros (common in real ISOs) or 16 ASCII '0' + offset=0
	allZero := true
	for i := 0; i < 17; i++ {
		if b[i] != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return time.Time{}, nil
	}

	isUnspecified := true
	for i := 0; i < 16; i++ {
		if b[i] != '0' {
			isUnspecified = false
			break
		}
	}
	if isUnspecified && b[16] == 0 {
		return time.Time{}, nil
	}

	dtStr := string(b[:16])
	var (
		year, mon, day int
		hour, min, sec int
		hundredths     int
	)
	_, err := fmt.Sscanf(dtStr, "%4d%2d%2d%2d%2d%2d%2d",
		&year, &mon, &day, &hour, &min, &sec, &hundredths)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse error: %v", err)
	}
	nsec := hundredths * 10_000_000

	offset15 := int8(b[16])
	if offset15 < -48 || offset15 > 52 {
		return time.Time{}, fmt.Errorf("offset %d out of ISO9660 bounds", offset15)
	}
	offsetSec := int(offset15) * 900 // 15 min = 900s

	// Use UTC if offset=0, else a numeric zone for offset
	var loc *time.Location
	if offsetSec == 0 {
		loc = time.UTC
	} else {
		// name = "" => prints like "UTC-0800" in logs
		loc = time.FixedZone("", offsetSec)
	}

	return time.Date(year, time.Month(mon), day, hour, min, sec, nsec, loc), nil
}

// MarshalRecordingDateTime converts a time.Time into a 7-byte field according
// to Table 9 – Recording Date and Time. It returns an error if the year is out of range.
// Note: All fields are stored as numerical values (not ASCII digits).
// Note: This type format is used in DirectoryRecords
func MarshalRecordingDateTime(t time.Time) ([7]byte, error) {
	var b [7]byte

	year, month, day := t.Date()
	hour, minute, second := t.Clock()

	// The field stores the number of years since 1900, so valid years are 1900–2155.
	if year < 1900 || year > 2155 {
		return b, fmt.Errorf("year %d out of range for Recording Date and Time (must be between 1900 and 2155)", year)
	}
	b[0] = byte(year - 1900)
	b[1] = byte(month) // month is 1-12
	b[2] = byte(day)
	b[3] = byte(hour)
	b[4] = byte(minute)
	b[5] = byte(second)

	// Get the time zone offset in seconds and convert to 15-minute intervals.
	_, offsetSec := t.Zone()
	offset15 := int8(offsetSec / (15 * 60))
	if offset15 < -48 || offset15 > 52 {
		return b, fmt.Errorf("time zone offset %d (in 15-minute intervals: %d) is out of allowed range", offsetSec, offset15)
	}
	// Store offset as a signed numerical value.
	b[6] = byte(offset15)
	return b, nil
}

// UnmarshalRecordingDateTime converts a 7-byte Recording Date and Time field into a time.Time.
// The fields are interpreted as follows:
//
//	Byte 1: years since 1900,
//	Byte 2: month (1-12),
//	Byte 3: day,
//	Byte 4: hour,
//	Byte 5: minute,
//	Byte 6: second,
//	Byte 7: offset from GMT in 15-minute intervals (as a signed value).
//
// If all seven bytes are zero, it indicates that the date/time are not specified.
// Note: This type format is used in DirectoryRecords
func UnmarshalRecordingDateTime(b [7]byte) (time.Time, error) {
	// If all fields are zero, return the zero time.
	allZero := true
	for _, v := range b {
		if v != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return time.Time{}, nil
	}

	year := int(b[0]) + 1900
	month := time.Month(b[1])
	day := int(b[2])
	hour := int(b[3])
	minute := int(b[4])
	second := int(b[5])
	// b[6] is stored as a byte but represents a signed 8-bit integer.
	offset15 := int8(b[6])
	offsetSec := int(offset15) * 15 * 60

	loc := time.FixedZone("ISO9660", offsetSec)
	return time.Date(year, month, day, hour, minute, second, 0, loc), nil
}

// DecodeUCS2BigEndian converts a UCS-2 Big-Endian encoded byte slice to a Go (UTF-8) string.
func DecodeUCS2BigEndian(ucs2 []byte) string {
	if len(ucs2) < 2 {
		return "" // Nothing to decode
	}

	utf16Slice := make([]uint16, len(ucs2)/2)
	for i := 0; i < len(ucs2)/2; i++ {
		utf16Slice[i] = uint16(ucs2[2*i])<<8 | uint16(ucs2[2*i+1])
	}

	// Decode into runes
	runes := utf16.Decode(utf16Slice)

	// Convert to string and trim trailing nulls
	return strings.TrimRight(string(runes), "\x00")
}

// EncodeUCS2BigEndian converts a Go (UTF-8) string into a UTF-16
// (big-endian) byte slice. Any runes above U+FFFF become surrogate pairs.
func EncodeUCS2BigEndian(s string) []byte {
	// Convert the string into a slice of runes,
	// then encode them as UTF-16 code units (including surrogates as needed).
	runes := []rune(s)
	utf16encoded := utf16.Encode(runes)

	// Each UTF-16 code unit becomes two bytes in big-endian order.
	out := make([]byte, 2*len(utf16encoded))
	for i, code := range utf16encoded {
		// High byte first, then low byte.
		out[2*i] = byte(code >> 8)
		out[2*i+1] = byte(code & 0xFF)
	}
	return out
}
