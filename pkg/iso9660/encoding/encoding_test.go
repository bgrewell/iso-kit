package encoding

import (
	"encoding/binary"
	"fmt"
	"github.com/stretchr/testify/require"
	"strconv"
	"testing"
	"time"
)

func TestISO9660DateTime(t *testing.T) {
	t.Run("Unspecified_AllZero", func(t *testing.T) {
		// 16 ASCII '0' plus offset=0
		var zeros [17]byte
		for i := 0; i < 16; i++ {
			zeros[i] = '0'
		}
		zeros[16] = 0

		// Unmarshal ASCII '0' form => zero time
		tm, err := UnmarshalDateTime(zeros)
		require.NoError(t, err)
		require.True(t, tm.IsZero())

		// Marshal that zero time => binary zeros (real-world ISO convention)
		reBytes, err := MarshalDateTime(tm)
		require.NoError(t, err)
		var binaryZeros [17]byte
		require.Equal(t, binaryZeros, reBytes)
	})

	t.Run("RoundTripUTCOffset0", func(t *testing.T) {
		want := time.Date(2023, 6, 1, 12, 0, 0, 500_000_000, time.UTC) // 50 hundredths
		data, err := MarshalDateTime(want)
		require.NoError(t, err, "marshaling should succeed")

		got, err := UnmarshalDateTime(data)
		require.NoError(t, err, "unmarshaling should succeed")

		// They should be the same moment and both in time.UTC
		require.Equal(t, want, got, "round-tripped time must match exactly")
		require.Equal(t, time.UTC, got.Location(), "Location should be UTC")
	})

	t.Run("RoundTripNonZeroOffset", func(t *testing.T) {
		// +3 hours => offset15 = +12
		loc := time.FixedZone("", 3*3600)
		want := time.Date(2023, 12, 31, 23, 59, 30, 370_000_000, loc)
		// => 37 hundredths

		data, err := MarshalDateTime(want)
		require.NoError(t, err)

		got, err := UnmarshalDateTime(data)
		require.NoError(t, err)

		// Times should match exactly, including offset
		require.Equal(t, want, got)
		require.Equal(t, loc, got.Location())
	})

	t.Run("OffsetOutOfRange", func(t *testing.T) {
		// e.g. +53 => out of range
		loc := time.FixedZone("", 53*900) // 53 * 15 mins
		badTime := time.Date(2023, 1, 1, 0, 0, 0, 0, loc)

		_, err := MarshalDateTime(badTime)
		require.Error(t, err, "offset 53 is beyond +52 => should fail")
	})
}

