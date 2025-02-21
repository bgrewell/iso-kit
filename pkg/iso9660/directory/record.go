package directory

import (
	"fmt"
	"github.com/bgrewell/iso-kit/pkg/iso9660/encoding"
	"github.com/bgrewell/iso-kit/pkg/iso9660/extensions"
	"github.com/bgrewell/iso-kit/pkg/logging"
	"os"
	"time"
)

type DirectoryRecord struct {
	// Length Of Directory Record specifies the length of the directory record in bytes.
	LengthOfDirectoryRecord uint8 `json:"length_of_directory_record"`
	// Extended Attribute Record Length specifies the assigned Extended Attribute Record length if an Extended Attribute
	// Record is recorded, otherwise it will be zero. If this is non-zero then the Extended Attribute Record will need
	// to be read from the extent before the file data
	ExtendedAttributeRecordLength uint8 `json:"extended_attribute_record_length"`
	// Location of Extent specifies the Logical Block Number of the first Logical Block allocated to the Extent.
	//  | Encoding: BothByteOrder
	LocationOfExtent uint32 `json:"location_of_extent"`
	// Data Length specifies the data length of the File Section.
	//  | Encoding: BothByteOrder
	DataLength uint32 `json:"data_length"`
	// Recording Date and Time specifies the date and time of the day at which the information in the Extent described
	// by the Directory Record was recorded.
	//  | Encoding: 7-byte time format
	RecordingDateAndTime time.Time `json:"recording_date_and_time"`
	// File Flags is an 8-bit field that records flags related to the Directory Record. The following are the flags, LSB
	// first. Bit-flag Positions:
	//  0 - Existence: 0 means that the existence of the file shall be made known to the user upon an inquiry by the
	//                 user. 1 means that the existence of the file need not be made known to the user.
	//  1 - Directory: 0 means the Directory Record identifies a File, 1 means that the Directory Record identifies a
	//                 directory.
	//  2 - Associated File: 0 means that the file is not an Associated File. 1 means that the file is an Associated
	//                       File.
	//  3 - Record: 0 means that the structure of the information in the file is not specified by the Record Format
	//              field of any associated Extended Attribute Record. 1 means that the structure of the information in
	//              the file has a record format specified by a number other than zero in the Record Format Field of the
	//              Extended Attribute Record. (see section 9.5.8 of ECMA-119 4th Edition for more details)
	//  4 - Protection: 0 means that an owner and group identification are not specified for the file and that any user
	//                  may read or execute the file. 1 means that an owner identification and group are specified for
	//                  the file and at least one of the even-numbered bits or bit 0 in the permissions field of the
	//                  associated Extended Attribute Record is set to 1.
	//  5 & 6: Reserved
	//  7 - Multi-Extent: 0 means that this is the final Directory Record for the file. 1 means that this is not the
	//                    final Directory Record for the file.
	FileFlags FileFlags `json:"file_flags"`
	// File Unit Size specifies the assigned File Unit size for the File Section if the File Section is recorded in
	// interleaved mode. Otherwise, this number shall be zero.
	FileUnitSize uint8 `json:"file_unit_size"`
	// Interleave Gap Size specifies the assigned Interleave Gap size for the File Section if the File Section is
	// recorded in interleaved mode. Otherwise, this number shall be zero.
	InterleaveGapSize uint8 `json:"interleave_gap_size"`
	// Volume Sequence Number specifies the ordinal number of the volume in the Volume Set on which the Extent described
	// by this Directory Record is recorded.
	//  | Encoding: BothByteOrder
	VolumeSequenceNumber uint16 `json:"volume_sequence_number"`
	// Length of File Identifier specifies the length in bytes of the File Identifier field of the Directory Record.
	LengthOfFileIdentifier uint8 `json:"length_of_file_identifier"`
	// File Identifier interpretation depends on the setting of the Directory bit of the File Flags field. If set to 0
	// then it means this field identifies a file and the field shall be d-characters or d1-characters, SEPARATOR_1 or
	// SEPARATOR_2. If the Directory bit is set to 1 then this field identifies a directory the characters in this field
	// should be d-characters or d1-characters or only a 0x00 byte or only a 0x01 byte.
	FileIdentifier string `json:"file_identifier"`
	// Padding Field adds a null byte pad 0x00 to the end of a File Identifier if the LengthOfFileIdentifier field is
	// even.
	// Note: The padding field isn't actually represented in this struct since it's presence or absence is simply
	// calculated when marshalling to an array of bytes. When unmarshalling if the LengthOfFileIdentifier field is even
	// then we make sure we skip the padding byte when we continue processing the following fields.
	// Padding *byte `json:"padding" ----------
	// System Use is an optional field that if present shall be reserved for system use. It's contents are not specified
	// by the EMCA-119/ISO9660 standard. If needed a null byte 0x00 will be added to this field to ensure that the
	// Directory Record comprises an even number of bytes. We must be careful here when unmarshalling to make sure that
	// a copy of the byte slice is made and stored otherwise we can get bad data if reading this field later if the
	// original buffer was reused in any way (i.e. if there is a loop processing records and the buffer is reused for
	// each iteration through the loop). This problem is seen in other libraries that do this type of work due to the
	// lack of understanding of the differences between slices and arrays where Go passes slices by reference and
	// arrays by value.
	// This field shall be at BP (LEN_DR - LEN_SU + 1) to LEN_DR
	SystemUse []byte `json:"system_use"`
	// RockRidge is a field to store Rock Ridge extensions if they exist
	RockRidge *extensions.RockRidgeExtensions `json:"rock_ridge"`
	// Joliet is a field to store if this record is from a volume with Joliet extensions
	Joliet bool `json:"joliet"`
	// Logger
	Logger *logging.Logger
}

