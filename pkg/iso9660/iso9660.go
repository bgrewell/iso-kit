package iso9660

import (
	"cmp"
	"errors"
	"fmt"
	"github.com/bgrewell/iso-kit/pkg/consts"
	"github.com/bgrewell/iso-kit/pkg/filesystem"
	"github.com/bgrewell/iso-kit/pkg/iso9660/boot"
	"github.com/bgrewell/iso-kit/pkg/iso9660/descriptor"
	"github.com/bgrewell/iso-kit/pkg/iso9660/info"
	"github.com/bgrewell/iso-kit/pkg/iso9660/parser"
	"github.com/bgrewell/iso-kit/pkg/iso9660/pathtable"
	"github.com/bgrewell/iso-kit/pkg/iso9660/systemarea"
	"github.com/bgrewell/iso-kit/pkg/logging"
	"github.com/bgrewell/iso-kit/pkg/option"
	"github.com/bgrewell/iso-kit/pkg/version"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Open opens an ISO9660 filesystem from the specified reader.
func Open(isoReader io.ReaderAt, opts ...option.OpenOption) (*ISO9660, error) {

	// Set default open options
	emptyCallback := func(currentFilename string, bytesTransferred int64, totalBytes int64, currentFileNumber int, totalFileCount int) {
	}
	openOptions := &option.OpenOptions{
		ReadOnly:                   true,
		ParseOnOpen:                true,
		PreloadDir:                 true,
		StripVersionInfo:           true,
		RockRidgeEnabled:           true,
		ElToritoEnabled:            true,
		PreferJoliet:               false,
		BootFileExtractLocation:    "[BOOT]",
		ExtractionProgressCallback: emptyCallback,
		Logger:                     logging.DefaultLogger(),
	}

	for _, opt := range opts {
		opt(openOptions)
	}

	// Read the System Area
	saBuf := [consts.ISO9660_SECTOR_SIZE * consts.ISO9660_SYSTEM_AREA_SECTORS]byte{}
	if _, err := isoReader.ReadAt(saBuf[:], 0); err != nil {
		return nil, err
	}
	sa := systemarea.SystemArea{
		Contents:   saBuf,
		ObjectSize: consts.ISO9660_SECTOR_SIZE * consts.ISO9660_SYSTEM_AREA_SECTORS,
	}

	// Create a parser
	p := parser.NewParser(isoReader, openOptions)

	// Read the boot record
	bootRecord, err := p.GetBootRecord()
	if err != nil {
		return nil, err
	}

	// Check for El-Torito boot record
	var et *boot.ElTorito
	if bootRecord != nil && boot.IsElTorito(bootRecord.BootSystemIdentifier) && openOptions.ElToritoEnabled {
		et, err = p.GetElTorito(bootRecord)
		if err != nil {
			return nil, err
		}
	}

	// Read the primary volume descriptor
	pvd, err := p.GetPrimaryVolumeDescriptor()
	if err != nil {
		return nil, err
	}

	// Read the supplementary volume descriptors
	svds, err := p.GetSupplementaryVolumeDescriptors()
	if err != nil {
		return nil, err
	}

	// Read any partition volume descriptors
	partitionvds, err := p.GetVolumePartitionDescriptors()
	if err != nil {
		return nil, err
	}

	// Mark the end of the volume descriptors
	term, err := p.GetVolumeDescriptorSetTerminator()
	if err != nil {
		return nil, err
	}

	// Handle walking the pvd directory records
	pvd.DirectoryRecords, err = p.WalkDirectoryRecords(pvd.RootDirectoryRecord)
	if err != nil {
		return nil, err
	}

	// Handle walking the svd directory records
	for _, svd := range svds {
		svd.DirectoryRecords, err = p.WalkDirectoryRecords(svd.RootDirectoryRecord)
		if err != nil {
			return nil, err
		}
	}

	// Handle processing volume descriptor
	var filesystemEntries []*filesystem.FileSystemEntry
	if openOptions.PreferJoliet && len(svds) > 0 {
		// Open the Joliet filesystem
		filesystemEntries, err = p.BuildFileSystemEntries(svds[0].RootDirectoryRecord, false)
	} else {
		filesystemEntries, err = p.BuildFileSystemEntries(pvd.RootDirectoryRecord, openOptions.RockRidgeEnabled)
	}

	// Handle the path tables
	tables, err := p.GetPathTables(pvd)
	if err != nil {
		return nil, err
	}
	for _, svd := range svds {
		svdTables, err := p.GetPathTables(svd)
		if err != nil {
			return nil, err
		}
		tables = append(tables, svdTables...)
	}

	volumeDescSet := &descriptor.VolumeDescriptorSet{
		Primary:       pvd,
		Supplementary: svds,
		Partition:     partitionvds,
		Boot:          bootRecord,
		Terminator:    term,
	}

	iso := &ISO9660{
		isoReader:           isoReader,
		openOptions:         openOptions, //TODO: Work on making composite options that limit users ability to create based on context but have a single set behind the scenes
		systemArea:          sa,
		volumeDescriptorSet: volumeDescSet,
		pathTables:          tables,
		filesystemEntries:   filesystemEntries,
		elTorito:            et,
		logger:              openOptions.Logger,
		isPacked:            true,
	}

	return iso, nil
}

func Create(name string, opts ...option.CreateOption) (*ISO9660, error) {
	// Set default create options
	createOptions := &option.CreateOptions{
		Preparer: fmt.Sprintf("iso-kit %s %s (%s) %s", version.Version(), version.Revision(), version.Branch(), version.Date()),
	}

	for _, opt := range opts {
		opt(createOptions)
	}

	//// Create a root directory record
	//rootDir := &directory.DirectoryRecord{
	//	FileIdentifier:                "\x00",
	//	LengthOfDirectoryRecord:       0,
	//	ExtendedAttributeRecordLength: 0,
	//	LocationOfExtent:              0, // Updated when writing directory structures
	//	DataLength:                    0,
	//	RecordingDateAndTime:          time.Now(),
	//	FileFlags:                     directory.FileFlags{Directory: true},
	//	VolumeSequenceNumber:          1, // Volume sequence should be set
	//}

	//// 1: Create system area (First 16 sectors reserved, boot record might go here)
	//sa := systemarea.SystemArea{}
	//
	//// 2: Create volume descriptor set
	//pvd := descriptor.PrimaryVolumeDescriptor{
	//	VolumeDescriptorHeader: descriptor.VolumeDescriptorHeader{
	//		VolumeDescriptorType:    descriptor.TYPE_PRIMARY_DESCRIPTOR,
	//		StandardIdentifier:      consts.ISO9660_STD_IDENTIFIER,
	//		VolumeDescriptorVersion: consts.ISO9660_VOLUME_DESC_VERSION,
	//	},
	//	PrimaryVolumeDescriptorBody: descriptor.PrimaryVolumeDescriptorBody{
	//		SystemIdentifier:              "",
	//		VolumeIdentifier:              name,
	//		VolumeSpaceSize:               0, // Set later
	//		VolumeSetSize:                 1, // Single volume
	//		VolumeSequenceNumber:          1,
	//		LogicalBlockSize:              consts.ISO9660_SECTOR_SIZE,
	//		RootDirectoryRecord:           rootDir,
	//		VolumeSetIdentifier:           "",
	//		PublisherIdentifier:           "",
	//		DataPreparerIdentifier:        createOptions.Preparer,
	//		ApplicationIdentifier:         "",
	//		VolumeCreationDateAndTime:     time.Now(),
	//		VolumeModificationDateAndTime: time.Now(),
	//		VolumeExpirationDateAndTime:   time.Now(),
	//		VolumeEffectiveDateAndTime:    time.Now(),
	//		FileStructureVersion:          1,
	//	},
	//}
	//
	//// 2.2: Create supplementary volume descriptor (if Joliet is enabled)
	//var svds []*descriptor.SupplementaryVolumeDescriptor
	//if createOptions.JolietEnabled {
	//	svd := descriptor.SupplementaryVolumeDescriptor{
	//		VolumeDescriptorHeader: descriptor.VolumeDescriptorHeader{
	//			VolumeDescriptorType:    descriptor.TYPE_SUPPLEMENTARY_DESCRIPTOR,
	//			StandardIdentifier:      consts.ISO9660_STD_IDENTIFIER,
	//			VolumeDescriptorVersion: consts.ISO9660_VOLUME_DESC_VERSION,
	//		},
	//		SupplementaryVolumeDescriptorBody: descriptor.SupplementaryVolumeDescriptorBody{
	//			VolumeFlags:                   0,
	//			SystemIdentifier:              "",
	//			VolumeIdentifier:              name,
	//			VolumeSpaceSize:               [8]byte{}, // Needs to be set later
	//			RootDirectoryRecord:           rootDir,
	//			VolumeSetIdentifier:           "",
	//			PublisherIdentifier:           "",
	//			DataPreparerIdentifier:        createOptions.Preparer,
	//			ApplicationIdentifier:         "",
	//			VolumeCreationDateAndTime:     time.Now(),
	//			VolumeModificationDateAndTime: time.Now(),
	//			VolumeExpirationDateAndTime:   time.Now(),
	//			VolumeEffectiveDateAndTime:    time.Now(),
	//			FileStructureVersion:          1,
	//		},
	//	}
	//	// Copy the Joliet escape sequence (%/@ for Level 3)
	//	copy(svd.SupplementaryVolumeDescriptorBody.EscapeSequences[:], []byte(consts.JOLIET_LEVEL_3_ESCAPE))
	//	svds = append(svds, &svd)
	//}

	// 2.3: Create volume partition descriptor(s) (not used often in basic ISO9660)
	//var pvds []*descriptor.VolumePartitionDescriptor
	//
	//// 2.4: Create boot record (only needed for bootable ISOs)
	//br := descriptor.BootRecordDescriptor{}

	// 3: Initialize path tables (Will need to be generated)
	// Placeholder for path table setup

	// 4: Create directory records (Root directory will be updated dynamically)
	// Placeholder for directory handling

	// Build ISO structure
	iso := &ISO9660{
		//createOptions: createOptions,
		//systemArea:    sa,
		//bootRecord:    &br,
		//pvd:           &pvd,
		//svds:          svds,
		//partitionvds:  pvds,
		//logger:        createOptions.Logger,
	}

	return iso, nil
}

// ISO9660 represents an ISO9660 filesystem.
type ISO9660 struct {
	// ISO Reader
	isoReader io.ReaderAt
	// Open Options
	openOptions *option.OpenOptions
	// Create Options
	createOptions *option.CreateOptions
	// System Area
	systemArea systemarea.SystemArea
	// Volume Descriptor Set
	volumeDescriptorSet *descriptor.VolumeDescriptorSet
	// Path Tables
	pathTables []*pathtable.PathTable
	// ElTorito Boot Record
	elTorito *boot.ElTorito
	// FileSystemEntries
	filesystemEntries []*filesystem.FileSystemEntry
	// Logger
	logger *logging.Logger
	// isPacked represents if the ISO9660 filesystem is packed and ready to write to disk
	isPacked bool
}

func (iso *ISO9660) preferJoliet() bool {
	return iso.openOptions != nil && iso.openOptions.PreferJoliet
}

// GetVolumeID returns the volume identifier of the ISO9660 filesystem.
func (iso *ISO9660) GetVolumeID() string {
	if iso.volumeDescriptorSet == nil {
		return ""
	}
	if iso.preferJoliet() && iso.volumeDescriptorSet.Supplementary != nil {
		return iso.volumeDescriptorSet.Supplementary[0].VolumeIdentifier()
	}
	if iso.volumeDescriptorSet.Primary == nil {
		return ""
	}
	return iso.volumeDescriptorSet.Primary.VolumeIdentifier()
}

// GetSystemID returns the system identifier of the ISO9660 filesystem.
func (iso *ISO9660) GetSystemID() string {
	if iso.volumeDescriptorSet == nil {
		return ""
	}
	if iso.preferJoliet() && iso.volumeDescriptorSet.Supplementary != nil {
		return iso.volumeDescriptorSet.Supplementary[0].SystemIdentifier()
	}
	if iso.volumeDescriptorSet.Primary == nil {
		return ""
	}
	return iso.volumeDescriptorSet.Primary.SystemIdentifier()
}

// GetVolumeSize returns the size of the ISO9660 filesystem.
func (iso *ISO9660) GetVolumeSize() uint32 {
	if iso.volumeDescriptorSet == nil || iso.volumeDescriptorSet.Primary == nil {
		return 0
	}
	return iso.volumeDescriptorSet.Primary.VolumeSpaceSize
}

// GetVolumeSetID returns the volume set identifier of the ISO9660 filesystem.
func (iso *ISO9660) GetVolumeSetID() string {
	if iso.volumeDescriptorSet == nil {
		return ""
	}
	if iso.preferJoliet() && iso.volumeDescriptorSet.Supplementary != nil {
		return iso.volumeDescriptorSet.Supplementary[0].VolumeSetIdentifier()
	}
	if iso.volumeDescriptorSet.Primary == nil {
		return ""
	}
	return iso.volumeDescriptorSet.Primary.VolumeSetIdentifier()
}

// GetPublisherID returns the publisher identifier of the ISO9660 filesystem.
func (iso *ISO9660) GetPublisherID() string {
	if iso.volumeDescriptorSet == nil {
		return ""
	}
	if iso.preferJoliet() && iso.volumeDescriptorSet.Supplementary != nil {
		return iso.volumeDescriptorSet.Supplementary[0].PublisherIdentifier()
	}
	if iso.volumeDescriptorSet.Primary == nil {
		return ""
	}
	return iso.volumeDescriptorSet.Primary.PublisherIdentifier()
}

// GetDataPreparerID returns the data preparer identifier of the ISO9660 filesystem.
func (iso *ISO9660) GetDataPreparerID() string {
	if iso.volumeDescriptorSet == nil {
		return ""
	}
	if iso.preferJoliet() && iso.volumeDescriptorSet.Supplementary != nil {
		return iso.volumeDescriptorSet.Supplementary[0].DataPreparerIdentifier()
	}
	if iso.volumeDescriptorSet.Primary == nil {
		return ""
	}
	return iso.volumeDescriptorSet.Primary.DataPreparerIdentifier()
}

// GetApplicationID returns the application identifier of the ISO9660 filesystem.
func (iso *ISO9660) GetApplicationID() string {
	if iso.volumeDescriptorSet == nil {
		return ""
	}
	if iso.preferJoliet() && iso.volumeDescriptorSet.Supplementary != nil {
		return iso.volumeDescriptorSet.Supplementary[0].ApplicationIdentifier()
	}
	if iso.volumeDescriptorSet.Primary == nil {
		return ""
	}
	return iso.volumeDescriptorSet.Primary.ApplicationIdentifier()
}

// GetCopyrightID returns the copyright identifier of the ISO9660 filesystem.
func (iso *ISO9660) GetCopyrightID() string {
	if iso.volumeDescriptorSet == nil {
		return ""
	}
	if iso.preferJoliet() && iso.volumeDescriptorSet.Supplementary != nil {
		return iso.volumeDescriptorSet.Supplementary[0].CopyrightFileIdentifier()
	}
	if iso.volumeDescriptorSet.Primary == nil {
		return ""
	}
	return iso.volumeDescriptorSet.Primary.CopyrightFileIdentifier()
}

// GetAbstractID returns the abstract identifier of the ISO9660 filesystem.
func (iso *ISO9660) GetAbstractID() string {
	if iso.volumeDescriptorSet == nil {
		return ""
	}
	if iso.preferJoliet() && iso.volumeDescriptorSet.Supplementary != nil {
		return iso.volumeDescriptorSet.Supplementary[0].AbstractFileIdentifier()
	}
	if iso.volumeDescriptorSet.Primary == nil {
		return ""
	}
	return iso.volumeDescriptorSet.Primary.AbstractFileIdentifier()
}

// GetBibliographicID returns the bibliographic identifier of the ISO9660 filesystem.
func (iso *ISO9660) GetBibliographicID() string {
	if iso.volumeDescriptorSet == nil {
		return ""
	}
	if iso.preferJoliet() && iso.volumeDescriptorSet.Supplementary != nil {
		return iso.volumeDescriptorSet.Supplementary[0].BibliographicFileIdentifier()
	}
	if iso.volumeDescriptorSet.Primary == nil {
		return ""
	}
	return iso.volumeDescriptorSet.Primary.BibliographicFileIdentifier()
}

// GetCreationDateTime returns the creation date and time of the ISO9660 filesystem.
func (iso *ISO9660) GetCreationDateTime() time.Time {
	if iso.volumeDescriptorSet == nil {
		return time.Time{}
	}
	if iso.preferJoliet() && iso.volumeDescriptorSet.Supplementary != nil {
		return iso.volumeDescriptorSet.Supplementary[0].VolumeCreationDateTime()
	}
	if iso.volumeDescriptorSet.Primary == nil {
		return time.Time{}
	}
	return iso.volumeDescriptorSet.Primary.VolumeCreationDateTime()
}

// GetModificationDateTime returns the modification date and time of the ISO9660 filesystem.
func (iso *ISO9660) GetModificationDateTime() time.Time {
	if iso.volumeDescriptorSet == nil {
		return time.Time{}
	}
	if iso.preferJoliet() && iso.volumeDescriptorSet.Supplementary != nil {
		return iso.volumeDescriptorSet.Supplementary[0].VolumeModificationDateTime()
	}
	if iso.volumeDescriptorSet.Primary == nil {
		return time.Time{}
	}
	return iso.volumeDescriptorSet.Primary.VolumeModificationDateTime()
}

// GetExpirationDateTime returns the expiration date and time of the ISO9660 filesystem.
func (iso *ISO9660) GetExpirationDateTime() time.Time {
	if iso.volumeDescriptorSet == nil {
		return time.Time{}
	}
	if iso.preferJoliet() && iso.volumeDescriptorSet.Supplementary != nil {
		return iso.volumeDescriptorSet.Supplementary[0].VolumeExpirationDateTime()
	}
	if iso.volumeDescriptorSet.Primary == nil {
		return time.Time{}
	}
	return iso.volumeDescriptorSet.Primary.VolumeExpirationDateTime()
}

// GetEffectiveDateTime returns the effective date and time of the ISO9660 filesystem.
func (iso *ISO9660) GetEffectiveDateTime() time.Time {
	if iso.volumeDescriptorSet == nil {
		return time.Time{}
	}
	if iso.preferJoliet() && iso.volumeDescriptorSet.Supplementary != nil {
		return iso.volumeDescriptorSet.Supplementary[0].VolumeEffectiveDateTime()
	}
	if iso.volumeDescriptorSet.Primary == nil {
		return time.Time{}
	}
	return iso.volumeDescriptorSet.Primary.VolumeEffectiveDateTime()
}

// HasJoliet returns true if the ISO9660 filesystem has Joliet extensions.
func (iso *ISO9660) HasJoliet() bool {
	if iso.volumeDescriptorSet == nil {
		return false
	}
	for _, svd := range iso.volumeDescriptorSet.Supplementary {
		if svd.HasJoliet() {
			return true
		}
	}
	return false
}

// HasRockRidge returns true if the ISO9660 filesystem has Rock Ridge extensions.
func (iso *ISO9660) HasRockRidge() bool {
	if iso.volumeDescriptorSet == nil || iso.volumeDescriptorSet.Primary == nil {
		return false
	}
	return iso.volumeDescriptorSet.Primary.HasRockRidge()
}

// HasElTorito returns true if the ISO9660 filesystem has El Torito boot extensions.
func (iso *ISO9660) HasElTorito() bool {
	return iso.elTorito != nil
}

// RootDirectoryLocation returns the location of the root directory in the ISO9660 filesystem.
func (iso *ISO9660) RootDirectoryLocation() uint32 {
	if iso.volumeDescriptorSet == nil {
		return 0
	}
	if iso.preferJoliet() && iso.volumeDescriptorSet.Supplementary != nil {
		return iso.volumeDescriptorSet.Supplementary[0].RootDirectoryRecord.LocationOfExtent
	}
	if iso.volumeDescriptorSet.Primary == nil || iso.volumeDescriptorSet.Primary.RootDirectoryRecord == nil {
		return 0
	}
	return iso.volumeDescriptorSet.Primary.RootDirectoryRecord.LocationOfExtent
}

// ListBootEntries returns a list of all boot entries in the ISO9660 filesystem.
func (iso *ISO9660) ListBootEntries() ([]*filesystem.FileSystemEntry, error) {
	if iso.elTorito == nil {
		return nil, nil
	}
	return iso.elTorito.BuildBootImageEntries()
}

// ListFiles returns a list of all files in the ISO9660 filesystem.
func (iso *ISO9660) ListFiles() ([]*filesystem.FileSystemEntry, error) {
	files := make([]*filesystem.FileSystemEntry, 0)
	for _, entry := range iso.filesystemEntries {
		if !entry.IsDir {
			files = append(files, entry)
		}
	}

	return files, nil
}

// ListDirectories returns a list of all directories in the ISO9660 filesystem.
func (iso *ISO9660) ListDirectories() ([]*filesystem.FileSystemEntry, error) {
	dirs := make([]*filesystem.FileSystemEntry, 0)
	for _, entry := range iso.filesystemEntries {
		if entry.IsDir {
			dirs = append(dirs, entry)
		}
	}

	return dirs, nil
}

func (iso *ISO9660) ReadFile(path string) ([]byte, error) {
	//TODO implement me
	panic("implement me")
}

func (iso *ISO9660) AddFile(path string, data []byte) error {
	//TODO implement me
	panic("implement me")
}

func (iso *ISO9660) RemoveFile(path string) error {
	//TODO implement me
	panic("implement me")
}

// CreateDirectories creates all directories from the ISO in the specified path.
func (iso *ISO9660) CreateDirectories(path string) error {
	// Ensure output directory exists
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", path, err)
	}

	// Iterate over directory FileSystemEntries and create directories
	dirs, err := iso.ListDirectories()
	if err != nil {
		return fmt.Errorf("failed to list directories: %w", err)
	}
	for _, entry := range dirs {
		dirPath := filepath.Join(path, entry.FullPath)
		if err := os.MkdirAll(dirPath, entry.Mode); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dirPath, err)
		}
	}

	return nil
}