func TestMarshalDateTime(t *testing.T) {
	tests := []struct {
		name      string
		timeVal   time.Time
		wantBytes string // Expected first 16 bytes as a string ("YYYYMMDDhhmmsscc")
		wantOff   int8   // Expected final offset (15-min increments)
		wantErr   bool
	}{
		{
			name:      "zero time",
			timeVal:   time.Time{}, // t.IsZero() == true
			wantBytes: "\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00",
			wantOff:   0,
			wantErr:   false,
		},
		{
			name: "normal date/time with fraction, +8 offset",
			// 2025-01-02 03:04:05.500 in a +8 hour zone => offsetSec = 8 * 3600 = 28800 => offset in 15-min increments = 28800 / 900 = 32
			timeVal: time.Date(2025, 1, 2, 3, 4, 5, 500_000_000, time.FixedZone("UTC+8", 8*3600)),
			// Year=2025 -> "2025"
			// Month=01 -> "01"
			// Day=02 -> "02"
			// Hour=03 -> "03"
			// Min=04 -> "04"
			// Sec=05 -> "05"
			// Hundredths=500ms => 500_000_000 ns / 10_000_000 = 50 -> "50"
			wantBytes: "2025010203040550",
			wantOff:   32,
			wantErr:   false,
		},
		{
			name: "negative offset, -6",
			// 2023-12-31 23:59:00 in a -6 hour zone => offsetSec = -6 * 3600 = -21600 => offset= -21600 / 900 = -24
			timeVal:   time.Date(2023, 12, 31, 23, 59, 0, 0, time.FixedZone("UTC-6", -6*3600)),
			wantBytes: "2023123123590000", // Hundredths=00
			wantOff:   -24,
			wantErr:   false,
		},
		{
			name: "offset out of range (+14 hours => error)",
			// +14 hours => offsetSec = 14 * 3600 = 50400 => offset= 50400/900=56 => out of range
			timeVal: time.Date(2025, 5, 10, 10, 30, 0, 0, time.FixedZone("UTC+14", 14*3600)),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MarshalDateTime(tt.timeVal)

			if tt.wantErr {
				require.Error(t, err, "Expected an error for out-of-range offset")
				return
			}

			require.NoError(t, err, "Unexpected error")

			// Check the first 16 bytes as a string
			gotStr := string(got[:16])
			require.Equal(t, tt.wantBytes, gotStr, "Mismatch in first 16 bytes")

			// Check the final offset byte
			offset := int8(got[16])
			require.Equal(t, tt.wantOff, offset, "Mismatch in offset byte")

			if tt.timeVal.IsZero() {
				for i := 0; i < 17; i++ {
					require.Equal(t, byte(0), got[i], "Expected zero-time to have 0x00 in each byte")
				}
			} else {
				// Further optional checks: parse the date/time portions from gotStr
				yyyy, _ := strconv.Atoi(gotStr[0:4])
				MM, _ := strconv.Atoi(gotStr[4:6])
				dd, _ := strconv.Atoi(gotStr[6:8])
				hh, _ := strconv.Atoi(gotStr[8:10])
				mm, _ := strconv.Atoi(gotStr[10:12])
				ss, _ := strconv.Atoi(gotStr[12:14])
				cc, _ := strconv.Atoi(gotStr[14:16]) // hundredths

				// Compare with original time, ignoring time zone because we can't invert offset easily in a direct parse.
				require.Equal(t, tt.timeVal.Year(), yyyy, "Year mismatch")
				require.Equal(t, int(tt.timeVal.Month()), MM, "Month mismatch")
				require.Equal(t, tt.timeVal.Day(), dd, "Day mismatch")
				require.Equal(t, tt.timeVal.Hour(), hh, "Hour mismatch")
				require.Equal(t, tt.timeVal.Minute(), mm, "Minute mismatch")
				require.Equal(t, tt.timeVal.Second(), ss, "Second mismatch")

				// Reconstruct hundredths from the original time
				expectedCC := tt.timeVal.Nanosecond() / 10_000_000
				require.Equal(t, expectedCC, cc, "Hundredths mismatch")
			}
		})
	}
}

func TestUnmarshalDateTime(t *testing.T) {
	tests := []struct {
		name    string
		input   [17]byte
		want    time.Time
		wantErr bool
	}{
		{
			name: "unspecified zero time",
			// 16 ASCII '0' plus offset=0
			input: [17]byte{
				'0', '0', '0', '0',
				'0', '0', '0', '0',
				'0', '0', '0', '0',
				'0', '0', '0', '0',
				0,
			},
			want:    time.Time{}, // zero time
			wantErr: false,
		},
		{
			name: "normal date/time +8 offset",
			// Example: "2025010203040550" => year=2025, mon=01, day=02, hour=03, min=04, sec=05, hundredths=50 => 500ms
			// offset=+8 => offset= (8 * 3600) / 900=32 => byte(32)
			input: func() [17]byte {
				var arr [17]byte
				copy(arr[:16], []byte("2025010203040550"))
				arr[16] = 32 // +8 hours in 15-min increments
				return arr
			}(),
			want:    time.Date(2025, 1, 2, 3, 4, 5, 50*10_000_000, time.FixedZone("", 8*3600)),
			wantErr: false,
		},
		{
			name: "negative offset -6",
			// "2023123123590000", offset = -6 => offset= (-6*3600)/900= -24 => arr[16]= -24
			input: func() [17]byte {
				var arr [17]byte
				copy(arr[:16], []byte("2023123123590000"))
				arr[16] = 0xE8 // 0xE8 is -24 in signed 8-bit
				return arr
			}(),
			want:    time.Date(2023, 12, 31, 23, 59, 0, 0, time.FixedZone("", -6*3600)),
			wantErr: false,
		},
		{
			name: "offset out of range",
			// +14 hours => offset=56 in 15-min increments => out of ISO9660 range (max=52)
			input: func() [17]byte {
				var arr [17]byte
				copy(arr[:16], []byte("2025051010300000")) // any valid date/time
				arr[16] = 56
				return arr
			}(),
			wantErr: true,
		},
		{
			name: "parse error - non-digit in first 16 bytes",
			// Insert a 'Z' or something invalid among digits
			input: func() [17]byte {
				var arr [17]byte
				copy(arr[:16], []byte("2025Z102030405060")) // invalid char 'Z' at pos 4
				arr[16] = 0                                 // valid offset, but string portion is invalid
				return arr
			}(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnmarshalDateTime(tt.input)
			if tt.wantErr {
				require.Error(t, err, "Expected parse/offset error")
				return
			}

			require.NoError(t, err, "Unexpected error")

			// If we expect zero time
			if tt.want.IsZero() {
				require.True(t, got.IsZero(), "Expected a zero time result")
				return
			}

			// Compare all date/time fields if non-zero
			require.Equal(t, tt.want.Year(), got.Year(), "Year mismatch")
			require.Equal(t, tt.want.Month(), got.Month(), "Month mismatch")
			require.Equal(t, tt.want.Day(), got.Day(), "Day mismatch")
			require.Equal(t, tt.want.Hour(), got.Hour(), "Hour mismatch")
			require.Equal(t, tt.want.Minute(), got.Minute(), "Minute mismatch")
			require.Equal(t, tt.want.Second(), got.Second(), "Second mismatch")

			wantNS := tt.want.Nanosecond()
			gotNS := got.Nanosecond()
			require.Equal(t, wantNS, gotNS, "Nanosecond mismatch")

			// Validate time zone offset by comparing location string offsets
			// We can't compare loc directly if it's created with an empty name
			_, wantOffset := tt.want.Zone()
			_, gotOffset := got.Zone()
			require.Equal(t, wantOffset, gotOffset, "UTC offset mismatch")

			// Additional checks can parse the original byte slice, if desired:
			dtStr := string(tt.input[:16])
			year, _ := strconv.Atoi(dtStr[0:4])
			mon, _ := strconv.Atoi(dtStr[4:6])
			day, _ := strconv.Atoi(dtStr[6:8])
			hour, _ := strconv.Atoi(dtStr[8:10])
			min, _ := strconv.Atoi(dtStr[10:12])
			sec, _ := strconv.Atoi(dtStr[12:14])
			cc, _ := strconv.Atoi(dtStr[14:16])
			// Make sure it lines up with the parsed time
			require.Equal(t, year, got.Year(), "Parsed year mismatch")
			require.Equal(t, mon, int(got.Month()), "Parsed month mismatch")
			require.Equal(t, day, got.Day(), "Parsed day mismatch")
			require.Equal(t, hour, got.Hour(), "Parsed hour mismatch")
			require.Equal(t, min, got.Minute(), "Parsed minute mismatch")
			require.Equal(t, sec, got.Second(), "Parsed second mismatch")
			require.Equal(t, cc, gotNS/10_000_000, "Parsed hundredths mismatch")
		})
	}
}

