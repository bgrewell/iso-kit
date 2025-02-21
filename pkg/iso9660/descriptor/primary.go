package descriptor

import (
	"encoding/binary"
	"fmt"
	"github.com/bgrewell/iso-kit/pkg/consts"
	"github.com/bgrewell/iso-kit/pkg/helpers"
	"github.com/bgrewell/iso-kit/pkg/iso9660/directory"
	"github.com/bgrewell/iso-kit/pkg/iso9660/encoding"
	"github.com/bgrewell/iso-kit/pkg/logging"
	"strings"
	"time"
)

//10.1 Level 1
// At Level 1 the following restrictions shall apply to a volume identified by a Primary Volume Descriptor or by a
// Supplementary Volume Descriptor:
//  - each file shall consist of only one File Section;
//  - a File Name shall not contain more than eight d-characters or eight d1-characters;
//  - a File Name Extension shall not contain more than three d-characters or three d1-characters;
//  - a Directory Identifier shall not contain more than eight d-characters or eight d1-characters.
//
// At Level 1 the following restrictions shall apply to a volume identified by an Enhanced Volume Descriptor:
//  - each file shall consist of only one File Section.

//10.2 Level 2
// At Level 2 the following restriction shall apply:
//  - each file shall consist of only one File Section.

//10.3 Level 3
// At Level 3 no restrictions shall apply

const (
	// Reserved for future use field from BP 1396 to 2048
	PRIMARY_RESERVED_FIELD2_SIZE        = 653
	PRIMARY_VOLUME_DESCRIPTOR_BODY_SIZE = 2041
)

type PrimaryVolumeDescriptor struct {
	VolumeDescriptorHeader
	PrimaryVolumeDescriptorBody
}

func (pvd *PrimaryVolumeDescriptor) VolumeIdentifier() string {
	return pvd.PrimaryVolumeDescriptorBody.VolumeIdentifier
}

func (pvd *PrimaryVolumeDescriptor) SystemIdentifier() string {
	return pvd.PrimaryVolumeDescriptorBody.SystemIdentifier
}

func (pvd *PrimaryVolumeDescriptor) VolumeSetIdentifier() string {
	return pvd.PrimaryVolumeDescriptorBody.VolumeSetIdentifier
}

func (pvd *PrimaryVolumeDescriptor) PublisherIdentifier() string {
	return pvd.PrimaryVolumeDescriptorBody.PublisherIdentifier
}

func (pvd *PrimaryVolumeDescriptor) DataPreparerIdentifier() string {
	return pvd.PrimaryVolumeDescriptorBody.DataPreparerIdentifier
}

func (pvd *PrimaryVolumeDescriptor) ApplicationIdentifier() string {
	return pvd.PrimaryVolumeDescriptorBody.ApplicationIdentifier
}

func (pvd *PrimaryVolumeDescriptor) CopyrightFileIdentifier() string {
	return pvd.PrimaryVolumeDescriptorBody.CopyrightFileIdentifier
}

func (pvd *PrimaryVolumeDescriptor) AbstractFileIdentifier() string {
	return pvd.PrimaryVolumeDescriptorBody.AbstractFileIdentifier
}

func (pvd *PrimaryVolumeDescriptor) BibliographicFileIdentifier() string {
	return pvd.PrimaryVolumeDescriptorBody.BibliographicFileIdentifier
}

func (pvd *PrimaryVolumeDescriptor) VolumeCreationDateTime() time.Time {
	return pvd.PrimaryVolumeDescriptorBody.VolumeCreationDateAndTime
}

func (pvd *PrimaryVolumeDescriptor) VolumeModificationDateTime() time.Time {
	return pvd.PrimaryVolumeDescriptorBody.VolumeModificationDateAndTime
}

func (pvd *PrimaryVolumeDescriptor) VolumeExpirationDateTime() time.Time {
	return pvd.PrimaryVolumeDescriptorBody.VolumeExpirationDateAndTime
}

func (pvd *PrimaryVolumeDescriptor) VolumeEffectiveDateTime() time.Time {
	return pvd.PrimaryVolumeDescriptorBody.VolumeEffectiveDateAndTime
}