// Extract extracts all files and directories from the ISO to the specified path.
func (iso *ISO9660) Extract(path string) error {
	// Create all directories first
	if err := iso.CreateDirectories(path); err != nil {
		return err
	}

	// Extract El Torito boot images if enabled
	if iso.elTorito != nil && iso.openOptions.ElToritoEnabled {
		err := iso.elTorito.ExtractBootImages(iso.isoReader, filepath.Join(path, iso.openOptions.BootFileExtractLocation))
		if err != nil {
			return fmt.Errorf("failed to extract El Torito boot images: %w", err)
		}
	}

	// Get list of files to extract
	files, err := iso.ListFiles()
	if err != nil {
		return fmt.Errorf("failed to list files: %w", err)
	}

	totalFiles := len(files)

	// Extract files
	for i, entry := range files {
		outputPath := filepath.Join(path, entry.FullPath)

		// Ensure parent directories exist
		if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			return fmt.Errorf("failed to create parent directories for %s: %w", outputPath, err)
		}

		// if the option to strip version info is enabled, enhanced and rr are not enabled then strip the version info
		if iso.openOptions.StripVersionInfo && !iso.openOptions.RockRidgeEnabled && !iso.openOptions.PreferJoliet {
			outputPath = strings.TrimSuffix(outputPath, ";1")
		}

		if err := iso.extractFile(entry, outputPath, i+1, totalFiles); err != nil {
			return err
		}
	}

	return nil
}