func TestRecordingDateTime_RoundTripMarshalUnmarshal(t *testing.T) {
	// These times must have years in [1900..2155] and offsets in multiples
	// of 15 minutes to be perfectly reversible in both directions.
	testTimes := []struct {
		name string
		time time.Time
	}{
		{
			name: "Year1900_UTC",
			// 1900-01-01 00:00:00 UTC => valid lower bound
			time: time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "Year2155_UTC",
			// 2155-12-31 23:59:59 UTC => valid upper bound
			time: time.Date(2155, 12, 31, 23, 59, 59, 0, time.UTC),
		},
		{
			name: "TypicalCase_PositiveOffset",
			// 2025-05-10 12:34:56 +8 => offset = 8*3600 = 28800 => offset15=32
			time: time.Date(2025, 5, 10, 12, 34, 56, 0, time.FixedZone("UTC+8", 8*3600)),
		},
		{
			name: "TypicalCase_NegativeOffset",
			// 2001-09-09 01:46:40 -6 => offset = -6*3600 = -21600 => offset15=-24
			time: time.Date(2001, 9, 9, 1, 46, 40, 0, time.FixedZone("UTC-6", -6*3600)),
		},
	}

	for _, tt := range testTimes {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			marshaled, err := MarshalRecordingDateTime(tt.time)
			require.NoError(t, err, "MarshalRecordingDateTime should succeed for valid time range/offset")

			unmarshaled, err := UnmarshalRecordingDateTime(marshaled)
			require.NoError(t, err, "UnmarshalRecordingDateTime should succeed on bytes we just produced")

			// Compare the date/time fields. Because the offset is stored in 15-minute increments,
			// if the original offset wasn't a multiple of 15 minutes, there would be a rounding difference.
			// In these tests, we used only multiples of 15 minutes, so it should match exactly.

			require.Equal(t, tt.time.Year(), unmarshaled.Year(), "Year mismatch after round-trip")
			require.Equal(t, tt.time.Month(), unmarshaled.Month(), "Month mismatch after round-trip")
			require.Equal(t, tt.time.Day(), unmarshaled.Day(), "Day mismatch after round-trip")
			require.Equal(t, tt.time.Hour(), unmarshaled.Hour(), "Hour mismatch after round-trip")
			require.Equal(t, tt.time.Minute(), unmarshaled.Minute(), "Minute mismatch after round-trip")
			require.Equal(t, tt.time.Second(), unmarshaled.Second(), "Second mismatch after round-trip")

			// Compare time zone offsets.
			_, origOff := tt.time.Zone()
			_, newOff := unmarshaled.Zone()
			require.Equal(t, origOff, newOff, "Offset mismatch after round-trip")
		})
	}
}