func (pvd *PrimaryVolumeDescriptor) HasJoliet() bool {
	return false
}

func (pvd *PrimaryVolumeDescriptor) HasRockRidge() bool {
	if pvd.PrimaryVolumeDescriptorBody.RootDirectoryRecord == nil ||
		pvd.PrimaryVolumeDescriptorBody.RootDirectoryRecord.RockRidge == nil {
		return false
	}

	return pvd.PrimaryVolumeDescriptorBody.RootDirectoryRecord.RockRidge.HasRockRidge()
}

func (pvd *PrimaryVolumeDescriptor) RootDirectory() *directory.DirectoryRecord {
	return pvd.PrimaryVolumeDescriptorBody.RootDirectoryRecord
}

func (pvd *PrimaryVolumeDescriptor) Marshal() ([consts.ISO9660_SECTOR_SIZE]byte, error) {
	// Marshal the VolumeDescriptorHeader and PrimaryVolumeDescriptorBody.
	headerBytes, err := pvd.VolumeDescriptorHeader.Marshal()
	if err != nil {
		return [consts.ISO9660_SECTOR_SIZE]byte{}, fmt.Errorf("failed to marshal VolumeDescriptorHeader: %w", err)
	}
	bodyBytes, err := pvd.PrimaryVolumeDescriptorBody.Marshal()
	if err != nil {
		return [consts.ISO9660_SECTOR_SIZE]byte{}, fmt.Errorf("failed to marshal PrimaryVolumeDescriptorBody: %w", err)
	}

	// Combine the header and body into a single 2048-byte slice.
	var data [consts.ISO9660_SECTOR_SIZE]byte
	copy(data[:consts.ISO9660_VOLUME_DESC_HEADER_SIZE], headerBytes[:])
	copy(data[consts.ISO9660_VOLUME_DESC_HEADER_SIZE:], bodyBytes[:])

	return data, nil
}

func (pvd *PrimaryVolumeDescriptor) Unmarshal(data [consts.ISO9660_SECTOR_SIZE]byte) error {
	if len(data) < consts.ISO9660_SECTOR_SIZE {
		return fmt.Errorf("data too short: expected %d bytes, got %d", consts.ISO9660_SECTOR_SIZE, len(data))
	}

	// Unmarshal the VolumeDescriptorHeader.
	if err := pvd.VolumeDescriptorHeader.Unmarshal([7]byte(data[:consts.ISO9660_VOLUME_DESC_HEADER_SIZE])); err != nil {
		return fmt.Errorf("failed to unmarshal VolumeDescriptorHeader: %w", err)
	}

	// Unmarshal the PrimaryVolumeDescriptorBody.
	if err := pvd.PrimaryVolumeDescriptorBody.Unmarshal(data[consts.ISO9660_VOLUME_DESC_HEADER_SIZE:]); err != nil {
		return fmt.Errorf("failed to unmarshal PrimaryVolumeDescriptorBody: %w", err)
	}

	return nil
}

