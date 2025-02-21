package iso9660

import (
	"errors"
	"fmt"
	"github.com/bgrewell/iso-kit/pkg/consts"
	"github.com/bgrewell/iso-kit/pkg/filesystem"
	"github.com/bgrewell/iso-kit/pkg/iso9660/boot"
	"github.com/bgrewell/iso-kit/pkg/iso9660/descriptor"
	"github.com/bgrewell/iso-kit/pkg/iso9660/directory"
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

	layout := info.NewISOLayout()

	// Read the System Area
	saBuf := [consts.ISO9660_SECTOR_SIZE * consts.ISO9660_SYSTEM_AREA_SECTORS]byte{}
	if _, err := isoReader.ReadAt(saBuf[:], 0); err != nil {
		return nil, err
	}
	sa := systemarea.SystemArea{
		Contents: saBuf,
	}
	layout.SystemAreaOffset = 0
	layout.SystemAreaLength = consts.ISO9660_SYSTEM_AREA_SECTORS * consts.ISO9660_SECTOR_SIZE

	// Create a parser
	p := parser.NewParser(isoReader, layout, openOptions)

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
	_, _ = p.GetVolumeDescriptorSetTerminator()

	// Handle walking the pvd directory records
	pvdDirectoryRecords, err := p.WalkDirectoryRecords(pvd.RootDirectoryRecord)
	if err != nil {
		return nil, err
	}

	// Handle walking the svd directory records
	var svdDirectoryRecords []*directory.DirectoryRecord
	if len(svds) > 0 {
		svdDirectoryRecords, err = p.WalkDirectoryRecords(svds[0].RootDirectoryRecord)
		if err != nil {
			return nil, err
		}
	}

	// Handle processing volume descriptor
	var filesystemEntries []*filesystem.FileSystemEntry
	var activeVD descriptor.VolumeDescriptor
	var activeDirectoryRecords = &pvdDirectoryRecords
	if openOptions.PreferJoliet && len(svds) > 0 {
		// Open the Joliet filesystem
		filesystemEntries, err = p.BuildFileSystemEntries(svds[0].RootDirectoryRecord, false)
		activeDirectoryRecords = &svdDirectoryRecords
		activeVD = svds[0]
	} else {
		filesystemEntries, err = p.BuildFileSystemEntries(pvd.RootDirectoryRecord, openOptions.RockRidgeEnabled)
		activeVD = pvd
	}

	// Handle the path tables
	pvdLPathTable, err := pathtable.NewPathTable(isoReader, pvd.LocationOfTypeLPathTable, int(pvd.PathTableSize), true)
	if err != nil {
		return nil, err
	}
	pvdMPathTable, err := pathtable.NewPathTable(isoReader, pvd.LocationOfTypeMPathTable, int(pvd.PathTableSize), false)
	if err != nil {
		return nil, err
	}
	var svdLPathTable, svdMPathTable *pathtable.PathTable
	if len(svds) > 0 {
		svdLPathTable, err = pathtable.NewPathTable(isoReader, svds[0].LocationOfTypeLPathTable, int(svds[0].PathTableSize), true)
		if err != nil {
			return nil, err
		}
		svdMPathTable, err = pathtable.NewPathTable(isoReader, svds[0].LocationOfTypeMPathTable, int(svds[0].PathTableSize), false)
		if err != nil {
			return nil, err
		}
	}

	iso := &ISO9660{
		isoReader:              isoReader,
		openOptions:            openOptions,
		systemArea:             sa,
		bootRecord:             bootRecord,
		pvd:                    pvd,
		svds:                   svds,
		partitionvds:           partitionvds,
		pvdDirectoryRecords:    pvdDirectoryRecords,
		svdDirectoryRecords:    svdDirectoryRecords,
		activeDirectoryRecords: activeDirectoryRecords,
		pvdLPathTable:          pvdLPathTable,
		pvdMPathTable:          pvdMPathTable,
		svdLPathTable:          svdLPathTable,
		svdMPathTable:          svdMPathTable,
		filesystemEntries:      filesystemEntries,
		activeVD:               activeVD,
		elTorito:               et,
		layout:                 layout,
		logger:                 openOptions.Logger,
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

	// Create a root directory record
	rootDir := &directory.DirectoryRecord{
		FileIdentifier:                "\x00",
		LengthOfDirectoryRecord:       0,
		ExtendedAttributeRecordLength: 0,
		LocationOfExtent:              0, // Updated when writing directory structures
		DataLength:                    0,
		RecordingDateAndTime:          time.Now(),
		FileFlags:                     directory.FileFlags{Directory: true},
		VolumeSequenceNumber:          1, // Volume sequence should be set
	}

	// 1: Create system area (First 16 sectors reserved, boot record might go here)
	sa := systemarea.SystemArea{}

	// 2: Create volume descriptor set
	pvd := descriptor.PrimaryVolumeDescriptor{
		VolumeDescriptorHeader: descriptor.VolumeDescriptorHeader{
			VolumeDescriptorType:    descriptor.TYPE_PRIMARY_DESCRIPTOR,
			StandardIdentifier:      consts.ISO9660_STD_IDENTIFIER,
			VolumeDescriptorVersion: consts.ISO9660_VOLUME_DESC_VERSION,
		},
		PrimaryVolumeDescriptorBody: descriptor.PrimaryVolumeDescriptorBody{
			SystemIdentifier:              "",
			VolumeIdentifier:              name,
			VolumeSpaceSize:               0, // Set later
			VolumeSetSize:                 1, // Single volume
			VolumeSequenceNumber:          1,
			LogicalBlockSize:              consts.ISO9660_SECTOR_SIZE,
			RootDirectoryRecord:           rootDir,
			VolumeSetIdentifier:           "",
			PublisherIdentifier:           "",
			DataPreparerIdentifier:        createOptions.Preparer,
			ApplicationIdentifier:         "",
			VolumeCreationDateAndTime:     time.Now(),
			VolumeModificationDateAndTime: time.Now(),
			VolumeExpirationDateAndTime:   time.Now(),
			VolumeEffectiveDateAndTime:    time.Now(),
			FileStructureVersion:          1,
		},
	}

	// 2.2: Create supplementary volume descriptor (if Joliet is enabled)
	var svds []*descriptor.SupplementaryVolumeDescriptor
	if createOptions.JolietEnabled {
		svd := descriptor.SupplementaryVolumeDescriptor{
			VolumeDescriptorHeader: descriptor.VolumeDescriptorHeader{
				VolumeDescriptorType:    descriptor.TYPE_SUPPLEMENTARY_DESCRIPTOR,
				StandardIdentifier:      consts.ISO9660_STD_IDENTIFIER,
				VolumeDescriptorVersion: consts.ISO9660_VOLUME_DESC_VERSION,
			},
			SupplementaryVolumeDescriptorBody: descriptor.SupplementaryVolumeDescriptorBody{
				VolumeFlags:                   0,
				SystemIdentifier:              "",
				VolumeIdentifier:              name,
				VolumeSpaceSize:               [8]byte{}, // Needs to be set later
				RootDirectoryRecord:           rootDir,
				VolumeSetIdentifier:           "",
				PublisherIdentifier:           "",
				DataPreparerIdentifier:        createOptions.Preparer,
				ApplicationIdentifier:         "",
				VolumeCreationDateAndTime:     time.Now(),
				VolumeModificationDateAndTime: time.Now(),
				VolumeExpirationDateAndTime:   time.Now(),
				VolumeEffectiveDateAndTime:    time.Now(),
				FileStructureVersion:          1,
			},
		}
		// Copy the Joliet escape sequence (%/@ for Level 3)
		copy(svd.SupplementaryVolumeDescriptorBody.EscapeSequences[:], []byte(consts.JOLIET_LEVEL_3_ESCAPE))
		svds = append(svds, &svd)
	}

	// 2.3: Create volume partition descriptor(s) (not used often in basic ISO9660)
	var pvds []*descriptor.VolumePartitionDescriptor

	// 2.4: Create boot record (only needed for bootable ISOs)
	br := descriptor.BootRecordDescriptor{}

	// 3: Initialize path tables (Will need to be generated)
	// Placeholder for path table setup

	// 4: Create directory records (Root directory will be updated dynamically)
	// Placeholder for directory handling

	// Build ISO structure
	iso := &ISO9660{
		createOptions: createOptions,
		systemArea:    sa,
		bootRecord:    &br,
		pvd:           &pvd,
		svds:          svds,
		partitionvds:  pvds,
		logger:        createOptions.Logger,
	}

	return iso, nil
}

// ISO9660 represents an ISO9660 filesystem.
// TODO: Volume Descriptors should be replaced with a Volume Descriptor Set
type ISO9660 struct {
	// ISO Reader
	isoReader io.ReaderAt
	// Open Options
	openOptions *option.OpenOptions
	// Create Options
	createOptions *option.CreateOptions
	// System Area
	systemArea systemarea.SystemArea
	// Boot Record Descriptor
	bootRecord *descriptor.BootRecordDescriptor
	// Partition Volume Descriptor(s)
	partitionvds []*descriptor.VolumePartitionDescriptor
	// Primary Volume Descriptor
	pvd *descriptor.PrimaryVolumeDescriptor
	// Supplementary Volume Descriptors
	svds []*descriptor.SupplementaryVolumeDescriptor
	// Pointer to the preferred Volume Descriptor
	activeVD descriptor.VolumeDescriptor
	// PVD Directory Records
	pvdDirectoryRecords []*directory.DirectoryRecord
	// SVD Directory Records
	svdDirectoryRecords []*directory.DirectoryRecord
	// Pointer to the preferred Directory Records
	activeDirectoryRecords *[]*directory.DirectoryRecord
	// pvdLPathTable is a pointer to the pvd's little-endian path table.
	pvdLPathTable *pathtable.PathTable
	// pvdMPathTable is a pointer to the pvd's big-endian path table.
	pvdMPathTable *pathtable.PathTable
	// svdLPathTable is a pointer to the svd's little-endian path table.
	svdLPathTable *pathtable.PathTable
	// svdMPathTable is a pointer to the svd's big-endian path table.
	svdMPathTable *pathtable.PathTable
	// FileSystemEntries
	filesystemEntries []*filesystem.FileSystemEntry
	// ElTorito Boot Record
	elTorito *boot.ElTorito
	// ISO Layout Information
	layout *info.ISOLayout
	// Logger
	logger *logging.Logger
}

// GetVolumeID returns the volume identifier of the ISO9660 filesystem.
func (iso *ISO9660) GetVolumeID() string {
	if iso.activeVD == nil {
		return ""
	}
	return iso.activeVD.VolumeIdentifier()
}

// GetSystemID returns the system identifier of the ISO9660 filesystem.
func (iso *ISO9660) GetSystemID() string {
	if iso.activeVD == nil {
		return ""
	}
	return iso.activeVD.SystemIdentifier()
}

// GetVolumeSize returns the size of the ISO9660 filesystem.
func (iso *ISO9660) GetVolumeSize() uint32 {
	return iso.pvd.VolumeSpaceSize
}

// GetVolumeSetID returns the volume set identifier of the ISO9660 filesystem.
func (iso *ISO9660) GetVolumeSetID() string {
	if iso.activeVD == nil {
		return ""
	}
	return iso.activeVD.VolumeSetIdentifier()
}

// GetPublisherID returns the publisher identifier of the ISO9660 filesystem.
func (iso *ISO9660) GetPublisherID() string {
	if iso.activeVD == nil {
		return ""
	}
	return iso.activeVD.PublisherIdentifier()
}

// GetDataPreparerID returns the data preparer identifier of the ISO9660 filesystem.
func (iso *ISO9660) GetDataPreparerID() string {
	if iso.activeVD == nil {
		return ""
	}
	return iso.activeVD.DataPreparerIdentifier()
}

// GetApplicationID returns the application identifier of the ISO9660 filesystem.
func (iso *ISO9660) GetApplicationID() string {
	if iso.activeVD == nil {
		return ""
	}
	return iso.activeVD.ApplicationIdentifier()
}

// GetCopyrightID returns the copyright identifier of the ISO9660 filesystem.
func (iso *ISO9660) GetCopyrightID() string {
	if iso.activeVD == nil {
		return ""
	}
	return iso.activeVD.CopyrightFileIdentifier()
}

// GetAbstractID returns the abstract identifier of the ISO9660 filesystem.
func (iso *ISO9660) GetAbstractID() string {
	if iso.activeVD == nil {
		return ""
	}
	return iso.activeVD.AbstractFileIdentifier()
}

// GetBibliographicID returns the bibliographic identifier of the ISO9660 filesystem.
func (iso *ISO9660) GetBibliographicID() string {
	if iso.activeVD == nil {
		return ""
	}
	return iso.activeVD.BibliographicFileIdentifier()
}

// GetCreationDateTime returns the creation date and time of the ISO9660 filesystem.
func (iso *ISO9660) GetCreationDateTime() time.Time {
	if iso.activeVD == nil {
		return time.Time{}
	}
	return iso.activeVD.VolumeCreationDateTime()
}

// GetModificationDateTime returns the modification date and time of the ISO9660 filesystem.
func (iso *ISO9660) GetModificationDateTime() time.Time {
	if iso.activeVD == nil {
		return time.Time{}
	}
	return iso.activeVD.VolumeModificationDateTime()
}

// GetExpirationDateTime returns the expiration date and time of the ISO9660 filesystem.
func (iso *ISO9660) GetExpirationDateTime() time.Time {
	if iso.activeVD == nil {
		return time.Time{}
	}
	return iso.activeVD.VolumeExpirationDateTime()
}

// GetEffectiveDateTime returns the effective date and time of the ISO9660 filesystem.
func (iso *ISO9660) GetEffectiveDateTime() time.Time {
	if iso.activeVD == nil {
		return time.Time{}
	}
	return iso.activeVD.VolumeEffectiveDateTime()
}

// HasJoliet returns true if the ISO9660 filesystem has Joliet extensions.
func (iso *ISO9660) HasJoliet() bool {
	if iso.activeVD == nil {
		return false
	}
	return iso.activeVD.HasJoliet()
}

// HasRockRidge returns true if the ISO9660 filesystem has Rock Ridge extensions.
func (iso *ISO9660) HasRockRidge() bool {
	for _, rec := range *iso.activeDirectoryRecords {
		if rec.RockRidge != nil {
			return true
		}
	}
	return false
}

// HasElTorito returns true if the ISO9660 filesystem has El Torito boot extensions.
func (iso *ISO9660) HasElTorito() bool {
	return iso.elTorito != nil
}

// RootDirectoryLocation returns the location of the root directory in the ISO9660 filesystem.
func (iso *ISO9660) RootDirectoryLocation() uint32 {
	return iso.activeVD.RootDirectory().LocationOfExtent
}

// ListBootEntries returns a list of all boot entries in the ISO9660 filesystem.
func (iso *ISO9660) ListBootEntries() ([]*filesystem.FileSystemEntry, error) {
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
			outputPath = strings.TrimRight(outputPath, ";1")
		}

		// Open output file for writing
		outFile, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("failed to create file %s: %w", outputPath, err)
		}
		defer outFile.Close()

		// Stream the file from the ISO
		startOffset := int64(entry.Location) * int64(consts.ISO9660_SECTOR_SIZE)
		size := int64(entry.Size)
		bufferSize := 4096 // 4KB buffer
		buffer := make([]byte, bufferSize)

		var bytesTransferred int64
		for bytesTransferred < size {
			// Read chunk from ISO
			bytesToRead := bufferSize
			if remaining := size - bytesTransferred; remaining < int64(bufferSize) {
				bytesToRead = int(remaining)
			}

			n, err := entry.ReadAt(buffer[:bytesToRead], startOffset+bytesTransferred)
			if err != nil && err != io.EOF {
				return fmt.Errorf("failed to read file %s from ISO: %w", entry.FullPath, err)
			}

			if n == 0 {
				break // Reached EOF
			}

			// Write chunk to file
			if _, err := outFile.Write(buffer[:n]); err != nil {
				return fmt.Errorf("failed to write to file %s: %w", outputPath, err)
			}

			// Update bytes transferred
			bytesTransferred += int64(n)

			// Invoke progress callback
			if iso.openOptions.ExtractionProgressCallback != nil {
				iso.openOptions.ExtractionProgressCallback(outputPath, bytesTransferred, size, i+1, totalFiles)
			}
		}

		// Set correct file permissions
		if err := os.Chmod(outputPath, entry.Mode); err != nil {
			return fmt.Errorf("failed to set permissions on %s: %w", outputPath, err)
		}

		// Set timestamps
		if !entry.ModTime.IsZero() {
			if err := os.Chtimes(outputPath, entry.ModTime, entry.ModTime); err != nil {
				return fmt.Errorf("failed to set timestamps on %s: %w", outputPath, err)
			}
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
	return iso.layout
}