func (iso *ISO9660) extractFile(entry *filesystem.FileSystemEntry, outputPath string, fileNum, totalFiles int) error {
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", outputPath, err)
	}
	defer outFile.Close()

	startOffset := int64(entry.Location) * int64(consts.ISO9660_SECTOR_SIZE)
	size := int64(entry.Size)
	bufferSize := 4096
	buffer := make([]byte, bufferSize)

	var bytesTransferred int64
	for bytesTransferred < size {
		bytesToRead := bufferSize
		if remaining := size - bytesTransferred; remaining < int64(bufferSize) {
			bytesToRead = int(remaining)
		}

		n, err := entry.ReadAt(buffer[:bytesToRead], startOffset+bytesTransferred)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read file %s from ISO: %w", entry.FullPath, err)
		}

		if n == 0 {
			break
		}

		if _, err := outFile.Write(buffer[:n]); err != nil {
			return fmt.Errorf("failed to write to file %s: %w", outputPath, err)
		}

		bytesTransferred += int64(n)

		if iso.openOptions != nil && iso.openOptions.ExtractionProgressCallback != nil {
			iso.openOptions.ExtractionProgressCallback(outputPath, bytesTransferred, size, fileNum, totalFiles)
		}
	}

	if err := os.Chmod(outputPath, entry.Mode); err != nil {
		return fmt.Errorf("failed to set permissions on %s: %w", outputPath, err)
	}

	if !entry.ModTime.IsZero() {
		if err := os.Chtimes(outputPath, entry.ModTime, entry.ModTime); err != nil {
			return fmt.Errorf("failed to set timestamps on %s: %w", outputPath, err)
		}
	}

	return nil
}

