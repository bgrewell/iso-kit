package descriptor

import (
	"encoding/binary"
	"fmt"
	"github.com/bgrewell/iso-kit/pkg/consts"
	"github.com/bgrewell/iso-kit/pkg/directory"
	"github.com/bgrewell/iso-kit/pkg/encoding"
	"github.com/bgrewell/iso-kit/pkg/logging"
	"github.com/bgrewell/iso-kit/pkg/path"
	"github.com/go-logr/logr"
	"io"
	"strings"
)

// ParsePrimaryVolumeDescriptor parses the given volume descriptor and returns a PrimaryVolumeDescriptor struct.
func ParsePrimaryVolumeDescriptor(vd VolumeDescriptor, isoFile io.ReaderAt, useRR bool, logger logr.Logger) (*PrimaryVolumeDescriptor, error) {
	logger.V(logging.TRACE).Info("Parsing primary volume descriptor")

	pvd := &PrimaryVolumeDescriptor{
		isoFile: isoFile,
		logger:  logger,
		useRR:   useRR,
	}

	if err := pvd.Unmarshal(vd.Data(), isoFile); err != nil {
		logger.Error(err, "Failed to unmarshal primary volume descriptor")
		return nil, err
	}
	logger.V(logging.TRACE).Info("Successfully parsed primary volume descriptor")

	logger.V(logging.TRACE).Info("Volume descriptor type", "type", pvd.Type())
	if pvd.Type() != VolumeDescriptorPrimary {
		logger.Error(nil, "WARNING: Invalid primary volume descriptor",
			"actualType", pvd.Type(), "expectedType", VolumeDescriptorPrimary)
	}

	logger.V(logging.TRACE).Info("Standard identifier", "identifier", pvd.Identifier())
	if pvd.Identifier() != consts.ISO9660_STD_IDENTIFIER {
		logger.Error(nil, "WARNING: Invalid standard identifier",
			"actualIdentifier", pvd.Identifier(), "expectedIdentifier", consts.ISO9660_STD_IDENTIFIER)
	}

	logger.V(logging.TRACE).Info("Volume descriptor version", "version", pvd.Version())
	if pvd.Version() != consts.ISO9660_VOLUME_DESC_VERSION {
		logger.Error(nil, "WARNING: Invalid volume descriptor version",
			"actualVersion", pvd.Version(), "expectedVersion", consts.ISO9660_VOLUME_DESC_VERSION)
	}

	logVolumeFields(logger, pvd)

	children, err := pvd.RootDirectoryEntry.GetChildren()
	if err != nil {
		return nil, fmt.Errorf("failed to get children: %w", err)
	}
	logger.V(logging.TRACE).Info("Number of children", "count", len(children))

	return pvd, nil
}

func logVolumeFields(logger logr.Logger, pvd *PrimaryVolumeDescriptor) {
	logger.V(logging.TRACE).Info("System identifier", "systemIdentifier", pvd.SystemIdentifier)
	logger.V(logging.TRACE).Info("Volume identifier", "volumeIdentifier", pvd.VolumeIdentifier)
	logger.V(logging.TRACE).Info("Volume space size", "volumeSpaceSize", pvd.VolumeSpaceSize)
	logger.V(logging.TRACE).Info("Volume set size", "volumeSetSize", pvd.VolumeSetSize)
	logger.V(logging.TRACE).Info("Volume sequence number", "volumeSequenceNumber", pvd.VolumeSequenceNumber)
	logger.V(logging.TRACE).Info("Logical block size", "logicalBlockSize", pvd.LogicalBlockSize)
	logger.V(logging.TRACE).Info("Path table size", "pathTableSize", pvd.PathTableSize())
	logger.V(logging.TRACE).Info("Path table location (L)", "lPathTableLocation", pvd.LPathTableLocation)
	logger.V(logging.TRACE).Info("Path table location (M)", "mPathTableLocation", pvd.MPathTableLocation)
	logger.V(logging.TRACE).Info("Root directory entry", "rootDirectoryEntry", pvd.RootDirectoryEntry)
	logger.V(logging.TRACE).Info("Volume set identifier", "volumeSetIdentifier", pvd.VolumeSetIdentifier)
	logger.V(logging.TRACE).Info("Publisher identifier", "publisherIdentifier", pvd.PublisherIdentifier)
	logger.V(logging.TRACE).Info("Data preparer identifier", "dataPreparerIdentifier", pvd.DataPreparerIdentifier)
	logger.V(logging.TRACE).Info("Application identifier", "applicationIdentifier", pvd.ApplicationIdentifier)
	logger.V(logging.TRACE).Info("Copyright file identifier", "copyrightFileIdentifier", pvd.CopyRightFileIdentifier)
	logger.V(logging.TRACE).Info("Abstract file identifier", "abstractFileIdentifier", pvd.AbstractFileIdentifier)
	logger.V(logging.TRACE).Info("Bibliographic file identifier", "bibliographicFileIdentifier", pvd.BibliographicFileIdentifier)
	logger.V(logging.TRACE).Info("Volume creation date", "volumeCreationDate", pvd.VolumeCreationDate)
	logger.V(logging.TRACE).Info("Volume modification date", "volumeModificationDate", pvd.VolumeModificationDate)
	logger.V(logging.TRACE).Info("Volume expiration date", "volumeExpirationDate", pvd.VolumeExpirationDate)
	logger.V(logging.TRACE).Info("Volume effective date", "volumeEffectiveDate", pvd.VolumeEffectiveDate)
	logger.V(logging.TRACE).Info("File structure version", "fileStructureVersion", pvd.FileStructureVersion)
	logger.V(logging.TRACE).Info("Application use", "applicationUse", strings.TrimSpace(string(pvd.ApplicationUse[:])))
}