func (iso *ISO9660) Save(writer io.WriterAt) error {

	sectorSize := int64(consts.ISO9660_SECTOR_SIZE)
	saOffset := int64(0)

	// Calculate offsets for descriptors
	pvdSize := sectorSize
	bootSize := int64(0)
	if iso.bootRecord != nil {
		bootSize = sectorSize
	}
	svdSize := int64(len(iso.svds)) * sectorSize
	ptvdSize := int64(len(iso.partitionvds)) * sectorSize

	pvdOffset := saOffset + consts.ISO9660_SYSTEM_AREA_SECTORS*sectorSize
	bootOffset := pvdOffset + pvdSize
	svdOffset := bootOffset + bootSize
	ptvdOffset := svdOffset + svdSize
	termOffset := ptvdOffset + ptvdSize

	type descriptorSetEntry struct {
		descriptor descriptor.VolumeDescriptor
		offset     int64
	}
	descriptorSet := []*descriptorSetEntry{
		{descriptor: iso.pvd, offset: pvdOffset},
	}
	if iso.bootRecord != nil {
		descriptorSet = append(descriptorSet,
			&descriptorSetEntry{descriptor: iso.bootRecord, offset: bootOffset},
		)
	}
	for i, svd := range iso.svds {
		descriptorSet = append(descriptorSet,
			&descriptorSetEntry{descriptor: svd, offset: svdOffset + int64(i)*sectorSize},
		)
	}
	for i, ptvd := range iso.partitionvds {
		descriptorSet = append(descriptorSet,
			&descriptorSetEntry{descriptor: ptvd, offset: ptvdOffset + int64(i)*sectorSize},
		)
	}
	descriptorSet = append(descriptorSet,
		&descriptorSetEntry{descriptor: descriptor.NewVolumeDescriptorSetTerminator(), offset: termOffset})

	// Write system area
	_, err := writer.WriteAt(iso.systemArea.Contents[:], 0)
	if err != nil {
		return err
	}

	// Write descriptor set
	for _, entry := range descriptorSet {
		if err = writeDescriptor(writer, entry.descriptor, entry.offset); err != nil {
			return err
		}
	}

	// Write path tables according to their location in the volume descriptors
	err = iso.writePathTables(writer)
	if err != nil {
		return err
	}

	// Write directory records

	//pathTableOffset := svdOffset + (len(iso.svds) * sectorSize)
	//// Directory Record offsets should be
	//
	//

	//// TODO: Clean up all of the offset calculations, instead this should be calculated somewhere
	////       else and the locations should just be used to write the data or this should be wrapped
	////       nicely in a function.
	//// Write volume descriptor set
	//pvdOffset := int64(16 * sectorSize)
	//err = writeDescriptor(writer, iso.pvd, pvdOffset)
	//if err != nil {
	//	return err
	//}
	//
	//svdOffset := pvdOffset + int64(1*sectorSize)
	//for i, svd := range iso.svds {
	//	err = writeDescriptor(writer, svd, svdOffset)
	//	if err != nil {
	//		return err
	//	}
	//	svdOffset = svdOffset+int64((i+1)*sectorSize)
	//}
	//
	//ptvdOffset := svdOffset
	//for i, pvd := range iso.partitionvds {
	//	if err = writeDescriptor(writer, pvd, ptvdOffset); err != nil {
	//		return err
	//	}
	//	ptvdOffset = ptvdOffset+int64((i+1)*sectorSize)
	//}
	//
	//bootOffset := ptvdOffset
	//if iso.bootRecord != nil {
	//	if err = writeDescriptor(writer, iso.bootRecord, bootOffset); err != nil {
	//		return err
	//	}
	//	bootOffset = bootOffset + int64(1*sectorSize)
	//}
	//
	//tr := descriptor.NewVolumeDescriptorSetTerminator()
	//if err := writeDescriptor(writer, tr, bootOffset); err != nil {
	//	return err
	//}

	//// 3: Write path tables (Little & Big Endian versions)
	//if err = iso.writePathTables(writer); err != nil {
	//	return err
	//}
	//
	//// 4: Write directory records (Root & Subdirectories)
	//totalRecords := append(iso.pvdDirectoryRecords, iso.svdDirectoryRecords...)
	//for i, dr := range totalRecords {
	//	drOffset :=
	//}
	//
	//
	//// 5: Write file contents (Ensuring correct logical block placement)
	//if err := iso.writeFileData(writer); err != nil {
	//	return err
	//}
	//
	//// 6: Align to sector size (Padding to 2048-byte boundaries)
	//if err := padToSector(writer); err != nil {
	//	return err
	//}

	return nil
}