func TestRecordingDateTime_RoundTripUnmarshalMarshal(t *testing.T) {
	// These byte arrays must represent valid data, or we get partial/broken round-trips.
	// For offset to match, it must be within [-48..52]. Year must be in [1900..2155].
	testBytes := []struct {
		name     string
		input    [7]byte
		expected [7]byte
	}{
		{
			name: "Year1900_Jan01_00_00_00_UTC",
			// 1900 => 1900-1900=0
			// 01 => month
			// 01 => day
			// 00 => hour
			// 00 => minute
			// 00 => second
			// offset=0 => UTC
			input:    [7]byte{0, 1, 1, 0, 0, 0, 0},
			expected: [7]byte{0, 1, 1, 0, 0, 0, 0}, // Should remain identical
		},
		{
			name: "Offset_Positive32",
			// Year=1950 => 1950-1900=50, Month=5, Day=10, Hour=23, Minute=59, Second=59, offset15=32 => +8 hours
			input:    [7]byte{50, 5, 10, 23, 59, 59, 32},
			expected: [7]byte{50, 5, 10, 23, 59, 59, 32},
		},
		{
			name: "Offset_Negative24",
			// Year=2022 => 2022-1900=122, Month=2, Day=2, Hour=6, Minute=0, Second=0, offset15=-24 => -6 hours
			input:    [7]byte{122, 2, 2, 6, 0, 0, 0xE8}, // 0xE8 is -24 in signed 8-bit
			expected: [7]byte{122, 2, 2, 6, 0, 0, 0xE8},
		},
		{
			name: "AllZeros_Unspecified",
			// Unspecified => zero time => re-marshal will fail (year=1 => out of range).
			input:    [7]byte{0, 0, 0, 0, 0, 0, 0},
			expected: [7]byte{0, 0, 0, 0, 0, 0, 0}, // If we re-marshal, it won't succeed. We'll test that logic.
		},
	}

	for _, tt := range testBytes {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tm, err := UnmarshalRecordingDateTime(tt.input)
			require.NoError(t, err, "UnmarshalRecordingDateTime should not fail for valid bytes (or zeros)")

			// If it's all zeros => indefinite time => time.Time{} => year=1 => out of range for re-marshal
			if tm.IsZero() {
				_, errMarsh := MarshalRecordingDateTime(tm)
				require.Error(t, errMarsh, "Expected error re-marshaling zero time (year=1 <1900)")
				return
			}

			marshaled, err := MarshalRecordingDateTime(tm)
			require.NoError(t, err, "MarshalRecordingDateTime must succeed for valid date/time data")

			require.Equal(t, tt.expected, marshaled, "Bytes changed after round-trip Unmarshal→Marshal")
		})
	}
}