// IsDirectory checks if the entry is a Directory
func (dr *DirectoryRecord) IsDirectory() bool {
	if dr.RockRidge != nil && dr.RockRidge.HasRockRidge() && dr.RockRidge.Permissions != nil {
		return dr.RockRidge.Permissions.IsDir()
	}
	return dr.FileFlags.Directory
}

// IsSpecial checks for "." or ".."
func (dr *DirectoryRecord) IsSpecial() bool {
	return dr.FileIdentifier == "\x00" || dr.FileIdentifier == "\x01"
}

// GetBestName retrieves the best file name
func (dr *DirectoryRecord) GetBestName(RockRidgeEnabled bool) string {

	// Handle special cases 'root' or 'parent'
	if dr.IsSpecial() {
		if dr.FileIdentifier == "\x00" {
			return "."
		}
		return ".."
	}

	if RockRidgeEnabled && dr.RockRidge != nil && dr.RockRidge.AlternateName != nil {
		return *dr.RockRidge.AlternateName
	}
	return dr.FileIdentifier // Default to ISO 9660 name
}

// GetPermissions retrieves file permissions
func (dr *DirectoryRecord) GetPermissions(RockRidgeEnabled bool) os.FileMode {
	if RockRidgeEnabled && dr.RockRidge != nil && dr.RockRidge.Permissions != nil {
		return os.FileMode(*dr.RockRidge.Permissions)
	}
	if dr.IsDirectory() {
		return os.FileMode(0o755) // Default for directories
	}
	return os.FileMode(0o644) // Default for files
}

// GetOwnership retrieves UID & GID
func (dr *DirectoryRecord) GetOwnership(RockRidgeEnabled bool) (uid, gid *uint32) {
	if RockRidgeEnabled && dr.RockRidge != nil {
		return dr.RockRidge.UID, dr.RockRidge.GID
	}
	return nil, nil
}

// GetTimestamps retrieves creation & modification time
func (dr *DirectoryRecord) GetTimestamps(RockRidgeEnabled bool) (creation, modification time.Time) {
	if RockRidgeEnabled && dr.RockRidge != nil {
		if dr.RockRidge.CreationTime != nil {
			creation = *dr.RockRidge.CreationTime
		}
		if dr.RockRidge.ModificationTime != nil {
			modification = *dr.RockRidge.ModificationTime
		}
	} else {
		creation = dr.RecordingDateAndTime
		modification = dr.RecordingDateAndTime
	}
	return creation, modification
}

