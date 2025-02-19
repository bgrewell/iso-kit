package iso9660

import (
	"fmt"
	"github.com/bgrewell/iso-kit/pkg/filesystem"
	"github.com/bgrewell/iso-kit/pkg/iso9660/boot"
	"github.com/bgrewell/iso-kit/pkg/iso9660/consts"
	"github.com/bgrewell/iso-kit/pkg/iso9660/descriptor"
	"github.com/bgrewell/iso-kit/pkg/iso9660/directory"
	"github.com/bgrewell/iso-kit/pkg/iso9660/parser"
	"github.com/bgrewell/iso-kit/pkg/iso9660/systemarea"
	"github.com/bgrewell/iso-kit/pkg/option"
	"github.com/go-logr/logr"
	"io"
	"os"
	"path/filepath"
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
		Logger:                     logr.Discard(),
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
		Contents: saBuf,
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
	if boot.IsElTorito(bootRecord.BootSystemIdentifier) && openOptions.ElToritoEnabled {
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

	// Handle processing volume descriptor
	var filesystemEntries []*filesystem.FileSystemEntry
	var directoryRecords []*directory.DirectoryRecord
	var activeVD descriptor.VolumeDescriptor
	if openOptions.PreferJoliet && len(svds) > 0 {
		// Open the Joliet filesystem
		filesystemEntries, err = p.BuildFileSystemEntries(svds[0].RootDirectoryRecord, false)
		directoryRecords, err = p.WalkDirectoryRecords(svds[0].RootDirectoryRecord)
		activeVD = svds[0]
	} else {
		filesystemEntries, err = p.BuildFileSystemEntries(pvd.RootDirectoryRecord, openOptions.RockRidgeEnabled)
		directoryRecords, err = p.WalkDirectoryRecords(pvd.RootDirectoryRecord)
		activeVD = pvd
	}

	iso := &ISO9660{
		isoReader:         isoReader,
		openOptions:       openOptions,
		systemArea:        sa,
		bootRecord:        bootRecord,
		pvd:               pvd,
		svds:              svds,
		directoryRecords:  directoryRecords,
		filesystemEntries: filesystemEntries,
		activeVD:          activeVD,
		elTorito:          et,
	}

	return iso, nil
}

func Create(filename string, rootPath string, opts ...option.CreateOption) (*ISO9660, error) {
	//TODO implement me
	panic("implement me")
}

type ISO9660 struct {
	isoReader         io.ReaderAt
	openOptions       *option.OpenOptions
	createOptions     *option.CreateOptions
	systemArea        systemarea.SystemArea
	bootRecord        *descriptor.BootRecordDescriptor
	pvd               *descriptor.PrimaryVolumeDescriptor
	svds              []*descriptor.SupplementaryVolumeDescriptor
	activeVD          descriptor.VolumeDescriptor
	directoryRecords  []*directory.DirectoryRecord
	filesystemEntries []*filesystem.FileSystemEntry
	elTorito          *boot.ElTorito
}

func (iso *ISO9660) GetVolumeID() string {
	if iso.activeVD == nil {
		return ""
	}
	return iso.activeVD.VolumeIdentifier()
}

func (iso *ISO9660) GetSystemID() string {
	if iso.activeVD == nil {
		return ""
	}
	return iso.activeVD.SystemIdentifier()
}

func (iso *ISO9660) GetVolumeSize() uint32 {
	return iso.pvd.VolumeSpaceSize
}

func (iso *ISO9660) GetVolumeSetID() string {
	if iso.activeVD == nil {
		return ""
	}
	return iso.activeVD.VolumeSetIdentifier()
}

func (iso *ISO9660) GetPublisherID() string {
	if iso.activeVD == nil {
		return ""
	}
	return iso.activeVD.PublisherIdentifier()
}

func (iso *ISO9660) GetDataPreparerID() string {
	if iso.activeVD == nil {
		return ""
	}
	return iso.activeVD.DataPreparerIdentifier()
}

func (iso *ISO9660) GetApplicationID() string {
	if iso.activeVD == nil {
		return ""
	}
	return iso.activeVD.ApplicationIdentifier()
}

func (iso *ISO9660) GetCopyrightID() string {
	if iso.activeVD == nil {
		return ""
	}
	return iso.activeVD.CopyrightFileIdentifier()
}

func (iso *ISO9660) GetAbstractID() string {
	if iso.activeVD == nil {
		return ""
	}
	return iso.activeVD.AbstractFileIdentifier()
}

func (iso *ISO9660) GetBibliographicID() string {
	if iso.activeVD == nil {
		return ""
	}
	return iso.activeVD.BibliographicFileIdentifier()
}

func (iso *ISO9660) GetCreationDateTime() time.Time {
	if iso.activeVD == nil {
		return time.Time{}
	}
	return iso.activeVD.VolumeCreationDateTime()
}

func (iso *ISO9660) GetModificationDateTime() time.Time {
	if iso.activeVD == nil {
		return time.Time{}
	}
	return iso.activeVD.VolumeModificationDateTime()
}

func (iso *ISO9660) GetExpirationDateTime() time.Time {
	if iso.activeVD == nil {
		return time.Time{}
	}
	return iso.activeVD.VolumeExpirationDateTime()
}

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
	for _, rec := range iso.directoryRecords {
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

func (iso *ISO9660) RootDirectoryLocation() uint32 {
	return iso.activeVD.RootDirectory().LocationOfExtent
}

func (iso *ISO9660) ListBootEntries() ([]*filesystem.FileSystemEntry, error) {
	return iso.elTorito.BuildBootImageEntries()
}

func (iso *ISO9660) ListFiles() ([]*filesystem.FileSystemEntry, error) {
	files := make([]*filesystem.FileSystemEntry, 0)
	for _, entry := range iso.filesystemEntries {
		if !entry.IsDir {
			files = append(files, entry)
		}
	}

	return files, nil
}

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

	// Extract files
	files, err := iso.ListFiles()
	if err != nil {
		return fmt.Errorf("failed to list files: %w", err)
	}
	for _, entry := range files {

		outputPath := filepath.Join(path, entry.FullPath)

		// Ensure parent directories exist
		if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			return fmt.Errorf("failed to create parent directories for %s: %w", outputPath, err)
		}

		// Open the output file for writing
		outFile, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("failed to create file %s: %w", outputPath, err)
		}
		defer outFile.Close()

		// Read the file bytes from the ISO
		data, err := entry.GetBytes()
		if err != nil {
			return fmt.Errorf("failed to read file data for %s: %w", entry.FullPath, err)
		}

		// Write data to the output file
		if _, err := outFile.Write(data); err != nil {
			return fmt.Errorf("failed to write file %s: %w", outputPath, err)
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

func (iso *ISO9660) Save(writer io.Writer) error {
	//TODO implement me
	panic("implement me")
}

func (iso *ISO9660) Close() error {
	if f, ok := iso.isoReader.(*os.File); ok {
		return f.Close()
	}
	return nil
}