func TestMarshalRecordingDateTime(t *testing.T) {
	tests := []struct {
		name      string
		input     time.Time
		wantBytes [7]byte
		wantErr   bool
	}{
		{
			name: "valid date/time (UTC offset=0)",
			// 2025-01-02 03:04:05 UTC => year-1900=125 => offset=0
			input: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
			wantBytes: [7]byte{
				125, // 2025 - 1900 = 125
				1,   // month
				2,   // day
				3,   // hour
				4,   // minute
				5,   // second
				0,   // offset in 15-min increments
			},
			wantErr: false,
		},
		{
			name: "valid date/time (positive offset +8h)",
			// 1950-05-10 23:59:59, offset +8 => offsetSec=8*3600=28800 => offset15=28800/900=32
			input: time.Date(1950, 5, 10, 23, 59, 59, 0, time.FixedZone("UTC+8", 8*3600)),
			wantBytes: [7]byte{
				50, // 1950 - 1900 = 50
				5,  // month
				10, // day
				23, // hour
				59, // minute
				59, // second
				32, // offset
			},
			wantErr: false,
		},
		{
			name: "year lower bound (exactly 1900)",
			// 1900 => year-1900=0
			input: time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC),
			wantBytes: [7]byte{
				0, // year=1900 => 1900-1900=0
				1, // month
				1, // day
				0, // hour
				0, // minute
				0, // second
				0, // offset
			},
			wantErr: false,
		},
		{
			name: "year upper bound (exactly 2155)",
			// 2155 => year-1900=255
			input: time.Date(2155, 12, 31, 23, 59, 59, 0, time.UTC),
			wantBytes: [7]byte{
				255, // year=2155 => 2155-1900=255
				12,  // month
				31,  // day
				23,  // hour
				59,  // minute
				59,  // second
				0,   // offset
			},
			wantErr: false,
		},
		{
			name:    "year below range",
			input:   time.Date(1899, 12, 31, 23, 59, 59, 0, time.UTC),
			wantErr: true,
		},
		{
			name:    "year above range",
			input:   time.Date(2156, 1, 1, 0, 0, 0, 0, time.UTC),
			wantErr: true,
		},
		{
			name:    "offset out of range (+14h => 14*3600=50400 => 50400/900=56 => out of range)",
			input:   time.Date(2000, 6, 15, 12, 0, 0, 0, time.FixedZone("UTC+14", 14*3600)),
			wantErr: true,
		},
		{
			name:    "offset out of range (-14h => -14*3600=-50400 => -50400/900=-56 => out of range)",
			input:   time.Date(2000, 6, 15, 12, 0, 0, 0, time.FixedZone("UTC-14", -14*3600)),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MarshalRecordingDateTime(tt.input)

			if tt.wantErr {
				require.Error(t, err, "Expected error for out-of-range year or offset")
				return
			}

			require.NoError(t, err, "Unexpected error")
			require.Equal(t, tt.wantBytes, got, fmt.Sprintf("Mismatch in returned [7]byte for %s", tt.name))
		})
	}
}

func TestUnmarshalRecordingDateTime(t *testing.T) {
	tests := []struct {
		name     string
		input    [7]byte
		want     time.Time
		wantZero bool // If we expect the zero time.
	}{
		{
			name:     "all fields zero -> unspecified (zero time)",
			input:    [7]byte{0, 0, 0, 0, 0, 0, 0},
			wantZero: true,
		},
		{
			name: "normal date/time with +8 offset",
			// Year = 2025 -> 2025 - 1900 = 125
			// Month = 1, Day = 2, Hour = 3, Minute = 4, Second = 5
			// Offset +8 hours => offsetSec = 28800 => offset15 = 28800/900=32
			input: [7]byte{125, 1, 2, 3, 4, 5, 32},
			want:  time.Date(2025, 1, 2, 3, 4, 5, 0, time.FixedZone("ISO9660", 8*3600)),
		},
		{
			name: "negative offset (-6 hours)",
			// Year = 1950 -> 1950 - 1900 = 50
			// Month=5, Day=10, Hour=23, Minute=59, Second=59
			// offsetSec = -6*3600 = -21600 => offset15= -21600/900= -24 => 8-bit=0xE8
			input: [7]byte{50, 5, 10, 23, 59, 59, 0xE8},
			want:  time.Date(1950, 5, 10, 23, 59, 59, 0, time.FixedZone("ISO9660", -6*3600)),
		},
		{
			name: "boundary year=1900 (offset=0)",
			// year=1900 => 1900-1900=0
			// month=1, day=1, hour=0, minute=0, second=0 => offset=0
			input: [7]byte{0, 1, 1, 0, 0, 0, 0},
			want:  time.Date(1900, 1, 1, 0, 0, 0, 0, time.FixedZone("ISO9660", 0)),
		},
		{
			name: "boundary year=2155 (offset=0)",
			// year=2155 => 2155-1900=255
			// month=12, day=31, hour=23, minute=59, second=59 => offset=0
			input: [7]byte{255, 12, 31, 23, 59, 59, 0},
			want:  time.Date(2155, 12, 31, 23, 59, 59, 0, time.FixedZone("ISO9660", 0)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnmarshalRecordingDateTime(tt.input)
			require.NoError(t, err, "Function does not return errors in current implementation")

			// Check if we expect the zero time
			if tt.wantZero {
				require.True(t, got.IsZero(), "Expected zero time for all-zero input")
				return
			}

			// Compare all date/time fields
			require.Equal(t, tt.want.Year(), got.Year(), "Year mismatch")
			require.Equal(t, tt.want.Month(), got.Month(), "Month mismatch")
			require.Equal(t, tt.want.Day(), got.Day(), "Day mismatch")
			require.Equal(t, tt.want.Hour(), got.Hour(), "Hour mismatch")
			require.Equal(t, tt.want.Minute(), got.Minute(), "Minute mismatch")
			require.Equal(t, tt.want.Second(), got.Second(), "Second mismatch")

			// Compare time zone offsets
			_, wantOff := tt.want.Zone()
			_, gotOff := got.Zone()
			require.Equal(t, wantOff, gotOff, "UTC offset mismatch")
		})
	}
}