// Close closes the ISO9660 filesystem.
func (iso *ISO9660) Close() error {
	if f, ok := iso.isoReader.(*os.File); ok {
		return f.Close()
	}
	return nil
}

// writePathTables writes the path tables to the ISO9660 filesystem.
func (iso *ISO9660) writePathTables(writer io.WriterAt) error {
	if iso.pvd == nil {
		return errors.New("PVD is missing")
	}

	buf, err := iso.pvdLPathTable.Marshal(true)
	if err != nil {
		return err
	}

	if _, err = writer.WriteAt(buf, int64(iso.pvd.LocationOfTypeLPathTable)*consts.ISO9660_SECTOR_SIZE); err != nil {
		return err
	}

	buf, err = iso.pvdMPathTable.Marshal(false)
	if err != nil {
		return err
	}

	if _, err = writer.WriteAt(buf, int64(iso.pvd.LocationOfTypeMPathTable)*consts.ISO9660_SECTOR_SIZE); err != nil {
		return err
	}

	if iso.svdLPathTable != nil {
		buf, err = iso.svdLPathTable.Marshal(true)
		if err != nil {
			return err
		}

		if _, err = writer.WriteAt(buf, int64(iso.svds[0].LocationOfTypeLPathTable)*consts.ISO9660_SECTOR_SIZE); err != nil {
			return err
		}
	}

	if iso.svdMPathTable != nil {
		buf, err = iso.svdMPathTable.Marshal(false)
		if err != nil {
			return err
		}

		if _, err = writer.WriteAt(buf, int64(iso.svds[0].LocationOfTypeMPathTable)*consts.ISO9660_SECTOR_SIZE); err != nil {
			return err
		}
	}

	return nil
}