type PrimaryVolumeDescriptorBody struct {
	// Unused byte should be set to 0x00.
	UnusedField1 byte `json:"unused_field_1"`
	// System Identifier specifies a system which can recognize and act upon the content of the Logical Sectors within
	// logical Sector Numbers 0 to 15 of the volume.
	//  | (a-characters)
	SystemIdentifier string `json:"system_identifier"`
	// Volume Identifier specifies an identification of the volume
	//  | (d-characters)
	VolumeIdentifier string `json:"volume_identifier"`
	// Unused all bytes should be set to 0x00
	UnusedField2 [8]byte `json:"unused_field_2"`
	// Volume Space Size is a field that specifies the number of logical blocks in which the Volume Space of the volume
	// is recorded as a 32-bit number.
	//  | Encoding: BothByteOrder
	VolumeSpaceSize uint32 `json:"volume_space_size"`
	// Unused all bytes should be set to 0x00
	UnusedField3 [32]byte `json:"unused_field_3"`
	// Volume Set Size is a field that specifies the assigned Volume Set size of the volume as a 16-bit number.
	//  | Encoding: BothByteOrder
	VolumeSetSize uint16 `json:"volume_set_size"`
	// Volume Sequence Number is a field that represents the ordinal number of the volume in the Volume Set.
	//  | Encoding: BothByteOrder
	VolumeSequenceNumber uint16 `json:"volume_sequence_number"`
	// Logical Block Size specifies the size in bytes of a logical block
	//  | Encoding: BothByteOrder
	LogicalBlockSize uint16 `json:"logical_block_size"`
	// Path Table Size specifies the length in bytes of a recorded occurrence of the Path Table identified by this
	// Volume Descriptor.
	//  | Encoding: BothByteOrder
	PathTableSize uint32 `json:"path_table_size"`
	// This field shall specify as a 32-bit number the Logical Block Number of the first Logical Block allocated to the
	// Extent which contains an occurrence of the Path Table.
	//  | Encoding: LittleEndian
	LocationOfTypeLPathTable uint32 `json:"location_type_of_l_path_table"`
	// This field shall specify as a 32-bit number the Logical Block Number of the first Logical Block allocated to the
	// Extent which contains an optional occurrence of the Path Table. If the value is 0, it shall mean that the Extent
	// shall not be expected to have been recorded.
	//  | Encoding: LittleEndian
	LocationOfOptionalTypeLPathTable uint32 `json:"location_of_optional_type_l_path_table"`
	// This field shall specify as a 32-bit number the Logical Block Number of the first Logical Block allocated to the
	// Extent which contains an occurrence of the Path Table.
	//  | Encoding: BigEndian
	LocationOfTypeMPathTable uint32 `json:"location_of_m_path_table"`
	// This field shall specify as a 32-bit number the Logical Block Number of the first Logical Block allocated to the
	// Extent which contains an optional occurrence of the Path Table. If the value is 0, it shall mean that the Extent
	// shall not be expected to have been recorded.
	//  | Encoding: BigEndian
	LocationOfOptionalTypeMPathTable uint32 `json:"location_of_optional_type_m_path_table"`
	// Root Director Record contains an occurrence of the Directory Record for the Root Directory.
	RootDirectoryRecord *directory.DirectoryRecord `json:"root_directory_record"`
	// Volume Set Identifier specifies an identification of the Volume Set of which the volume is a member.
	//  | (d-characters)
	VolumeSetIdentifier string `json:"volume_set_identifier"`
	// Publisher Identifier specifies an identification of the user who specified what shall be recorded on the
	// Volume Group of which the volume is a member. If the first byte is set to (5F), the remaining bytes of this field
	// shall specify an identifier for a file containing the identification of the user. This file shall be described in
	// the Root Directory. The File Name shall not contain more than eight d-characters and the File Name Extension
	// shall not contain more than three d-characters. All 'ISO9660_FILLER' means there is no identifier.
	PublisherIdentifier string `json:"publisher_identifier"`
	// Data Preparer Identifier specifies an identification of the person or other entity which controls the
	// preparation of the data to be recorded on the Volume Group of which the volume is a member. If the first byte is
	// set to (5F), the remaining bytes of this field shall specify an identifier for a file containing the
	// identification of the data preparer. This file shall be described in the Root Directory. The File Name shall not
	// contain more than eight d-characters and the File Name Extension shall not contain more than three d-characters.
	// All 'ISO9660_FILLER' means there is no identifier.
	DataPreparerIdentifier string `json:"data_preparer_identifier"`
	// Application Identifier specifies an identification of the specification of how the data are recorded on the
	// Volume Group of which the volume is a member. If the first byte is set to (5F), the remaining bytes of this field
	// shall specify an identifier for a file containing the identification of the application. This file shall be
	// described in the Root Directory. The File Name shall not contain more than eight d-characters and the File Name
	// Extension shall not contain more than three d-characters. All 'ISO9660_FILLER' means there is no identifier.
	ApplicationIdentifier string `json:"application_identifier"`
	// Copyright File Identifier specifies an identification for a file described by the Root Directory and containing a copyright
	// statement for those volumes of the Volume Set the sequence numbers of which are less than, or equal to, the
	// assigned Volume Set size of the volume. If all bytes of this field are set to (FILLER), it shall mean that no
	// such file is identified. The File Name of a Copyright File Identifier shall not contain more than eight
	// d-characters. The File Name Extension of a Copyright File Identifier shall not contain more than three
	// d-characters. Allowed characters are d-characters, SEPARATOR 1 and SEPARATOR 2
	CopyrightFileIdentifier string `json:"copyright_file_identifier"`
	// Abstract File Identifier specifies an identification for a file described by the Root Directory and containing an
	// abstract statement for those volumes of the Volume Set the sequence numbers of which are less than, or equal to,
	// the assigned Volume Set size of the volume. If all bytes of this field are set to (FILLER), it shall mean that no
	// such file is identified. The File Name of an Abstract File Identifier shall not contain more than eight
	// d-characters. The File Name Extension of an Abstract File Identifier shall not contain more than three
	// d-characters. The characters in this field shall be d-characters, SEPARATOR 1 and SEPARATOR 2.
	AbstractFileIdentifier string `json:"abstract_file_identifier"`
	// Bibliographic File Identifier specifies an identification for a file described by the Root Directory and
	// containing bibliographic records interpreted according to standards that are the subject of an agreement between
	// the originator and the recipient of the volume. If all bytes of this field are set to (FILLER), it shall mean
	// that no such file is identified. The File Name of a Bibliographic File Identifier shall not contain more than
	// eight d-characters. The File Name Extension of a Bibliographic File Identifier shall not contain more than three
	// d-characters. The characters in this field shall be d-characters, SEPARATOR 1 and SEPARATOR 2.
	BibliographicFileIdentifier string `json:"bibliographic_file_identifier"`
	// Volume Creation Date and Time specifies the date and the time of the day at which the information in the
	// volume was created.
	//  | 8.4.26.1 Date and Time Format
	VolumeCreationDateAndTime time.Time `json:"volume_creation_date_and_time"`
	// Volume Modification Date and Time specifies the date and the time of the day at which the information in the
	// volume was last modified.
	//  | 8.4.26.1 Date and Time Format
	VolumeModificationDateAndTime time.Time `json:"volume_modification_date_and_time"`
	// Volume Expiration Date and Time specifies the date and the time of the day at which the information in the
	// volume may be regarded as obsolete. If the date and time are not specified then the information shall not be
	// regarded as obsolete.
	//  | 8.4.26.1 Date and Time Format
	VolumeExpirationDateAndTime time.Time `json:"volume_expiration_date_and_time"`
	// Volume Effective Date and Time specifies the date and the time of the day at which the information in the volume
	// may be used. If the date and time are not specified then the information may be used at once.
	//  | 8.4.26.1 Date and Time Format
	VolumeEffectiveDateAndTime time.Time `json:"volume_effective_date_and_time"`
	// File Structure Version specifies as an 8-bit number the version of the specification of the records of a
	// directory and of a Path Table. For a Primary Volume Descriptor or for a Supplementary Volume Descriptor, 1 shall
	// indicate the structure of this Standard. For an Enhanced Volume Descriptor, 2 shall indicate the structure of
	// this Standard.
	FileStructureVersion uint8 `json:"file_structure_version"`
	// Reserved Field 1 is unused and should be set to 0x00.
	ReservedField1 byte `json:"reserved_field_1"`
	// Application Use field is reserved for application use.
	ApplicationUse [consts.ISO9660_APPLICATION_USE_SIZE]byte `json:"application_use"`
	// Reserved Field 2 is unused and all bytes should be set to 0x00.
	ReservedField2 [PRIMARY_RESERVED_FIELD2_SIZE]byte `json:"reserved_field_2"`
	// Logger
	Logger *logging.Logger
}