func TestMarshalBothByteOrders32(t *testing.T) {
	tests := []struct {
		name   string
		val    uint32
		wantLE []byte
		wantBE []byte
	}{
		{
			name:   "zero",
			val:    0x00000000,
			wantLE: []byte{0x00, 0x00, 0x00, 0x00},
			wantBE: []byte{0x00, 0x00, 0x00, 0x00},
		},
		{
			name:   "simple",
			val:    0x01020304,
			wantLE: []byte{0x04, 0x03, 0x02, 0x01}, // LE of 0x01020304
			wantBE: []byte{0x01, 0x02, 0x03, 0x04}, // BE of 0x01020304
		},
		{
			name:   "random",
			val:    0xAABBCCDD,
			wantLE: []byte{0xDD, 0xCC, 0xBB, 0xAA},
			wantBE: []byte{0xAA, 0xBB, 0xCC, 0xDD},
		},
		{
			name:   "all ones",
			val:    0xFFFFFFFF,
			wantLE: []byte{0xFF, 0xFF, 0xFF, 0xFF},
			wantBE: []byte{0xFF, 0xFF, 0xFF, 0xFF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MarshalBothByteOrders32(tt.val)

			// Verify the output length is 8.
			require.Equal(t, 8, len(got), "Output should be 8 bytes")

			// Verify the first 4 bytes are little-endian.
			le := binary.LittleEndian.Uint32(got[0:4])
			require.Equal(t, tt.val, le, "Little-endian portion mismatch")

			// Verify the last 4 bytes are big-endian.
			be := binary.BigEndian.Uint32(got[4:8])
			require.Equal(t, tt.val, be, "Big-endian portion mismatch")

			// (Optional) If you want to compare exact slices against expected:
			require.Equal(t, tt.wantLE, got[:4], "Expected little-endian bytes")
			require.Equal(t, tt.wantBE, got[4:], "Expected big-endian bytes")
		})
	}
}

func TestUnmarshalUint32LSBMSB(t *testing.T) {
	tests := []struct {
		name    string
		input   [8]byte
		want    uint32
		wantErr bool
	}{
		{
			name:    "zero",
			input:   [8]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			want:    0x00000000,
			wantErr: false,
		},
		{
			name: "simple",
			// 0x01020304 => LE: 04 03 02 01; BE: 01 02 03 04
			input:   [8]byte{0x04, 0x03, 0x02, 0x01, 0x01, 0x02, 0x03, 0x04},
			want:    0x01020304,
			wantErr: false,
		},
		{
			name: "random",
			// 0xAABBCCDD => LE: DD CC BB AA; BE: AA BB CC DD
			input:   [8]byte{0xDD, 0xCC, 0xBB, 0xAA, 0xAA, 0xBB, 0xCC, 0xDD},
			want:    0xAABBCCDD,
			wantErr: false,
		},
		{
			name: "all ones",
			// 0xFFFFFFFF => LE: FF FF FF FF; BE: FF FF FF FF
			input:   [8]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			want:    0xFFFFFFFF,
			wantErr: false,
		},
		{
			name: "mismatch",
			// LE decodes to 0x01020304, BE decodes to 0xA1B2C3D4 (arbitrary mismatch)
			input:   [8]byte{0x04, 0x03, 0x02, 0x01, 0xA1, 0xB2, 0xC3, 0xD4},
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnmarshalUint32LSBMSB(tt.input)

			if tt.wantErr {
				require.Error(t, err, "Expected an error for mismatch case")
				require.Equal(t, uint32(0), got, "Value should be zero on mismatch")
				require.Contains(t, err.Error(), "mismatched both-byte orders")
			} else {
				require.NoError(t, err, fmt.Sprintf("Unexpected error for test: %s", tt.name))
				require.Equal(t, tt.want, got, "Decoded value mismatch")
			}
		})
	}
}