// PrimaryVolumeDescriptor represents the primary volume descriptor of an ISO 9660 image.
type PrimaryVolumeDescriptor struct {
	rawData                     [2048]byte                // Raw data from the volume descriptor
	vdType                      VolumeDescriptorType      // Always 1
	standardIdentifier          string                    // Always "CD001"
	volumeDescriptorVersion     int8                      // Always 1
	UnusedField1                [1]byte                   // Unused field should be 0x00
	SystemIdentifier            string                    // Identifier of the system that can act upon the volume
	VolumeIdentifier            string                    // Identifier of the volume
	UnusedField2                [8]byte                   // Unused field should be 0x00
	VolumeSpaceSize             int32                     // Size of the volume in logical blocks
	UnusedField3                [32]byte                  // Unused field should be 0x00
	VolumeSetSize               int16                     // Number of volumes in the volume set
	VolumeSequenceNumber        int16                     // Number of this volume in the volume set
	LogicalBlockSize            int16                     // Size of the logical blocks in bytes
	pathTableSize               int32                     // Size of the path table in bytes
	LPathTableLocation          uint32                    // Location of the path table for the first directory record
	LOptionalPathTableLocation  uint32                    // Location of the optional path table
	MPathTableLocation          uint32                    // Location of the path table for the second directory record
	MOptionalPathTableLocation  uint32                    // Location of the optional path table
	RootDirectoryEntry          *directory.DirectoryEntry // Directory entry for the root directory
	VolumeSetIdentifier         string                    // Identifier of the volume set
	PublisherIdentifier         string                    // Identifier of the publisher
	DataPreparerIdentifier      string                    // Identifier of the data preparer
	ApplicationIdentifier       string                    // Identifier of the application
	CopyRightFileIdentifier     string                    // Identifier of the copyright file
	AbstractFileIdentifier      string                    // Identifier of the abstract file
	BibliographicFileIdentifier string                    // Identifier of the bibliographic file
	VolumeCreationDate          string                    // Date and time the volume was created
	VolumeModificationDate      string                    // Date and time the volume was last modified
	VolumeExpirationDate        string                    // Date and time the volume expires
	VolumeEffectiveDate         string                    // Date and time the volume is effective
	FileStructureVersion        byte                      // Version of the file structure
	UnusedField4                byte                      // Unused field should be 0x00
	ApplicationUse              [512]byte                 // Application-specific data
	UnusedField5                [653]byte                 // Unused field should be 0x00
	pathTable                   []*path.PathTableRecord   // Path Table
	isoFile                     io.ReaderAt               // Reader for the ISO file
	logger                      logr.Logger               // Logger
	useRR                       bool                      // Use RockRidge Extensions
}

// PathTableLocation returns the location of the path table for the primary volume descriptor.
func (pvd *PrimaryVolumeDescriptor) PathTableLocation() uint32 {
	return pvd.LPathTableLocation
}