// SetLogger sets the logger for the ISO9660 filesystem.
func (iso *ISO9660) SetLogger(logger *logging.Logger) {
	iso.logger = logger
}

// GetLogger returns the logger for the ISO9660 filesystem.
func (iso *ISO9660) GetLogger() *logging.Logger {
	return iso.logger
}

// GetLayout returns the layout information for the ISO9660 filesystem.
func (iso *ISO9660) GetLayout() *info.ISOLayout {
	objects := iso.GetObjects()

	return &info.ISOLayout{
		Objects: objects,
	}
}

func (iso *ISO9660) GetObjects() []info.ImageObject {
	var objects []info.ImageObject

	for _, objs := range []([]info.ImageObject){
		iso.systemArea.GetObjects(),
		iso.volumeDescriptorSet.Primary.GetObjects(),
		iso.volumeDescriptorSet.Terminator.GetObjects(),
	} {
		objects = append(objects, objs...)
	}

	if iso.volumeDescriptorSet.Boot != nil {
		objects = append(objects, iso.volumeDescriptorSet.Boot.GetObjects()...)
	}

	if iso.volumeDescriptorSet.Supplementary != nil {
		for _, svd := range iso.volumeDescriptorSet.Supplementary {
			objects = append(objects, svd.GetObjects()...)
		}
	}

	if iso.volumeDescriptorSet.Partition != nil {
		for _, pvd := range iso.volumeDescriptorSet.Partition {
			objects = append(objects, pvd.GetObjects()...)
		}
	}

	if iso.pathTables != nil {
		for _, pt := range iso.pathTables {
			objects = append(objects, pt.GetObjects()...)
		}
	}

	if iso.elTorito != nil {
		objects = append(objects, iso.elTorito.GetObjects()...)
	}
	return objects
}

