package udf

import (
	"encoding/binary"
	"fmt"
	"time"
)

// ECMA-167 / UDF structure layouts, read-path scope.

const (
	// UDF sector (logical block) size used by essentially all UDF media.
	SectorSize = 2048

	// The Volume Recognition Sequence starts at byte offset 32768
	// (sector 16) per ECMA-167 part 2.
	vrsStartSector = 16

	// The Anchor Volume Descriptor Pointer is at sector 256 (with
	// backups at N-256 and N-1).
	anchorSector = 256

	// Descriptor tag identifiers (ECMA-167 part 3 & 4).
	TAG_PRIMARY_VOLUME_DESCRIPTOR     = 1
	TAG_ANCHOR_VOLUME_DESCRIPTOR_PTR  = 2
	TAG_VOLUME_DESCRIPTOR_PTR         = 3
	TAG_IMPLEMENTATION_USE_DESCRIPTOR = 4
	TAG_PARTITION_DESCRIPTOR          = 5
	TAG_LOGICAL_VOLUME_DESCRIPTOR     = 6
	TAG_UNALLOCATED_SPACE_DESCRIPTOR  = 7
	TAG_TERMINATING_DESCRIPTOR        = 8
	TAG_FILE_SET_DESCRIPTOR           = 256
	TAG_FILE_IDENTIFIER_DESCRIPTOR    = 257
	TAG_FILE_ENTRY                    = 261
	TAG_EXTENDED_FILE_ENTRY           = 266

	// ICB file types.
	FILE_TYPE_DIRECTORY = 4
	FILE_TYPE_REGULAR   = 5
	FILE_TYPE_SYMLINK   = 12

	// File identifier characteristics bits.
	FID_CHAR_HIDDEN    = 0x01
	FID_CHAR_DIRECTORY = 0x02
	FID_CHAR_DELETED   = 0x04
	FID_CHAR_PARENT    = 0x08
)

// Tag is the 16-byte descriptor tag prefixing every ECMA-167 descriptor.
type Tag struct {
	ID       uint16
	Version  uint16
	Location uint32
}

// parseTag decodes and verifies a descriptor tag. The checksum byte must
// match the sum of the other 15 tag bytes.
func parseTag(data []byte) (Tag, error) {
	if len(data) < 16 {
		return Tag{}, fmt.Errorf("descriptor tag: data too short")
	}
	var sum byte
	for i := 0; i < 16; i++ {
		if i == 4 {
			continue
		}
		sum += data[i]
	}
	if sum != data[4] {
		return Tag{}, fmt.Errorf("descriptor tag: checksum mismatch")
	}
	return Tag{
		ID:       binary.LittleEndian.Uint16(data[0:2]),
		Version:  binary.LittleEndian.Uint16(data[2:4]),
		Location: binary.LittleEndian.Uint32(data[12:16]),
	}, nil
}

// ExtentAD is an extent_ad: a length in bytes and an absolute sector.
type ExtentAD struct {
	Length   uint32
	Location uint32
}

func parseExtentAD(data []byte) ExtentAD {
	return ExtentAD{
		Length:   binary.LittleEndian.Uint32(data[0:4]),
		Location: binary.LittleEndian.Uint32(data[4:8]),
	}
}

// LongAD is a long_ad: an extent length plus a logical block address
// within a numbered partition.
type LongAD struct {
	Length       uint32
	LogicalBlock uint32
	Partition    uint16
}

func parseLongAD(data []byte) LongAD {
	return LongAD{
		Length:       binary.LittleEndian.Uint32(data[0:4]) & 0x3FFFFFFF,
		LogicalBlock: binary.LittleEndian.Uint32(data[4:8]),
		Partition:    binary.LittleEndian.Uint16(data[8:10]),
	}
}

// decodeDString decodes an OSTA compressed unicode dstring of the given
// used length: the first byte is the compression ID (8 for latin-1, 16
// for UCS-2 big-endian).
func decodeDString(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	comp := data[0]
	content := data[1:]
	switch comp {
	case 8:
		runes := make([]rune, len(content))
		for i, b := range content {
			runes[i] = rune(b)
		}
		return string(runes)
	case 16:
		runes := make([]rune, 0, len(content)/2)
		for i := 0; i+1 < len(content); i += 2 {
			runes = append(runes, rune(uint16(content[i])<<8|uint16(content[i+1])))
		}
		return string(runes)
	default:
		return ""
	}
}

// decodeFixedDString decodes a fixed-size dstring field whose final byte
// holds the used length (including the compression ID byte).
func decodeFixedDString(field []byte) string {
	if len(field) == 0 {
		return ""
	}
	used := int(field[len(field)-1])
	if used == 0 || used > len(field)-1 {
		return ""
	}
	return decodeDString(field[:used])
}