// Marshal converts the DirectoryRecord into its on‑disk byte representation.
// It computes and sets the LengthOfDirectoryRecord field and handles the optional
// padding byte for the File Identifier.
func (dr *DirectoryRecord) Marshal() ([]byte, error) {
	var buf []byte

	// Reserve a byte for LengthOfDirectoryRecord; we'll set it at the end.
	buf = append(buf, 0)

	// Extended Attribute Record Length (1 byte)
	buf = append(buf, dr.ExtendedAttributeRecordLength)

	// Location Of Extent: 8 bytes (both-byte orders for uint32)
	locBytes := encoding.MarshalBothByteOrders32(dr.LocationOfExtent)
	buf = append(buf, locBytes[:]...)

	// Data Length: 8 bytes (both-byte orders for uint32)
	dataLenBytes := encoding.MarshalBothByteOrders32(dr.DataLength)
	buf = append(buf, dataLenBytes[:]...)

	// Recording Date and Time: 7 bytes
	recTimeBytes, err := encoding.MarshalRecordingDateTime(dr.RecordingDateAndTime)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal RecordingDateAndTime: %w", err)
	}
	buf = append(buf, recTimeBytes[:]...)

	// File Flags: 1 byte
	buf = append(buf, dr.FileFlags.Marshal())

	// File Unit Size: 1 byte
	buf = append(buf, dr.FileUnitSize)

	// Interleave Gap Size: 1 byte
	buf = append(buf, dr.InterleaveGapSize)

	// Volume Sequence Number: 4 bytes (both-byte orders for uint16)
	volSeqBytes := encoding.MarshalBothByteOrders16(dr.VolumeSequenceNumber)
	buf = append(buf, volSeqBytes[:]...)

	// File Identifier:
	// First, the Length of File Identifier (1 byte)
	fileIDBytes := []byte(dr.FileIdentifier)
	fiLen := uint8(len(fileIDBytes))
	buf = append(buf, fiLen)

	// Then, the File Identifier itself.
	buf = append(buf, fileIDBytes...)

	// Padding Field: present if the File Identifier length is even.
	if fiLen%2 == 0 {
		buf = append(buf, 0x00)
	}

	// System Use: the remainder of the record.
	buf = append(buf, dr.SystemUse...)

	// Now that we know the total length, set the LengthOfDirectoryRecord.
	recordLength := uint8(len(buf))
	if recordLength == 0 {
		return nil, fmt.Errorf("record length is zero")
	}
	buf[0] = recordLength

	// (Optional) You might want to store recordLength into dr.LengthOfDirectoryRecord.
	dr.LengthOfDirectoryRecord = recordLength

	return buf, nil
}