func TestMarshalBothByteOrders16(t *testing.T) {
	tests := []struct {
		name   string
		val    uint16
		wantLE []byte // Expected little-endian representation
		wantBE []byte // Expected big-endian representation
	}{
		{
			name:   "zero",
			val:    0x0000,
			wantLE: []byte{0x00, 0x00},
			wantBE: []byte{0x00, 0x00},
		},
		{
			name:   "simple",
			val:    0x1234,
			wantLE: []byte{0x34, 0x12},
			wantBE: []byte{0x12, 0x34},
		},
		{
			name:   "random",
			val:    0xA1B2,
			wantLE: []byte{0xB2, 0xA1},
			wantBE: []byte{0xA1, 0xB2},
		},
		{
			name:   "all ones",
			val:    0xFFFF,
			wantLE: []byte{0xFF, 0xFF},
			wantBE: []byte{0xFF, 0xFF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MarshalBothByteOrders16(tt.val)

			// Verify the output length is 4 bytes.
			require.Equal(t, 4, len(got), "Output should be 4 bytes")

			// Verify the first 2 bytes are little-endian.
			le := binary.LittleEndian.Uint16(got[:2])
			require.Equal(t, tt.val, le, fmt.Sprintf("Little-endian mismatch for %s", tt.name))

			// Verify the last 2 bytes are big-endian.
			be := binary.BigEndian.Uint16(got[2:])
			require.Equal(t, tt.val, be, fmt.Sprintf("Big-endian mismatch for %s", tt.name))

			// (Optional) Check exact slices if desired:
			require.Equal(t, tt.wantLE, got[:2], "Expected little-endian bytes do not match")
			require.Equal(t, tt.wantBE, got[2:], "Expected big-endian bytes do not match")
		})
	}
}

func TestUnmarshalUint16LSBMSB(t *testing.T) {
	tests := []struct {
		name    string
		input   [4]byte
		want    uint16
		wantErr bool
	}{
		{
			name:    "zero",
			input:   [4]byte{0x00, 0x00, 0x00, 0x00},
			want:    0x0000,
			wantErr: false,
		},
		{
			name: "simple",
			// 0x1234 => LE: 0x34, 0x12; BE: 0x12, 0x34
			input:   [4]byte{0x34, 0x12, 0x12, 0x34},
			want:    0x1234,
			wantErr: false,
		},
		{
			name: "random",
			// 0xA1B2 => LE: 0xB2, 0xA1; BE: 0xA1, 0xB2
			input:   [4]byte{0xB2, 0xA1, 0xA1, 0xB2},
			want:    0xA1B2,
			wantErr: false,
		},
		{
			name: "all ones",
			// 0xFFFF => LE: 0xFF, 0xFF; BE: 0xFF, 0xFF
			input:   [4]byte{0xFF, 0xFF, 0xFF, 0xFF},
			want:    0xFFFF,
			wantErr: false,
		},
		{
			name: "mismatch",
			// Mismatch: LE -> 0x1234, BE -> 0xA1B2
			input:   [4]byte{0x34, 0x12, 0xA1, 0xB2},
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnmarshalUint16LSBMSB(tt.input)

			if tt.wantErr {
				require.Error(t, err, "Expected an error for mismatch case")
				require.Equal(t, uint16(0), got, "Should return zero on mismatch")
				require.Contains(t, err.Error(), "mismatched both-byte orders")
			} else {
				require.NoError(t, err, fmt.Sprintf("Unexpected error for test: %s", tt.name))
				require.Equal(t, tt.want, got, "Decoded value mismatch")
			}
		})
	}
}

func TestDecodeUCS2(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		want      string
		wantEmpty bool // Expect an empty string due to error/invalid input
	}{
		{
			name:  "empty input",
			input: []byte{},
			want:  "",
		},
		{
			name: "simple ASCII",
			// 'H' = 0x48, 'i' = 0x69 in ASCII => big-endian => [0x00,0x48, 0x00,0x69]
			input: []byte{0x00, 0x48, 0x00, 0x69},
			want:  "Hi",
		},
		{
			name: "accented BMP characters",
			// 'é' = U+00E9 => 0x00E9 => big-endian => [0x00, 0xE9]
			// 'ü' = U+00FC => 0x00FC => [0x00, 0xFC]
			// combined => [0x00, 0xE9, 0x00, 0xFC]
			input: []byte{0x00, 0xE9, 0x00, 0xFC},
			want:  "éü",
		},
		{
			name: "supplementary character (surrogate pair)",
			// U+1F600 (GRINNING FACE) => in UTF-16, surrogate pair D83D DE00
			// big-endian => [0xD8,0x3D, 0xDE,0x00]
			input: []byte{0xD8, 0x3D, 0xDE, 0x00},
			want:  "😀",
		},
		{
			name: "odd-length => invalid",
			// decode function returns empty string if len % 2 != 0
			input:     []byte{0x00},
			wantEmpty: true,
		},
		{
			name:  "leading space",
			input: []byte{0x00, 0x20, 0x00, 0x48, 0x00, 0x69, 0x00, 0x20},
			want:  " Hi ",
		},
		{
			name: "37-byte field with padding",
			input: []byte{
				0x00, 0x54, 0x00, 0x65, 0x00, 0x73, 0x00, 0x74, // "Test"
				0x00, 0x20, 0x00, 0x53, 0x00, 0x74, 0x00, 0x72, // " Str"
				0x00, 0x69, 0x00, 0x6E, 0x00, 0x67, 0x00, 0x20, // "ing "
				0x00, 0x57, 0x00, 0x69, 0x00, 0x74, 0x00, 0x68, // "With"
				0x00, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Padding (null bytes)
			},
			want: "Test String With ",
		},
	}

	for _, tt := range tests {
		tt := tt // pin
		t.Run(tt.name, func(t *testing.T) {
			got := DecodeUCS2BigEndian(tt.input)
			if tt.wantEmpty {
				require.Empty(t, got, "Expected empty string due to odd length or invalid input")
				return
			}
			require.Equal(t, tt.want, got, "Decoded string mismatch")
		})
	}
}

