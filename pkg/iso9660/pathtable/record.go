package pathtable

import (
	"encoding/binary"
	"fmt"
	"github.com/bgrewell/iso-kit/pkg/consts"
	"github.com/bgrewell/iso-kit/pkg/iso9660/info"
	"io"
)

func NewPathTable(reader io.ReaderAt, location uint32, size int, source string, littleEndian bool) (*PathTable, error) {
	data := make([]byte, size)
	_, err := reader.ReadAt(data, int64(location)*consts.ISO9660_SECTOR_SIZE)
	if err != nil {
		return nil, fmt.Errorf("failed to read path table: %w", err)
	}

	pt := &PathTable{
		source:         source,
		littleEndian:   littleEndian,
		ObjectLocation: int64(location),
		ObjectSize:     uint32(size),
	}
	offset := 0

	for offset < len(data) {
		record := &PathTableRecord{
			littleEndian: littleEndian,
		}
		if err := record.Unmarshal(data[offset:], littleEndian); err != nil {
			return nil, err
		}
		pt.Records = append(pt.Records, record)

		// Move to the next record
		recordLen := int(record.LengthOfDirectoryIdentifier) + 8
		if recordLen%2 != 0 {
			recordLen++ // Handle padding byte
		}
		offset += recordLen
	}

	return pt, nil
}

// NewEmptyPathTable creates an empty path table for building an image in
// memory. Records are added with AddRecord; the table's byte location is
// assigned later by the layout engine via SetLocation.
func NewEmptyPathTable(source string, littleEndian bool) *PathTable {
	return &PathTable{
		source:       source,
		littleEndian: littleEndian,
	}
}

// AddRecord appends a path table record. Records must be added in the
// order required by ECMA-119: breadth-first, parents before children,
// siblings ordered by directory identifier.
func (pt *PathTable) AddRecord(directoryIdentifier string, locationOfExtent uint32, parentDirectoryNumber uint16) {
	pt.Records = append(pt.Records, &PathTableRecord{
		LengthOfDirectoryIdentifier: uint8(len(directoryIdentifier)),
		LocationOfExtent:            locationOfExtent,
		ParentDirectoryNumber:       parentDirectoryNumber,
		DirectoryIdentifier:         directoryIdentifier,
		littleEndian:                pt.littleEndian,
	})
}

// SetLocation assigns the table's on-image sector location and its size in
// bytes, used by the layout engine after sector assignment.
func (pt *PathTable) SetLocation(sector uint32, size uint32) {
	pt.ObjectLocation = int64(sector)
	pt.ObjectSize = size
}

// PathTable represents a full path table, containing multiple records.
type PathTable struct {
	Records      []*PathTableRecord
	source       string
	littleEndian bool
	// --- Fields that are not part of the ISO9660 object ---
	// Object Location (in bytes)
	ObjectLocation int64 `json:"object_location"`
	// Object Size (in bytes)
	ObjectSize uint32 `json:"object_size"`
}

func (pt *PathTable) Type() string {
	return "Path Table"
}

func (pt *PathTable) Name() string {
	encoding := "Big-Endian"
	if pt.littleEndian {
		encoding = "Little-Endian"
	}
	return fmt.Sprintf("%s Path Table (%s)", encoding, pt.source)
}

func (pt *PathTable) Description() string {
	return ""
}

func (pt *PathTable) Properties() map[string]interface{} {
	encoding := "big-endian"
	if pt.littleEndian {
		encoding = "little-endian"
	}

	return map[string]interface{}{
		"Records":  len(pt.Records),
		"Source":   pt.source,
		"Encoding": encoding,
	}
}

func (pt *PathTable) Offset() int64 {
	return pt.ObjectLocation * consts.ISO9660_SECTOR_SIZE
}

func (pt *PathTable) Size() int {
	return int(pt.ObjectSize)
}

func (pt *PathTable) GetObjects() []info.ImageObject {
	objects := []info.ImageObject{pt}
	//TODO: Fix this
	//for _, record := range pt.Records {
	//	objects = append(objects, record.GetObjects()...)
	//}
	return objects
}

// Marshal converts a PathTable into a contiguous byte array.
func (pt *PathTable) Marshal() ([]byte, error) {
	var buf []byte

	for _, record := range pt.Records {
		recBytes, err := record.Marshal()
		if err != nil {
			return nil, err
		}
		buf = append(buf, recBytes...)
	}

	return buf, nil
}