// parseTimestamp decodes a 12-byte ECMA-167 timestamp.
func parseTimestamp(data []byte) time.Time {
	if len(data) < 12 {
		return time.Time{}
	}
	typeAndTZ := binary.LittleEndian.Uint16(data[0:2])
	year := int(int16(binary.LittleEndian.Uint16(data[2:4])))
	month := int(data[4])
	day := int(data[5])
	hour := int(data[6])
	minute := int(data[7])
	second := int(data[8])
	if year == 0 || month == 0 || day == 0 {
		return time.Time{}
	}

	loc := time.UTC
	// Bits 0-11 hold the timezone offset in minutes (two's complement);
	// -2047 means unspecified.
	if tz := int16(typeAndTZ<<4) >> 4; tz != -2047 && tz != 0 {
		loc = time.FixedZone("", int(tz)*60)
	}
	return time.Date(year, time.Month(month), day, hour, minute, second, 0, loc)
}

// AnchorVolumeDescriptorPointer locates the main and reserve volume
// descriptor sequences.
type AnchorVolumeDescriptorPointer struct {
	MainVDS    ExtentAD
	ReserveVDS ExtentAD
}

func parseAVDP(data []byte) (*AnchorVolumeDescriptorPointer, error) {
	tag, err := parseTag(data)
	if err != nil {
		return nil, err
	}
	if tag.ID != TAG_ANCHOR_VOLUME_DESCRIPTOR_PTR {
		return nil, fmt.Errorf("expected anchor volume descriptor pointer (tag 2), got tag %d", tag.ID)
	}
	return &AnchorVolumeDescriptorPointer{
		MainVDS:    parseExtentAD(data[16:24]),
		ReserveVDS: parseExtentAD(data[24:32]),
	}, nil
}

// PrimaryVolumeDescriptor carries the volume identity (ECMA-167 3/10.1).
type PrimaryVolumeDescriptor struct {
	VolumeIdentifier    string
	VolumeSetIdentifier string
	RecordingDateTime   time.Time
}

func parsePVD(data []byte) *PrimaryVolumeDescriptor {
	return &PrimaryVolumeDescriptor{
		VolumeIdentifier:    decodeFixedDString(data[24:56]),
		VolumeSetIdentifier: decodeFixedDString(data[72:200]),
		RecordingDateTime:   parseTimestamp(data[376:388]),
	}
}

// PartitionDescriptor maps a partition number to an absolute extent
// (ECMA-167 3/10.5).
type PartitionDescriptor struct {
	PartitionNumber uint16
	StartingSector  uint32
	Length          uint32
}

func parsePD(data []byte) *PartitionDescriptor {
	return &PartitionDescriptor{
		PartitionNumber: binary.LittleEndian.Uint16(data[22:24]),
		StartingSector:  binary.LittleEndian.Uint32(data[188:192]),
		Length:          binary.LittleEndian.Uint32(data[192:196]),
	}
}

// LogicalVolumeDescriptor carries the logical volume identity, block
// size, and the file set descriptor location (ECMA-167 3/10.6).
type LogicalVolumeDescriptor struct {
	LogicalVolumeIdentifier string
	LogicalBlockSize        uint32
	FileSetLocation         LongAD
}

func parseLVD(data []byte) *LogicalVolumeDescriptor {
	return &LogicalVolumeDescriptor{
		LogicalVolumeIdentifier: decodeFixedDString(data[84:212]),
		LogicalBlockSize:        binary.LittleEndian.Uint32(data[212:216]),
		FileSetLocation:         parseLongAD(data[248:264]),
	}
}

// FileSetDescriptor points at the root directory ICB (ECMA-167 4/14.1).
type FileSetDescriptor struct {
	FileSetIdentifier string
	RootDirectoryICB  LongAD
}

func parseFSD(data []byte) (*FileSetDescriptor, error) {
	tag, err := parseTag(data)
	if err != nil {
		return nil, err
	}
	if tag.ID != TAG_FILE_SET_DESCRIPTOR {
		return nil, fmt.Errorf("expected file set descriptor (tag 256), got tag %d", tag.ID)
	}
	return &FileSetDescriptor{
		FileSetIdentifier: decodeFixedDString(data[304:336]),
		RootDirectoryICB:  parseLongAD(data[400:416]),
	}, nil
}

// FileEntry is the parsed form of a File Entry or Extended File Entry
// ICB (ECMA-167 4/14.9, 4/14.17).
type FileEntry struct {
	FileType          byte
	UID               uint32
	GID               uint32
	Permissions       uint32
	InformationLength uint64
	ModificationTime  time.Time
	// AllocationType is icbTag flags & 7: 0 short_ad, 1 long_ad,
	// 3 inline data.
	AllocationType byte
	// AllocationData is the raw allocation descriptor area (or inline
	// file data when AllocationType is 3).
	AllocationData []byte
	// Extended reports whether this was an Extended File Entry.
	Extended bool
}