func TestEncodeUCS2BigEndian(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []byte
	}{
		{
			name:  "empty string",
			input: "",
			want:  []byte{},
		},
		{
			name:  "simple ASCII",
			input: "Hi",
			// 'H' => 0x0048, 'i' => 0x0069 => big-endian => [0x00,0x48, 0x00,0x69]
			want: []byte{0x00, 0x48, 0x00, 0x69},
		},
		{
			name:  "accented BMP characters",
			input: "éü",
			// 'é' => U+00E9 => 0x00E9 => [0x00, 0xE9]
			// 'ü' => U+00FC => [0x00, 0xFC]
			want: []byte{0x00, 0xE9, 0x00, 0xFC},
		},
		{
			name:  "supplementary character (surrogate pair)",
			input: "😀", // U+1F600 => D83D DE00 in UTF-16 big-endian => [0xD8,0x3D, 0xDE,0x00]
			want:  []byte{0xD8, 0x3D, 0xDE, 0x00},
		},
	}

	for _, tt := range tests {
		tt := tt // pin
		t.Run(tt.name, func(t *testing.T) {
			got := EncodeUCS2BigEndian(tt.input)
			require.Equal(t, tt.want, got, "Encoded byte array mismatch")
		})
	}
}

func TestUCS2RoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "empty string",
			input: "",
		},
		{
			name:  "simple ASCII",
			input: "Hi",
		},
		{
			name:  "accented characters",
			input: "éü",
		},
		{
			name:  "supplementary character",
			input: "😀",
		},
		{
			name:  "mixed text",
			input: "Hello, 世界 😀",
		},
	}

	for _, tt := range tests {
		tt := tt // pin
		t.Run(tt.name, func(t *testing.T) {
			encoded := EncodeUCS2BigEndian(tt.input)
			decoded := DecodeUCS2BigEndian(encoded)
			require.Equal(t, tt.input, decoded, "Round-trip UCS2 mismatch")
		})
	}
}

func TestUCS2InverseRoundTrip(t *testing.T) {
	// This test takes valid big-endian UTF-16 data, decodes it, and then re-encodes
	// to ensure we get the same byte array.
	// Here we provide sample big-endian sequences.
	tests := []struct {
		name  string
		input []byte
	}{
		{
			name:  "empty",
			input: []byte{},
		},
		{
			name: "ASCII",
			// "Go" => G=0x0047 => [0x00,0x47], o=0x006F => [0x00,0x6F]
			input: []byte{0x00, 0x47, 0x00, 0x6F},
		},
		{
			name: "accented BMP",
			// "éü" => [0x00,0xE9, 0x00,0xFC]
			input: []byte{0x00, 0xE9, 0x00, 0xFC},
		},
		{
			name: "supplementary character",
			// "😀" => surrogate pair => [0xD8,0x3D,0xDE,0x00]
			input: []byte{0xD8, 0x3D, 0xDE, 0x00},
		},
		{
			name: "mixed text",
			// "é😀" => U+00E9, U+1F600 =>
			// 'é' => [0x00,0xE9], "😀" => [0xD8,0x3D,0xDE,0x00]
			input: []byte{0x00, 0xE9, 0xD8, 0x3D, 0xDE, 0x00},
		},
	}

	for _, tt := range tests {
		tt := tt // pin
		t.Run(tt.name, func(t *testing.T) {
			decoded := DecodeUCS2BigEndian(tt.input)
			encoded := EncodeUCS2BigEndian(decoded)

			require.Equal(t, tt.input, encoded, "Inverse round-trip mismatch (Decode→Encode did not preserve bytes)")
		})
	}
}