func (iso *ISO9660) Save(writer io.WriterAt) error {
	// Ensure the ISO is packed and all objects have been assigned locations
	if !iso.isPacked {
		// TODO: Make packing automatic
		return errors.New("iso is not packed, cannot save")
	}

	// Get all objects
	objects := iso.GetObjects()

	// Sort objects by offset before writing
	slices.SortFunc(objects, func(a, b info.ImageObject) int {
		return cmp.Compare(a.Offset(), b.Offset())
	})

	// Write each object at its assigned offset
	for _, obj := range objects {
		// Get raw data for the object
		data, err := obj.Marshal()
		if err != nil {
			return fmt.Errorf("failed to marshal object %s: %w", obj.Name(), err)
		}

		// Write data at the correct offset
		_, err = writer.WriteAt(data, obj.Offset())
		if err != nil {
			return fmt.Errorf("failed to write object %s at offset %d: %w", obj.Name(), obj.Offset(), err)
		}
	}

	return nil
}

// Close closes the ISO9660 filesystem.
func (iso *ISO9660) Close() error {
	if f, ok := iso.isoReader.(*os.File); ok {
		return f.Close()
	}
	return nil
}

func writeDescriptor(writer io.WriterAt, descriptor descriptor.VolumeDescriptor, offset int64) error {
	data, err := descriptor.Marshal()
	if err != nil {
		return err
	}

	n, err := writer.WriteAt(data[:], offset)
	if err != nil {
		return err
	}
	if n != len(data) {
		return errors.New("failed to write descriptor")
	}

	return nil
}