// Marshal converts the PrimaryVolumeDescriptorBody into its 2041‑byte on‑disk representation,
// ensuring that all string fields are padded with consts.ISO9660_FILLER (' ').
func (pvdb *PrimaryVolumeDescriptorBody) Marshal() ([PRIMARY_VOLUME_DESCRIPTOR_BODY_SIZE]byte, error) {
	var data [PRIMARY_VOLUME_DESCRIPTOR_BODY_SIZE]byte
	offset := 0

	// 1. unusedField1: 1 byte.
	data[offset] = pvdb.UnusedField1
	offset++

	// 2. systemIdentifier: 32 bytes (padded with ' ').
	sysID := helpers.PadString(pvdb.SystemIdentifier, 32)
	copy(data[offset:offset+32], sysID)
	offset += 32

	// 3. volumeIdentifier: 32 bytes (padded).
	volID := helpers.PadString(pvdb.VolumeIdentifier, 32)
	copy(data[offset:offset+32], volID)
	offset += 32

	// 4. unusedField2: 8 bytes.
	copy(data[offset:offset+8], pvdb.UnusedField2[:])
	offset += 8

	// 5. volumeSpaceSize: 8 bytes (both-byte orders for uint32).
	vsBytes := encoding.MarshalBothByteOrders32(pvdb.VolumeSpaceSize)
	copy(data[offset:offset+8], vsBytes[:])
	offset += 8

	// 6. unusedField3: 32 bytes.
	copy(data[offset:offset+32], pvdb.UnusedField3[:])
	offset += 32

	// 7. volumeSetSize: 4 bytes (both-byte orders for uint16).
	vssBytes := encoding.MarshalBothByteOrders16(pvdb.VolumeSetSize)
	copy(data[offset:offset+4], vssBytes[:])
	offset += 4

	// 8. volumeSequenceNumber: 4 bytes (both-byte orders for uint16).
	vsnBytes := encoding.MarshalBothByteOrders16(pvdb.VolumeSequenceNumber)
	copy(data[offset:offset+4], vsnBytes[:])
	offset += 4

	// 9. logicalBlockSize: 4 bytes (both-byte orders for uint16).
	lbsBytes := encoding.MarshalBothByteOrders16(pvdb.LogicalBlockSize)
	copy(data[offset:offset+4], lbsBytes[:])
	offset += 4

	// 10. pathTableSize: 8 bytes (both-byte orders for uint32).
	ptsBytes := encoding.MarshalBothByteOrders32(pvdb.PathTableSize)
	copy(data[offset:offset+8], ptsBytes[:])
	offset += 8

	// 11. locationOfTypeLPathTable: 4 bytes, little-endian.
	binary.LittleEndian.PutUint32(data[offset:offset+4], pvdb.LocationOfTypeLPathTable)
	offset += 4

	// 12. locationOfOptionalTypeLPathTable: 4 bytes, little-endian.
	binary.LittleEndian.PutUint32(data[offset:offset+4], pvdb.LocationOfOptionalTypeLPathTable)
	offset += 4

	// 13. locationOfTypeMPathTable: 4 bytes, big-endian.
	binary.BigEndian.PutUint32(data[offset:offset+4], pvdb.LocationOfTypeMPathTable)
	offset += 4

	// 14. locationOfOptionalTypeMPathTable: 4 bytes, big-endian.
	binary.BigEndian.PutUint32(data[offset:offset+4], pvdb.LocationOfOptionalTypeMPathTable)
	offset += 4

	// 15. rootDirectoryRecord: 34 bytes.
	if pvdb.RootDirectoryRecord == nil {
		return data, fmt.Errorf("rootDirectoryRecord is nil")
	}
	rdBytes, err := pvdb.RootDirectoryRecord.Marshal()
	if err != nil {
		return data, fmt.Errorf("failed to marshal rootDirectoryRecord: %w", err)
	}
	if len(rdBytes) != 34 {
		return data, fmt.Errorf("expected 34 bytes for rootDirectoryRecord, got %d", len(rdBytes))
	}
	copy(data[offset:offset+34], rdBytes)
	offset += 34

	// 16. volumeSetIdentifier: 128 bytes.
	vsi := helpers.PadString(pvdb.VolumeSetIdentifier, 128)
	copy(data[offset:offset+128], vsi)
	offset += 128

	// 17. publisherIdentifier: 128 bytes.
	pubID := helpers.PadString(pvdb.PublisherIdentifier, 128)
	copy(data[offset:offset+128], pubID)
	offset += 128

	// 18. dataPreparerIdentifier: 128 bytes.
	dpID := helpers.PadString(pvdb.DataPreparerIdentifier, 128)
	copy(data[offset:offset+128], dpID)
	offset += 128

	// 19. applicationIdentifier: 128 bytes.
	appID := helpers.PadString(pvdb.ApplicationIdentifier, 128)
	copy(data[offset:offset+128], appID)
	offset += 128

	// 20. copyrightFileIdentifier: 37 bytes.
	cfID := helpers.PadString(pvdb.CopyrightFileIdentifier, 37)
	copy(data[offset:offset+37], cfID)
	offset += 37

	// 21. abstractFileIdentifier: 37 bytes.
	afID := helpers.PadString(pvdb.AbstractFileIdentifier, 37)
	copy(data[offset:offset+37], afID)
	offset += 37

	// 22. bibliographicFileIdentifier: 37 bytes.
	bfID := helpers.PadString(pvdb.BibliographicFileIdentifier, 37)
	copy(data[offset:offset+37], bfID)
	offset += 37

	// 23. volumeCreationDateAndTime: 17 bytes.
	vcdBytes, err := encoding.MarshalDateTime(pvdb.VolumeCreationDateAndTime)
	if err != nil {
		return data, fmt.Errorf("failed to marshal volumeCreationDateAndTime: %w", err)
	}
	copy(data[offset:offset+17], vcdBytes[:])
	offset += 17

	// 24. volumeModificationDateAndTime: 17 bytes.
	vmdBytes, err := encoding.MarshalDateTime(pvdb.VolumeModificationDateAndTime)
	if err != nil {
		return data, fmt.Errorf("failed to marshal volumeModificationDateAndTime: %w", err)
	}
	copy(data[offset:offset+17], vmdBytes[:])
	offset += 17

	// 25. volumeExpirationDateAndTime: 17 bytes.
	vedBytes, err := encoding.MarshalDateTime(pvdb.VolumeExpirationDateAndTime)
	if err != nil {
		return data, fmt.Errorf("failed to marshal volumeExpirationDateAndTime: %w", err)
	}
	copy(data[offset:offset+17], vedBytes[:])
	offset += 17

	// 26. volumeEffectiveDateAndTime: 17 bytes.
	vefBytes, err := encoding.MarshalDateTime(pvdb.VolumeEffectiveDateAndTime)
	if err != nil {
		return data, fmt.Errorf("failed to marshal volumeEffectiveDateAndTime: %w", err)
	}
	copy(data[offset:offset+17], vefBytes[:])
	offset += 17

	// 27. fileStructureVersion: 1 byte.
	data[offset] = pvdb.FileStructureVersion
	offset++

	// 28. reservedField1: 1 byte.
	data[offset] = pvdb.ReservedField1
	offset++

	// 29. applicationUse: 512 bytes.
	copy(data[offset:offset+512], pvdb.ApplicationUse[:])
	offset += 512

	// 30. reservedField2: 653 bytes.
	copy(data[offset:offset+653], pvdb.ReservedField2[:])
	offset += 653

	if offset != PRIMARY_VOLUME_DESCRIPTOR_BODY_SIZE {
		return data, fmt.Errorf("marshal error: expected offset %d, got %d",
			PRIMARY_VOLUME_DESCRIPTOR_BODY_SIZE, offset)
	}

	return data, nil
}

