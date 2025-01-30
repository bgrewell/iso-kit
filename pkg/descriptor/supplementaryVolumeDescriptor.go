package descriptor

import (
	"encoding/binary"
	"github.com/bgrewell/iso-kit/pkg/consts"
	. "github.com/bgrewell/iso-kit/pkg/directory"
	. "github.com/bgrewell/iso-kit/pkg/encoding"
	"github.com/bgrewell/iso-kit/pkg/logging"
	"github.com/bgrewell/iso-kit/pkg/path"
	"io"
	"strings"
)

// ParseSupplementaryVolumeDescriptor parses the given volume descriptor and returns a SupplementaryVolumeDescriptor.
func ParseSupplementaryVolumeDescriptor(vd VolumeDescriptor, isoFile io.ReaderAt) (*SupplementaryVolumeDescriptor, error) {
	logging.Logger().Trace("Parsing supplementary volume descriptor")
	svd := &SupplementaryVolumeDescriptor{
		isoFile: isoFile,
	}
	if err := svd.Unmarshal(vd.Data(), isoFile); err != nil {
		logging.Logger().Error(err, "Failed to unmarshal supplementary volume descriptor")
		return nil, err
	}
	logging.Logger().Trace("Successfully parsed supplementary volume descriptor")

	logging.Logger().Tracef("Volume descriptor type: %d", svd.Type())
	if svd.Type() != VolumeDescriptorSupplementary {
		logging.Logger().Warnf("Invalid supplementary volume descriptor: %d", svd.Type())
	}

	logging.Logger().Tracef("Standard identifier: %s", svd.Identifier())
	if svd.Identifier() != consts.ISO9660_STD_IDENTIFIER {
		logging.Logger().Warnf("Invalid standard identifier: %s, expected: %s", svd.Identifier(), consts.ISO9660_STD_IDENTIFIER)
	}

	// Note: The version number is not always 1. It can be 1 or 2. 1 is for Standard and 2 is for enhanced.
	logging.Logger().Tracef("Volume descriptor version: %d", svd.Version())
	if svd.Version() != consts.ISO9660_VOLUME_DESC_VERSION {
		logging.Logger().Warnf("Invalid volume descriptor version: %d, expected: %d", svd.Version(), consts.ISO9660_VOLUME_DESC_VERSION)
	}

	logging.Logger().Tracef("Volume flags: %v", svd.VolumeFlags)
	logging.Logger().Tracef("System identifier: %s", svd.SystemIdentifier)
	logging.Logger().Tracef("Volume identifier: %s", svd.VolumeIdentifier)
	logging.Logger().Tracef("Volume space size: %d", svd.VolumeSpaceSize)
	logging.Logger().Tracef("Volume set size: %d", svd.VolumeSetSize)
	logging.Logger().Tracef("Volume sequence number: %d", svd.VolumeSequenceNumber)
	logging.Logger().Tracef("Logical block size: %d", svd.LogicalBlockSize)
	logging.Logger().Tracef("Path table size: %d", svd.PathTableSize())
	logging.Logger().Tracef("Path table location (L): %d", svd.LPathTableLocation)
	logging.Logger().Tracef("Path table location (M): %d", svd.MPathTableLocation)
	logging.Logger().Tracef("Root directory entry: %v", svd.RootDirectoryEntry)
	logging.Logger().Tracef("Volume set identifier: %s", svd.VolumeSetIdentifier)
	logging.Logger().Tracef("Publisher identifier: %s", svd.PublisherIdentifier)
	logging.Logger().Tracef("Data preparer identifier: %s", svd.DataPreparerIdentifier)
	logging.Logger().Tracef("Application identifier: %s", svd.ApplicationIdentifier)
	logging.Logger().Tracef("Copyright file identifier: %s", svd.CopyRightFileIdentifier)
	logging.Logger().Tracef("Abstract file identifier: %s", svd.AbstractFileIdentifier)
	logging.Logger().Tracef("Bibliographic file identifier: %s", svd.BibliographicFileIdentifier)
	logging.Logger().Tracef("Volume creation date: %s", svd.VolumeCreationDate)
	logging.Logger().Tracef("Volume modification date: %s", svd.VolumeModificationDate)
	logging.Logger().Tracef("Volume expiration date: %s", svd.VolumeExpirationDate)
	logging.Logger().Tracef("Volume effective date: %s", svd.VolumeEffectiveDate)
	logging.Logger().Tracef("File structure version: %d", svd.FileStructureVersion)
	logging.Logger().Tracef("Application use: %s", strings.TrimSpace(string(svd.ApplicationUse[:])))

	// Optional path table locations (not always used in PVD)
	logging.Logger().Tracef("Optional path table location (L): %d", svd.LOptionalPathTableLocation)
	logging.Logger().Tracef("Optional path table location (M): %d", svd.MOptionalPathTableLocation)

	// Escape sequences (unique to SVD)
	logging.Logger().Tracef("Escape sequences: %v", svd.EscapeSequences)
	if string(svd.EscapeSequences[0:3]) == consts.JOLIET__LEVEL_1_ESCAPE {
		logging.Logger().Trace("Level 1 Joliet escape sequence detected")
	} else if string(svd.EscapeSequences[0:3]) == consts.JOLIET__LEVEL_2_ESCAPE {
		logging.Logger().Trace("Level 2 Joliet escape sequence detected")

	} else if string(svd.EscapeSequences[0:3]) == consts.JOLIET__LEVEL_3_ESCAPE {
		logging.Logger().Trace("Level 3 Joliet escape sequence detected")
	}
	// Log any unused fields, if helpful for debugging
	logging.Logger().Tracef("Unused field 2: %v", svd.UnusedField2)
	logging.Logger().Tracef("Unused field 4: %v", svd.UnusedField4)
	logging.Logger().Tracef("Unused field 5: %v", svd.UnusedField5)

	// Walk the directory entries
	// TODO: use logging.Logger().Tracef to log the number of directories and number of files

	return svd, nil
}