func parseFileEntry(data []byte) (*FileEntry, error) {
	tag, err := parseTag(data)
	if err != nil {
		return nil, err
	}
	if tag.ID != TAG_FILE_ENTRY && tag.ID != TAG_EXTENDED_FILE_ENTRY {
		return nil, fmt.Errorf("expected file entry (tag 261/266), got tag %d", tag.ID)
	}
	extended := tag.ID == TAG_EXTENDED_FILE_ENTRY

	fe := &FileEntry{
		FileType:       data[16+11],
		UID:            binary.LittleEndian.Uint32(data[36:40]),
		GID:            binary.LittleEndian.Uint32(data[40:44]),
		Permissions:    binary.LittleEndian.Uint32(data[44:48]),
		AllocationType: data[16+18] & 0x07,
		Extended:       extended,
	}
	fe.InformationLength = binary.LittleEndian.Uint64(data[56:64])

	var eaLenOff, adLenOff, dataOff int
	if extended {
		fe.ModificationTime = parseTimestamp(data[92:104])
		eaLenOff, adLenOff, dataOff = 208, 212, 216
	} else {
		fe.ModificationTime = parseTimestamp(data[84:96])
		eaLenOff, adLenOff, dataOff = 168, 172, 176
	}

	eaLen := int(binary.LittleEndian.Uint32(data[eaLenOff : eaLenOff+4]))
	adLen := int(binary.LittleEndian.Uint32(data[adLenOff : adLenOff+4]))
	start := dataOff + eaLen
	if start+adLen > len(data) {
		return nil, fmt.Errorf("file entry allocation descriptors exceed descriptor size")
	}
	fe.AllocationData = data[start : start+adLen]
	return fe, nil
}

// FileIdentifier is one directory entry (ECMA-167 4/14.4).
type FileIdentifier struct {
	Characteristics byte
	Name            string
	ICB             LongAD
}

// IsDirectory reports whether the entry names a directory.
func (f *FileIdentifier) IsDirectory() bool { return f.Characteristics&FID_CHAR_DIRECTORY != 0 }

// IsParent reports whether the entry is the parent-directory link.
func (f *FileIdentifier) IsParent() bool { return f.Characteristics&FID_CHAR_PARENT != 0 }

// IsDeleted reports whether the entry is marked deleted.
func (f *FileIdentifier) IsDeleted() bool { return f.Characteristics&FID_CHAR_DELETED != 0 }

// parseFileIdentifiers walks a directory's content bytes, decoding each
// File Identifier Descriptor. Each FID is padded to a 4-byte boundary.
func parseFileIdentifiers(data []byte) ([]*FileIdentifier, error) {
	var out []*FileIdentifier
	offset := 0
	for offset+38 <= len(data) {
		tag, err := parseTag(data[offset:])
		if err != nil {
			// Zero padding after the last entry.
			break
		}
		if tag.ID != TAG_FILE_IDENTIFIER_DESCRIPTOR {
			break
		}
		fidLen := int(data[offset+19])
		iuLen := int(binary.LittleEndian.Uint16(data[offset+36 : offset+38]))
		total := 38 + iuLen + fidLen
		total = (total + 3) / 4 * 4
		if offset+total > len(data) {
			return nil, fmt.Errorf("file identifier descriptor exceeds directory data")
		}

		nameStart := offset + 38 + iuLen
		out = append(out, &FileIdentifier{
			Characteristics: data[offset+18],
			Name:            decodeDString(data[nameStart : nameStart+fidLen]),
			ICB:             parseLongAD(data[offset+20 : offset+36]),
		})
		offset += total
	}
	return out, nil
}

// shortAD is a short_ad allocation descriptor: 30-bit length plus a
// logical block within the same partition.
type shortAD struct {
	Length       uint32
	LogicalBlock uint32
}

func parseShortADs(data []byte) []shortAD {
	var out []shortAD
	for off := 0; off+8 <= len(data); off += 8 {
		length := binary.LittleEndian.Uint32(data[off : off+4])
		if length&0x3FFFFFFF == 0 {
			break
		}
		// Top two bits: 0 = recorded and allocated; anything else is not
		// readable content.
		if length>>30 != 0 {
			continue
		}
		out = append(out, shortAD{
			Length:       length & 0x3FFFFFFF,
			LogicalBlock: binary.LittleEndian.Uint32(data[off+4 : off+8]),
		})
	}
	return out
}

func parseLongADs(data []byte) []LongAD {
	var out []LongAD
	for off := 0; off+16 <= len(data); off += 16 {
		raw := binary.LittleEndian.Uint32(data[off : off+4])
		if raw&0x3FFFFFFF == 0 {
			break
		}
		if raw>>30 != 0 {
			continue
		}
		out = append(out, parseLongAD(data[off:off+16]))
	}
	return out
}