// PathTableSize returns the size of the path table for the primary volume descriptor.
func (pvd *PrimaryVolumeDescriptor) PathTableSize() int32 {
	return pvd.pathTableSize
}

// PathTable returns the path table for the primary volume descriptor.
func (pvd *PrimaryVolumeDescriptor) PathTable() *[]*path.PathTableRecord {
	if pvd.pathTable == nil {
		pvd.pathTable = make([]*path.PathTableRecord, 0)
	}

	return &pvd.pathTable
}

// Type returns the type of the primary volume descriptor.
func (pvd *PrimaryVolumeDescriptor) Type() VolumeDescriptorType {
	return pvd.vdType
}

// Identifier returns the standard identifier of the primary volume descriptor.
func (pvd *PrimaryVolumeDescriptor) Identifier() string {
	return pvd.standardIdentifier
}

// Version returns the version of the primary volume descriptor.
func (pvd *PrimaryVolumeDescriptor) Version() int8 {
	return pvd.volumeDescriptorVersion
}

// Data returns the raw data of the primary volume descriptor.
func (pvd *PrimaryVolumeDescriptor) Data() [2048]byte {
	return pvd.rawData
}

// Unmarshal parses the given byte slice and populates the PrimaryVolumeDescriptor struct.
func (pvd *PrimaryVolumeDescriptor) Unmarshal(data [consts.ISO9660_SECTOR_SIZE]byte, isoFile io.ReaderAt) (err error) {

	pvd.logger.V(logging.TRACE).Info("Unmarshalling primary volume descriptor", "len", len(data))

	// Save the raw data
	pvd.rawData = data

	rootRecord := directory.NewRecord(pvd.logger)
	err = rootRecord.Unmarshal(data[156:190], isoFile)
	if err != nil {
		return err
	}

	pvd.vdType = VolumeDescriptorType(data[0])
	pvd.standardIdentifier = string(data[1:6])
	pvd.volumeDescriptorVersion = int8(data[6])
	copy(pvd.UnusedField1[:], data[7:8])
	pvd.SystemIdentifier = string(data[8:40])
	pvd.VolumeIdentifier = string(data[40:72])
	copy(pvd.UnusedField2[:], data[72:80])
	pvd.VolumeSpaceSize, err = encoding.UnmarshalInt32LSBMSB(data[80:88])
	if err != nil {
		return err
	}
	copy(pvd.UnusedField3[:], data[88:120])
	pvd.VolumeSetSize, err = encoding.UnmarshalInt16LSBMSB(data[120:124])
	if err != nil {
		return err
	}
	pvd.VolumeSequenceNumber, err = encoding.UnmarshalInt16LSBMSB(data[124:128])
	if err != nil {
		return err
	}
	pvd.LogicalBlockSize, err = encoding.UnmarshalInt16LSBMSB(data[128:132])
	if err != nil {
		return err
	}
	pvd.pathTableSize, err = encoding.UnmarshalInt32LSBMSB(data[132:140])
	if err != nil {
		return err
	}
	pvd.LPathTableLocation = binary.LittleEndian.Uint32(data[140:144])
	pvd.LOptionalPathTableLocation = binary.LittleEndian.Uint32(data[144:148])
	pvd.MPathTableLocation = binary.BigEndian.Uint32(data[148:152])
	pvd.MOptionalPathTableLocation = binary.BigEndian.Uint32(data[152:156])
	pvd.RootDirectoryEntry = directory.NewEntry(rootRecord, isoFile, pvd.useRR, pvd.logger)
	pvd.VolumeSetIdentifier = string(data[190:318])
	pvd.PublisherIdentifier = string(data[318:446])
	pvd.DataPreparerIdentifier = string(data[446:574])
	pvd.ApplicationIdentifier = string(data[574:702])
	pvd.CopyRightFileIdentifier = string(data[702:739])
	pvd.AbstractFileIdentifier = string(data[739:776])
	pvd.BibliographicFileIdentifier = string(data[776:813])
	pvd.VolumeCreationDate = string(data[813:830])
	pvd.VolumeModificationDate = string(data[830:847])
	pvd.VolumeExpirationDate = string(data[847:864])
	pvd.VolumeEffectiveDate = string(data[864:881])
	pvd.FileStructureVersion = data[881]
	pvd.UnusedField4 = data[882]
	copy(pvd.ApplicationUse[:], data[883:1395])
	copy(pvd.UnusedField5[:], data[1395:2048])
	return nil
}