// SupplementaryVolumeDescriptor represents a supplementary volume descriptor in an ISO file.
type SupplementaryVolumeDescriptor struct {
	rawData                     [2048]byte              // Raw data from the volume descriptor
	vdType                      VolumeDescriptorType    // Numeric value
	standardIdentifier          string                  // Always "CD001"
	volumeDescriptorVersion     int8                    // Numeric value
	VolumeFlags                 [1]byte                 // 8 bits of flags
	SystemIdentifier            string                  // Identifier of the system that can act upon the volume
	VolumeIdentifier            string                  // Identifier of the volume
	UnusedField2                [8]byte                 // Unused field should be 0x00
	VolumeSpaceSize             int32                   // Size of the volume in logical blocks
	EscapeSequences             [32]byte                // Should be 0x00
	VolumeSetSize               int16                   // Number of volumes in the volume set
	VolumeSequenceNumber        int16                   // Number of this volume in the volume set
	LogicalBlockSize            int16                   // Size of the logical blocks in bytes
	pathTableSize               int32                   // Size of the path table in bytes
	LPathTableLocation          uint32                  // Location of the path table for the first directory record
	LOptionalPathTableLocation  uint32                  // Location of the optional path table
	MPathTableLocation          uint32                  // Location of the path table for the second directory record
	MOptionalPathTableLocation  uint32                  // Location of the optional path table
	RootDirectoryEntry          *DirectoryEntry         // Directory entry for the root directory
	VolumeSetIdentifier         string                  // Identifier of the volume set
	PublisherIdentifier         string                  // Identifier of the publisher
	DataPreparerIdentifier      string                  // Identifier of the data preparer
	ApplicationIdentifier       string                  // Identifier of the application
	CopyRightFileIdentifier     string                  // Identifier of the copyright file
	AbstractFileIdentifier      string                  // Identifier of the abstract file
	BibliographicFileIdentifier string                  // Identifier of the bibliographic file
	VolumeCreationDate          string                  // Date and time the volume was created
	VolumeModificationDate      string                  // Date and time the volume was last modified
	VolumeExpirationDate        string                  // Date and time the volume expires
	VolumeEffectiveDate         string                  // Date and time the volume is effective
	FileStructureVersion        byte                    // Version of the file structure
	UnusedField4                byte                    // Unused field should be 0x00
	ApplicationUse              [512]byte               // Application-specific data
	UnusedField5                [653]byte               // Unused field should be 0x00
	pathTable                   []*path.PathTableRecord // Path Table
	isoFile                     io.ReaderAt             // Reader for the ISO file
	isJoliet                    bool                    // Whether this is a Joliet SVD
}

// Type returns the volume descriptor type for the SVD.
func (svd *SupplementaryVolumeDescriptor) Type() VolumeDescriptorType {
	return svd.vdType
}

// Identifier returns the standard identifier for the SVD.
func (svd *SupplementaryVolumeDescriptor) Identifier() string {
	return svd.standardIdentifier
}

// Version returns the volume descriptor version for the SVD.
func (svd *SupplementaryVolumeDescriptor) Version() int8 {
	return svd.volumeDescriptorVersion
}