// Unmarshal parses a 2041-byte slice into the PrimaryVolumeDescriptorBody.
// Fixed-width string fields have their trailing spaces trimmed.
func (pvdb *PrimaryVolumeDescriptorBody) Unmarshal(data []byte) error {
	if len(data) < PRIMARY_VOLUME_DESCRIPTOR_BODY_SIZE {
		return fmt.Errorf("data too short: expected %d bytes, got %d", PRIMARY_VOLUME_DESCRIPTOR_BODY_SIZE, len(data))
	}
	offset := 0

	// 1. unusedField1: 1 byte.
	pvdb.UnusedField1 = data[offset]
	offset++

	// 2. systemIdentifier: 32 bytes.
	pvdb.SystemIdentifier = strings.TrimRight(string(data[offset:offset+32]), " ")
	offset += 32

	// 3. volumeIdentifier: 32 bytes.
	pvdb.VolumeIdentifier = strings.TrimRight(string(data[offset:offset+32]), " ")
	offset += 32

	// 4. unusedField2: 8 bytes.
	copy(pvdb.UnusedField2[:], data[offset:offset+8])
	offset += 8

	// 5. volumeSpaceSize: 8 bytes (both-byte orders for uint32).
	var vsBytes [8]byte
	copy(vsBytes[:], data[offset:offset+8])
	volSpace, err := encoding.UnmarshalUint32LSBMSB(vsBytes)
	if err != nil {
		return fmt.Errorf("failed to unmarshal volumeSpaceSize: %w", err)
	}
	pvdb.VolumeSpaceSize = volSpace
	offset += 8

	// 6. unusedField3: 32 bytes.
	copy(pvdb.UnusedField3[:], data[offset:offset+32])
	offset += 32

	// 7. volumeSetSize: 4 bytes (both-byte orders for uint16).
	var vssBytes [4]byte
	copy(vssBytes[:], data[offset:offset+4])
	volSetSize, err := encoding.UnmarshalUint16LSBMSB(vssBytes)
	if err != nil {
		return fmt.Errorf("failed to unmarshal volumeSetSize: %w", err)
	}
	pvdb.VolumeSetSize = volSetSize
	offset += 4

	// 8. volumeSequenceNumber: 4 bytes (both-byte orders for uint16).
	var vsnBytes [4]byte
	copy(vsnBytes[:], data[offset:offset+4])
	volSeqNum, err := encoding.UnmarshalUint16LSBMSB(vsnBytes)
	if err != nil {
		return fmt.Errorf("failed to unmarshal volumeSequenceNumber: %w", err)
	}
	pvdb.VolumeSequenceNumber = volSeqNum
	offset += 4

	// 9. logicalBlockSize: 4 bytes (both-byte orders for uint16).
	var lbsBytes [4]byte
	copy(lbsBytes[:], data[offset:offset+4])
	logBlockSize, err := encoding.UnmarshalUint16LSBMSB(lbsBytes)
	if err != nil {
		return fmt.Errorf("failed to unmarshal logicalBlockSize: %w", err)
	}
	pvdb.LogicalBlockSize = logBlockSize
	offset += 4

	// 10. pathTableSize: 8 bytes (both-byte orders for uint32).
	var ptsBytes [8]byte
	copy(ptsBytes[:], data[offset:offset+8])
	pathTableSize, err := encoding.UnmarshalUint32LSBMSB(ptsBytes)
	if err != nil {
		return fmt.Errorf("failed to unmarshal pathTableSize: %w", err)
	}
	pvdb.PathTableSize = pathTableSize
	offset += 8

	// 11. locationOfTypeLPathTable: 4 bytes, little-endian.
	pvdb.LocationOfTypeLPathTable = binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	// 12. locationOfOptionalTypeLPathTable: 4 bytes, little-endian.
	pvdb.LocationOfOptionalTypeLPathTable = binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	// 13. locationOfTypeMPathTable: 4 bytes, big-endian.
	pvdb.LocationOfTypeMPathTable = binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4

	// 14. locationOfOptionalTypeMPathTable: 4 bytes, big-endian.
	pvdb.LocationOfOptionalTypeMPathTable = binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4

	// 15. rootDirectoryRecord: 34 bytes.
	if pvdb.RootDirectoryRecord == nil {
		pvdb.RootDirectoryRecord = new(directory.DirectoryRecord)
	}
	if err := pvdb.RootDirectoryRecord.Unmarshal(data[offset : offset+34]); err != nil {
		return fmt.Errorf("failed to unmarshal rootDirectoryRecord: %w", err)
	}
	offset += 34

	// 16. volumeSetIdentifier: 128 bytes.
	pvdb.VolumeSetIdentifier = strings.TrimRight(string(data[offset:offset+128]), " ")
	offset += 128

	// 17. publisherIdentifier: 128 bytes.
	pvdb.PublisherIdentifier = strings.TrimRight(string(data[offset:offset+128]), " ")
	offset += 128

	// 18. dataPreparerIdentifier: 128 bytes.
	pvdb.DataPreparerIdentifier = strings.TrimRight(string(data[offset:offset+128]), " ")
	offset += 128

	// 19. applicationIdentifier: 128 bytes.
	pvdb.ApplicationIdentifier = strings.TrimRight(string(data[offset:offset+128]), " ")
	offset += 128

	// 20. copyrightFileIdentifier: 37 bytes.
	pvdb.CopyrightFileIdentifier = strings.TrimRight(string(data[offset:offset+37]), " ")
	offset += 37

	// 21. abstractFileIdentifier: 37 bytes.
	pvdb.AbstractFileIdentifier = strings.TrimRight(string(data[offset:offset+37]), " ")
	offset += 37

	// 22. bibliographicFileIdentifier: 37 bytes.
	pvdb.BibliographicFileIdentifier = strings.TrimRight(string(data[offset:offset+37]), " ")
	offset += 37

	// 23. volumeCreationDateAndTime: 17 bytes.
	var vcdBytes [17]byte
	copy(vcdBytes[:], data[offset:offset+17])
	volCreation, err := encoding.UnmarshalDateTime(vcdBytes)
	if err != nil {
		return fmt.Errorf("failed to unmarshal volumeCreationDateAndTime: %w", err)
	}
	pvdb.VolumeCreationDateAndTime = volCreation
	offset += 17

	// 24. volumeModificationDateAndTime: 17 bytes.
	var vmdBytes [17]byte
	copy(vmdBytes[:], data[offset:offset+17])
	volMod, err := encoding.UnmarshalDateTime(vmdBytes)
	if err != nil {
		return fmt.Errorf("failed to unmarshal volumeModificationDateAndTime: %w", err)
	}
	pvdb.VolumeModificationDateAndTime = volMod
	offset += 17

	// 25. volumeExpirationDateAndTime: 17 bytes.
	var vedBytes [17]byte
	copy(vedBytes[:], data[offset:offset+17])
	volExp, err := encoding.UnmarshalDateTime(vedBytes)
	if err != nil {
		return fmt.Errorf("failed to unmarshal volumeExpirationDateAndTime: %w", err)
	}
	pvdb.VolumeExpirationDateAndTime = volExp
	offset += 17

	// 26. volumeEffectiveDateAndTime: 17 bytes.
	var vefBytes [17]byte
	copy(vefBytes[:], data[offset:offset+17])
	volEff, err := encoding.UnmarshalDateTime(vefBytes)
	if err != nil {
		return fmt.Errorf("failed to unmarshal volumeEffectiveDateAndTime: %w", err)
	}
	pvdb.VolumeEffectiveDateAndTime = volEff
	offset += 17

	// 27. fileStructureVersion: 1 byte.
	pvdb.FileStructureVersion = data[offset]
	offset++

	// 28. reservedField1: 1 byte.
	pvdb.ReservedField1 = data[offset]
	offset++

	// 29. applicationUse: 512 bytes.
	copy(pvdb.ApplicationUse[:], data[offset:offset+512])
	offset += 512

	// 30. reservedField2: 653 bytes.
	copy(pvdb.ReservedField2[:], data[offset:offset+653])
	offset += 653

	if offset != PRIMARY_VOLUME_DESCRIPTOR_BODY_SIZE {
		return fmt.Errorf("unmarshal error: expected offset %d, got %d", PRIMARY_VOLUME_DESCRIPTOR_BODY_SIZE, offset)
	}
	return nil
}