// Unmarshal decodes a DirectoryRecord from the provided byte slice.
// It expects that data contains at least LengthOfDirectoryRecord bytes.
// It also handles skipping the optional Padding Field if the File Identifier length is even.
func (dr *DirectoryRecord) Unmarshal(data []byte) error {
	if len(data) < 1 {
		return fmt.Errorf("data too short to contain a DirectoryRecord")
	}
	offset := 0

	// LengthOfDirectoryRecord: 1 byte.
	recordLength := data[offset]
	dr.LengthOfDirectoryRecord = recordLength
	if len(data) < int(recordLength) {
		return fmt.Errorf("data length %d is less than expected record length %d", len(data), recordLength)
	}
	offset++

	// Extended Attribute Record Length: 1 byte.
	dr.ExtendedAttributeRecordLength = data[offset]
	offset++

	// Location Of Extent: 8 bytes.
	if offset+8 > int(recordLength) {
		return fmt.Errorf("insufficient data for Location Of Extent")
	}
	var locBytes [8]byte
	copy(locBytes[:], data[offset:offset+8])
	loc, err := encoding.UnmarshalUint32LSBMSB(locBytes)
	if err != nil {
		return fmt.Errorf("failed to unmarshal Location Of Extent: %w", err)
	}
	dr.LocationOfExtent = loc
	offset += 8

	// Data Length: 8 bytes.
	if offset+8 > int(recordLength) {
		return fmt.Errorf("insufficient data for Data Length")
	}
	var dataLenBytes [8]byte
	copy(dataLenBytes[:], data[offset:offset+8])
	dataLen, err := encoding.UnmarshalUint32LSBMSB(dataLenBytes)
	if err != nil {
		return fmt.Errorf("failed to unmarshal Data Length: %w", err)
	}
	dr.DataLength = dataLen
	offset += 8

	// Recording Date and Time: 7 bytes.
	if offset+7 > int(recordLength) {
		return fmt.Errorf("insufficient data for Recording Date and Time")
	}
	var recTimeBytes [7]byte
	copy(recTimeBytes[:], data[offset:offset+7])
	recTime, err := encoding.UnmarshalRecordingDateTime(recTimeBytes)
	if err != nil {
		return fmt.Errorf("failed to unmarshal Recording Date and Time: %w", err)
	}
	dr.RecordingDateAndTime = recTime
	offset += 7

	// File Flags: 1 byte.
	if offset+1 > int(recordLength) {
		return fmt.Errorf("insufficient data for File Flags")
	}
	ff, err := UnmarshalFileFlags(data[offset])
	if err != nil {
		return fmt.Errorf("failed to unmarshal File Flags: %w", err)
	}
	dr.FileFlags = ff
	offset++

	// File Unit Size: 1 byte.
	if offset+1 > int(recordLength) {
		return fmt.Errorf("insufficient data for File Unit Size")
	}
	dr.FileUnitSize = data[offset]
	offset++

	// Interleave Gap Size: 1 byte.
	if offset+1 > int(recordLength) {
		return fmt.Errorf("insufficient data for Interleave Gap Size")
	}
	dr.InterleaveGapSize = data[offset]
	offset++

	// Volume Sequence Number: 4 bytes.
	if offset+4 > int(recordLength) {
		return fmt.Errorf("insufficient data for Volume Sequence Number")
	}
	var volSeqBytes [4]byte
	copy(volSeqBytes[:], data[offset:offset+4])
	volSeq, err := encoding.UnmarshalUint16LSBMSB(volSeqBytes)
	if err != nil {
		return fmt.Errorf("failed to unmarshal Volume Sequence Number: %w", err)
	}
	dr.VolumeSequenceNumber = volSeq
	offset += 4

	// Length of File Identifier: 1 byte.
	if offset+1 > int(recordLength) {
		return fmt.Errorf("insufficient data for Length of File Identifier")
	}
	dr.LengthOfFileIdentifier = data[offset]
	offset++

	// File Identifier: fiLen bytes.
	fiLen := int(dr.LengthOfFileIdentifier)
	if offset+fiLen > int(recordLength) {
		return fmt.Errorf("insufficient data for File Identifier")
	}

	if dr.Joliet {
		if fiLen == 1 {
			// Under Joliet spec, special directory identifiers remain as 8-bit values
			dr.FileIdentifier = string(data[offset : offset+fiLen])
		} else {
			dr.FileIdentifier = encoding.DecodeUCS2BigEndian(data[offset : offset+fiLen])
		}
	} else {
		dr.FileIdentifier = string(data[offset : offset+fiLen])
	}
	offset += fiLen

	// Padding Field: present if the File Identifier length is even.
	if fiLen%2 == 0 {
		if offset+1 > int(recordLength) {
			return fmt.Errorf("insufficient data for padding byte")
		}
		if data[offset] != 0x00 {
			return fmt.Errorf("expected padding byte 0x00, got 0x%02X", data[offset])
		}
		offset++
	}

	// System Use: remainder of the record.
	if offset < int(recordLength) {
		suLen := int(recordLength) - offset
		dr.SystemUse = make([]byte, suLen)
		copy(dr.SystemUse, data[offset:offset+suLen])
	} else {
		dr.SystemUse = nil
	}

	return nil
}