func (iso *ISO9660) writeDirectoryRecords(writer io.WriterAt) error {

	//var rootDirOffset uint32
	//
	//// TODO: Write PVD Directory records starting with the root
	//rootDirOffset = iso.pvd.RootDirectoryRecord.LocationOfExtent

	// TODO: Write SVD Directory records starting with the root

	//sectorSize := consts.ISO9660_SECTOR_SIZE
	//
	//// Root Directory
	//rootDirData, err := iso.
	//if err != nil {
	//	return err
	//}
	//if _, err := writer.Write(rootDirData[:]); err != nil {
	//	return err
	//}
	//
	//// Write Subdirectories
	//for _, dir := range iso.directories {
	//	dirData, err := dir.Marshal()
	//	if err != nil {
	//		return err
	//	}
	//
	//	// Ensure correct sector alignment
	//	padding := sectorSize - (len(dirData) % sectorSize)
	//	if padding < sectorSize {
	//		dirData = append(dirData, make([]byte, padding)...)
	//	}
	//
	//	if _, err := writer.Write(dirData[:]); err != nil {
	//		return err
	//	}
	//}
	//
	//return nil
	return errors.New("not implemented")
}

func (iso *ISO9660) writeFileData(writer io.Writer) error {
	//sectorSize := consts.ISO9660_SECTOR_SIZE
	//
	//for _, file := range iso. {
	//	fileData, err := file.ReadData()
	//	if err != nil {
	//		return err
	//	}
	//
	//	if _, err := writer.Write(fileData); err != nil {
	//		return err
	//	}
	//
	//	// Align to sector size
	//	padding := sectorSize - (len(fileData) % sectorSize)
	//	if padding < sectorSize {
	//		if _, err := writer.Write(make([]byte, padding)); err != nil {
	//			return err
	//		}
	//	}
	//}
	//
	//return nil
	return errors.New("not implemented")
}

func padToSector(writer io.Writer) error {
	//sectorSize := consts.ISO9660_SECTOR_SIZE
	//
	//// Get current file position
	//offset, err := writer.Seek(0, io.SeekCurrent)
	//if err != nil {
	//	return err
	//}
	//
	//// Compute padding
	//padding := sectorSize - (offset % sectorSize)
	//if padding < sectorSize {
	//	_, err := writer.Write(make([]byte, padding))
	//	return err
	//}
	//
	//return nil
	return errors.New("not implemented")
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

func writePathTable(writer io.Writer, location uint32, littleEndian bool) error {
	//if location == 0 {
	//	return nil // Skip if no path table is set
	//}
	//
	//pathTable := generatePathTable(littleEndian) // Implement the path table generator
	//data := pathTable.Marshal()
	//
	//_, err := writer.Write(data)
	//return err
	return errors.New("not implemented")
}