// Data returns the raw data for the SVD.
func (svd *SupplementaryVolumeDescriptor) Data() [2048]byte {
	return svd.rawData
}

// SystemID returns the path table location for the SVD.
func (svd *SupplementaryVolumeDescriptor) PathTableLocation() uint32 {
	return svd.LPathTableLocation
}

// PathTableSize returns the size of the path table for the SVD.
func (svd *SupplementaryVolumeDescriptor) PathTableSize() int32 {
	return svd.pathTableSize
}

// PathTable returns the path table records for the SVD.
func (svd *SupplementaryVolumeDescriptor) PathTable() *[]*path.PathTableRecord {
	if svd.pathTable == nil {
		svd.pathTable = make([]*path.PathTableRecord, 0)
	}

	return &svd.pathTable
}

// IsJoliet returns true if the SVD is a Joliet SVD.
func (svd *SupplementaryVolumeDescriptor) IsJoliet() bool {
	return svd.isJoliet
}

// Unmarshal parses the given byte slice and populates the PrimaryVolumeDescriptor struct.
func (svd *SupplementaryVolumeDescriptor) Unmarshal(data [consts.ISO9660_SECTOR_SIZE]byte, isoFile io.ReaderAt) (err error) {

	logging.Logger().Tracef("Unmarshalling %d bytes of supplementary volume descriptor data", len(data))

	svd.rawData = data

	// Handle escape sequences early to determine if Joliet is in use
	copy(svd.EscapeSequences[:], data[88:120])
	if string(svd.EscapeSequences[0:3]) == consts.JOLIET__LEVEL_1_ESCAPE ||
		string(svd.EscapeSequences[0:3]) == consts.JOLIET__LEVEL_2_ESCAPE ||
		string(svd.EscapeSequences[0:3]) == consts.JOLIET__LEVEL_3_ESCAPE {
		svd.isJoliet = true
	}

	rootRecord := DirectoryRecord{
		Joliet: svd.isJoliet,
	}
	err = rootRecord.Unmarshal(data[156:190], isoFile)
	if err != nil {
		return err
	}

	svd.vdType = VolumeDescriptorType(data[0])
	svd.standardIdentifier = string(data[1:6])
	svd.volumeDescriptorVersion = int8(data[6])
	copy(svd.VolumeFlags[:], data[7:8])
	svd.SystemIdentifier = string(data[8:40])
	svd.VolumeIdentifier = string(data[40:72])
	copy(svd.UnusedField2[:], data[72:80])
	svd.VolumeSpaceSize, err = UnmarshalInt32LSBMSB(data[80:88])
	if err != nil {
		return err
	}
	svd.VolumeSetSize, err = UnmarshalInt16LSBMSB(data[120:124])
	if err != nil {
		return err
	}
	svd.VolumeSequenceNumber, err = UnmarshalInt16LSBMSB(data[124:128])
	if err != nil {
		return err
	}
	svd.LogicalBlockSize, err = UnmarshalInt16LSBMSB(data[128:132])
	if err != nil {
		return err
	}
	svd.pathTableSize, err = UnmarshalInt32LSBMSB(data[132:140])
	if err != nil {
		return err
	}
	svd.LPathTableLocation = binary.LittleEndian.Uint32(data[140:144])
	svd.LOptionalPathTableLocation = binary.LittleEndian.Uint32(data[144:148])
	svd.MPathTableLocation = binary.BigEndian.Uint32(data[148:152])
	svd.MOptionalPathTableLocation = binary.BigEndian.Uint32(data[152:156])
	svd.RootDirectoryEntry = &DirectoryEntry{
		Record:    &rootRecord,
		IsoReader: isoFile,
	}
	svd.VolumeSetIdentifier = string(data[190:318])
	svd.PublisherIdentifier = string(data[318:446])
	svd.DataPreparerIdentifier = string(data[446:574])
	svd.ApplicationIdentifier = string(data[574:702])
	svd.CopyRightFileIdentifier = string(data[702:739])
	svd.AbstractFileIdentifier = string(data[739:776])
	svd.BibliographicFileIdentifier = string(data[776:813])
	svd.VolumeCreationDate = string(data[813:830])
	svd.VolumeModificationDate = string(data[830:847])
	svd.VolumeExpirationDate = string(data[847:864])
	svd.VolumeEffectiveDate = string(data[864:881])
	svd.FileStructureVersion = data[881]
	svd.UnusedField4 = data[882]
	copy(svd.ApplicationUse[:], data[883:1395])
	copy(svd.UnusedField5[:], data[1395:2048])

	return nil
}