type PathTableRecord struct {
	// Length of Directory Identifier specifies the length in bytes of the Directory Identifier field of the Path Table
	// Record.
	LengthOfDirectoryIdentifier uint8 `json:"length_of_directory_identifier"`
	// Extended Attribute Record Length specifies the Extended Attribute Record length if an Extended Attribute Record
	// is recorded. Otherwise, this number will be zero.
	ExtendedAttributeRecordLength uint8 `json:"extended_atrribute_record_length"`
	// Location of Extent specifies the Logical Block Number of the first Logical Block allocated to the Extent in which
	// the directory is recorded.
	LocationOfExtent uint32 `json:"location_of_extent"`
	// Parent Directory Number specifies the record number in the Path Table for the parent directory of the directory.
	ParentDirectoryNumber uint16 `json:"parent_directory_number"`
	// Directory Identifier specifies the identification for the directory. The characters in this field shall be
	// d-characters or d1-characters or only a null byte 0x00.
	DirectoryIdentifier string `json:"directory_identifier"`
	// Padding Field is only present in the Path Table Record if the number in the Length Of Directory Identifier field
	// is an odd number. If present, this field shall be set to a null byte 0x00.
	// Note: The padding field isn't actually represented in this struct since it's presence or absence is simply
	// calculated when marshalling to an array of bytes. When unmarshalling if the LengthOfFileIdentifier field is even
	// then we make sure we skip the padding byte when we continue processing the following fields.
	// Padding *byte `json:"padding" ----------
	littleEndian bool
	// --- Fields that are not part of the ISO9660 object ---
	// Object Location (in bytes)
	ObjectLocation int64 `json:"object_location"`
	// Object Size (in bytes)
	ObjectSize uint32 `json:"object_size"`
}

func (ptr *PathTableRecord) Type() string {
	return "Path Table Record"
}

func (ptr *PathTableRecord) Name() string {
	return ptr.DirectoryIdentifier
}

func (ptr *PathTableRecord) Description() string {
	return ""
}

func (ptr *PathTableRecord) Properties() map[string]interface{} {
	return map[string]interface{}{
		"LocationOfExtent":      ptr.LocationOfExtent,
		"ParentDirectoryNumber": ptr.ParentDirectoryNumber,
	}
}

func (ptr *PathTableRecord) Offset() int64 {
	return ptr.ObjectLocation
}

func (ptr *PathTableRecord) Size() int {
	return int(ptr.ObjectSize)
}

func (ptr *PathTableRecord) GetObjects() []info.ImageObject {
	return []info.ImageObject{ptr}
}

// Marshal converts a single PathTableRecord into a byte slice.
func (ptr *PathTableRecord) Marshal() ([]byte, error) {
	dirIDBytes := []byte(ptr.DirectoryIdentifier)
	ptr.LengthOfDirectoryIdentifier = uint8(len(dirIDBytes))

	recordLen := 8 + len(dirIDBytes)
	if len(dirIDBytes)%2 != 0 {
		recordLen++
	}

	buf := make([]byte, recordLen)
	offset := 0

	buf[offset] = ptr.LengthOfDirectoryIdentifier
	offset++
	buf[offset] = ptr.ExtendedAttributeRecordLength
	offset++

	if ptr.littleEndian {
		binary.LittleEndian.PutUint32(buf[offset:], ptr.LocationOfExtent)
	} else {
		binary.BigEndian.PutUint32(buf[offset:], ptr.LocationOfExtent)
	}
	offset += 4

	if ptr.littleEndian {
		binary.LittleEndian.PutUint16(buf[offset:], ptr.ParentDirectoryNumber)
	} else {
		binary.BigEndian.PutUint16(buf[offset:], ptr.ParentDirectoryNumber)
	}
	offset += 2

	copy(buf[offset:], dirIDBytes)
	offset += len(dirIDBytes)

	if len(dirIDBytes)%2 != 0 {
		buf[offset] = 0x00 // Padding byte
	}

	return buf, nil
}

// Unmarshal decodes a single PathTableRecord from a byte slice.
func (ptr *PathTableRecord) Unmarshal(data []byte, littleEndian bool) error {
	if len(data) < 8 {
		return fmt.Errorf("data too short to contain a PathTableRecord")
	}
	offset := 0

	ptr.LengthOfDirectoryIdentifier = data[offset]
	offset++
	ptr.ExtendedAttributeRecordLength = data[offset]
	offset++

	if littleEndian {
		ptr.LocationOfExtent = binary.LittleEndian.Uint32(data[offset:])
	} else {
		ptr.LocationOfExtent = binary.BigEndian.Uint32(data[offset:])
	}
	offset += 4

	if littleEndian {
		ptr.ParentDirectoryNumber = binary.LittleEndian.Uint16(data[offset:])
	} else {
		ptr.ParentDirectoryNumber = binary.BigEndian.Uint16(data[offset:])
	}
	offset += 2

	n := int(ptr.LengthOfDirectoryIdentifier)
	if len(data) < offset+n {
		return fmt.Errorf("data too short for DirectoryIdentifier")
	}
	ptr.DirectoryIdentifier = string(data[offset : offset+n])
	offset += n

	if n%2 != 0 {
		offset++ // Skip padding byte
	}

	return nil
}
