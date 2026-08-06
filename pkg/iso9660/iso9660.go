package iso9660

import (
	"cmp"
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
	"github.com/bgrewell/iso-kit/pkg/iso9660/tree"
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

	// Build the mutable directory tree from the parsed entries. It backs
	// the file APIs (ReadFile/AddFile/RemoveFile) and, once modified,
	// becomes the source of truth for Save.
	root := tree.NewRoot()
	for _, entry := range filesystemEntries {
		if entry.IsDir {
			node, err := root.AddDirectory(entry.FullPath)
			if err != nil {
				return nil, fmt.Errorf("failed to build directory tree: %w", err)
			}
			node.SetMode(entry.Mode)
			node.SetModTime(entry.ModTime)
		} else {
			_, err := root.AddExistingFile(entry.FullPath, isoReader, entry.Location, entry.Size, entry.Mode, entry.ModTime)
			if err != nil {
				return nil, fmt.Errorf("failed to build directory tree: %w", err)
			}
		}
	}

	iso := &ISO9660{
		isoReader:           isoReader,
		openOptions:         openOptions, //TODO: Work on making composite options that limit users ability to create based on context but have a single set behind the scenes
		systemArea:          sa,
		volumeDescriptorSet: volumeDescSet,
		pathTables:          tables,
		filesystemEntries:   filesystemEntries,
		root:                root,
		elTorito:            et,
		logger:              openOptions.Logger,
		isPacked:            true,
	}

	return iso, nil
}

// Create builds a new, empty ISO9660 filesystem in memory. Files and
// directories are added with AddFile, AddDirectory, and AddLocalDirectory;
// Save then lays out and writes the complete image.
func Create(name string, opts ...option.CreateOption) (*ISO9660, error) {
	// Set default create options
	createOptions := &option.CreateOptions{
		Preparer: fmt.Sprintf("iso-kit %s %s (%s) %s", version.Version(), version.Revision(), version.Branch(), version.Date()),
		Logger:   logging.DefaultLogger(),
	}

	for _, opt := range opts {
		opt(createOptions)
	}
	if createOptions.Logger == nil {
		createOptions.Logger = logging.DefaultLogger()
	}

	now := time.Now()
	root := tree.NewRoot()

	// The root directory record is a placeholder here; Pack replaces it
	// with one carrying the assigned extent location and size.
	rootRecord := &directory.DirectoryRecord{
		LocationOfExtent:       0,
		DataLength:             0,
		RecordingDateAndTime:   now,
		FileFlags:              directory.FileFlags{Directory: true},
		VolumeSequenceNumber:   1,
		LengthOfFileIdentifier: 1,
		FileIdentifier:         "\x00",
	}

	pvd := &descriptor.PrimaryVolumeDescriptor{
		VolumeDescriptorHeader: descriptor.VolumeDescriptorHeader{
			VolumeDescriptorType:    descriptor.TYPE_PRIMARY_DESCRIPTOR,
			StandardIdentifier:      consts.ISO9660_STD_IDENTIFIER,
			VolumeDescriptorVersion: consts.ISO9660_VOLUME_DESC_VERSION,
		},
		PrimaryVolumeDescriptorBody: descriptor.PrimaryVolumeDescriptorBody{
			VolumeIdentifier:              name,
			VolumeSetSize:                 1,
			VolumeSequenceNumber:          1,
			LogicalBlockSize:              consts.ISO9660_SECTOR_SIZE,
			RootDirectoryRecord:           rootRecord,
			DataPreparerIdentifier:        createOptions.Preparer,
			VolumeCreationDateAndTime:     now,
			VolumeModificationDateAndTime: now,
			FileStructureVersion:          1,
			Logger:                        createOptions.Logger,
		},
	}

	iso := &ISO9660{
		createOptions: createOptions,
		systemArea: systemarea.SystemArea{
			ObjectSize: consts.ISO9660_SECTOR_SIZE * consts.ISO9660_SYSTEM_AREA_SECTORS,
		},
		volumeDescriptorSet: &descriptor.VolumeDescriptorSet{
			Primary:    pvd,
			Terminator: descriptor.NewVolumeDescriptorSetTerminator(),
		},
		root:     root,
		logger:   createOptions.Logger,
		isDirty:  true,
		isPacked: false,
	}

	if createOptions.JolietEnabled {
		iso.logger.Info("WARNING: Joliet output is not yet supported; the image will be written without a supplementary volume descriptor")
	}

	if createOptions.RootDir != "" {
		if err := iso.AddLocalDirectory(createOptions.RootDir, "/"); err != nil {
			return nil, err
		}
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
	// Mutable directory tree; the source of truth for filesystem state
	root *tree.Node
	// Sector assignments produced by Pack
	layout *packLayout
	// Logger
	logger *logging.Logger
	// isPacked represents if the ISO9660 filesystem is packed and ready to write to disk
	isPacked bool
	// isDirty is set when the tree has been modified since Open/Create,
	// meaning Save must rebuild the image layout rather than re-serialize
	// parsed structures at their original offsets
	isDirty bool
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

// refreshEntriesFromTree rebuilds the flat entry list from the mutable
// tree when the cached parse-time list has been invalidated by a
// modification. Entries built this way have Location 0 with a reader whose
// offset 0 is the start of the file, and carry no directory record.
func (iso *ISO9660) refreshEntriesFromTree() error {
	if iso.filesystemEntries != nil || iso.root == nil {
		return nil
	}
	entries := make([]*filesystem.FileSystemEntry, 0)
	err := iso.root.Walk(func(node *tree.Node) error {
		var reader io.ReaderAt
		if !node.IsDir() {
			r, err := node.ContentReaderAt(consts.ISO9660_SECTOR_SIZE)
			if err != nil {
				return err
			}
			reader = r
		}
		entries = append(entries, filesystem.NewFileSystemEntry(
			node.Name(),
			node.FullPath(),
			node.IsDir(),
			node.Size(),
			0,
			nil,
			nil,
			node.Mode(),
			node.ModTime(),
			node.ModTime(),
			nil,
			reader,
		))
		return nil
	})
	if err != nil {
		return err
	}
	iso.filesystemEntries = entries
	return nil
}

// ListFiles returns a list of all files in the ISO9660 filesystem.
func (iso *ISO9660) ListFiles() ([]*filesystem.FileSystemEntry, error) {
	if err := iso.refreshEntriesFromTree(); err != nil {
		return nil, err
	}
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
	if err := iso.refreshEntriesFromTree(); err != nil {
		return nil, err
	}
	dirs := make([]*filesystem.FileSystemEntry, 0)
	for _, entry := range iso.filesystemEntries {
		if entry.IsDir {
			dirs = append(dirs, entry)
		}
	}

	return dirs, nil
}

// ReadFile returns the content of the file at the given path. The path is
// slash-separated relative to the image root; an ISO 9660 version suffix
// (";1") on the final component is optional.
func (iso *ISO9660) ReadFile(path string) ([]byte, error) {
	if iso.root == nil {
		return nil, errors.New("no filesystem is loaded")
	}
	node := iso.root.Lookup(path)
	if node == nil {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	if node.IsDir() {
		return nil, fmt.Errorf("path is a directory: %s", path)
	}
	return node.ReadData(consts.ISO9660_SECTOR_SIZE)
}

// AddFile adds a file with the given content at the given path, creating
// parent directories as needed. An existing file at the path is replaced.
func (iso *ISO9660) AddFile(path string, data []byte) error {
	if iso.root == nil {
		return errors.New("no filesystem is loaded")
	}
	if _, err := iso.root.AddFile(path, data); err != nil {
		return err
	}
	iso.markDirty()
	return nil
}

// AddDirectory creates an empty directory at the given path, creating
// parent directories as needed.
func (iso *ISO9660) AddDirectory(path string) error {
	if iso.root == nil {
		return errors.New("no filesystem is loaded")
	}
	if _, err := iso.root.AddDirectory(path); err != nil {
		return err
	}
	iso.markDirty()
	return nil
}

// AddLocalDirectory recursively imports a directory from the local
// filesystem into the image at targetPath. File content is read into
// memory at Save time via the pending-data mechanism; large trees are read
// eagerly here, so callers importing very large directories should expect
// proportional memory use until streaming import support lands.
func (iso *ISO9660) AddLocalDirectory(sourcePath, targetPath string) error {
	if iso.root == nil {
		return errors.New("no filesystem is loaded")
	}
	sourcePath = filepath.Clean(sourcePath)
	err := filepath.Walk(sourcePath, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return err
		}
		isoPath := targetPath
		if rel != "." {
			isoPath = strings.TrimSuffix(targetPath, "/") + "/" + filepath.ToSlash(rel)
		}
		if fi.IsDir() {
			node, err := iso.root.AddDirectory(isoPath)
			if err != nil {
				return err
			}
			node.SetMode(fi.Mode().Perm())
			node.SetModTime(fi.ModTime())
			return nil
		}
		if !fi.Mode().IsRegular() {
			iso.logger.Info("Skipping non-regular file", "path", path)
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}
		node, err := iso.root.AddFile(isoPath, data)
		if err != nil {
			return err
		}
		node.SetMode(fi.Mode().Perm())
		node.SetModTime(fi.ModTime())
		return nil
	})
	if err != nil {
		return err
	}
	iso.markDirty()
	return nil
}

// RemoveFile removes the file at the given path. Removing a directory is
// an error; use RemoveDirectory instead.
func (iso *ISO9660) RemoveFile(path string) error {
	if iso.root == nil {
		return errors.New("no filesystem is loaded")
	}
	node := iso.root.Lookup(path)
	if node == nil {
		return fmt.Errorf("file not found: %s", path)
	}
	if node.IsDir() {
		return fmt.Errorf("path is a directory (use RemoveDirectory): %s", path)
	}
	if err := iso.root.Remove(path); err != nil {
		return err
	}
	iso.markDirty()
	return nil
}

// RemoveDirectory removes the directory at the given path along with its
// entire subtree.
func (iso *ISO9660) RemoveDirectory(path string) error {
	if iso.root == nil {
		return errors.New("no filesystem is loaded")
	}
	node := iso.root.Lookup(path)
	if node == nil {
		return fmt.Errorf("directory not found: %s", path)
	}
	if !node.IsDir() {
		return fmt.Errorf("path is a file (use RemoveFile): %s", path)
	}
	if err := iso.root.Remove(path); err != nil {
		return err
	}
	iso.markDirty()
	return nil
}

// markDirty records that the tree has diverged from the parsed image, so
// Save must rebuild the layout, and invalidates the flat entry cache.
func (iso *ISO9660) markDirty() {
	iso.isDirty = true
	iso.isPacked = false
	iso.filesystemEntries = nil
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

// Save writes the complete image to the writer. An image opened from disk
// and never modified is re-serialized at its original offsets; a created
// or modified image is packed (sector layout assigned) and rebuilt from
// the directory tree.
func (iso *ISO9660) Save(writer io.WriterAt) error {
	if iso.isDirty || iso.isoReader == nil {
		return iso.saveRebuild(writer)
	}

	// Pristine passthrough: re-serialize parsed structures at their
	// original byte offsets.
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

// saveRebuild lays out and writes a complete image from the directory
// tree: system area, volume descriptors, path tables, directory extents,
// and file extents. Every allocated sector is written in full, so the
// resulting image is exactly VolumeSpaceSize sectors long.
func (iso *ISO9660) saveRebuild(writer io.WriterAt) error {
	if iso.root == nil {
		return errors.New("no filesystem is loaded")
	}

	if iso.volumeDescriptorSet.Boot != nil || iso.elTorito != nil {
		iso.logger.Info("WARNING: El Torito boot structures are not yet supported in rebuilt images; the output will not be bootable")
	}
	if len(iso.volumeDescriptorSet.Supplementary) > 0 {
		iso.logger.Info("WARNING: Joliet output is not yet supported; the supplementary volume descriptor is dropped from the rebuilt image")
	}

	if err := iso.Pack(); err != nil {
		return fmt.Errorf("failed to pack image: %w", err)
	}

	// 1. System area (sectors 0-15).
	if _, err := writer.WriteAt(iso.systemArea.Contents[:], 0); err != nil {
		return fmt.Errorf("failed to write system area: %w", err)
	}

	// 2. Primary volume descriptor.
	pvdBytes, err := iso.volumeDescriptorSet.Primary.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal primary volume descriptor: %w", err)
	}
	if _, err := writer.WriteAt(pvdBytes, int64(iso.layout.pvdSector)*consts.ISO9660_SECTOR_SIZE); err != nil {
		return fmt.Errorf("failed to write primary volume descriptor: %w", err)
	}

	// 3. Volume descriptor set terminator.
	term := iso.volumeDescriptorSet.Terminator
	if term == nil {
		term = descriptor.NewVolumeDescriptorSetTerminator()
		iso.volumeDescriptorSet.Terminator = term
	}
	term.ObjectLocation = int64(iso.layout.terminatorSector) * consts.ISO9660_SECTOR_SIZE
	term.VolumeDescriptorSetTerminatorBody.ObjectSize = consts.ISO9660_SECTOR_SIZE
	termBytes, err := term.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal volume descriptor set terminator: %w", err)
	}
	if _, err := writer.WriteAt(termBytes, term.ObjectLocation); err != nil {
		return fmt.Errorf("failed to write volume descriptor set terminator: %w", err)
	}

	// 4. Path tables (L then M), zero-padded to whole sectors.
	ptL, ptM, err := iso.buildPathTables()
	if err != nil {
		return err
	}
	for _, pt := range []*pathtable.PathTable{ptL, ptM} {
		data, err := pt.Marshal()
		if err != nil {
			return fmt.Errorf("failed to marshal path table: %w", err)
		}
		padded := make([]byte, sectorsFor(uint32(len(data)))*consts.ISO9660_SECTOR_SIZE)
		copy(padded, data)
		if _, err := writer.WriteAt(padded, pt.Offset()); err != nil {
			return fmt.Errorf("failed to write path table: %w", err)
		}
	}
	iso.pathTables = []*pathtable.PathTable{ptL, ptM}

	// 5. Directory extents in breadth-first order.
	for _, dir := range iso.layout.dirs {
		data, err := marshalDirectoryExtent(dir)
		if err != nil {
			return err
		}
		if _, err := writer.WriteAt(data, int64(dir.PackedLocation)*consts.ISO9660_SECTOR_SIZE); err != nil {
			return fmt.Errorf("failed to write directory extent for %q: %w", dir.FullPath(), err)
		}
	}

	// 6. File extents, each zero-padded to whole sectors.
	for _, file := range iso.layout.files {
		if err := iso.writeFileExtent(writer, file); err != nil {
			return err
		}
	}

	iso.isDirty = false
	return nil
}

// writeFileExtent streams a file's content to its packed location and
// zero-fills the remainder of its final sector.
func (iso *ISO9660) writeFileExtent(writer io.WriterAt, node *tree.Node) error {
	if node.Size() == 0 {
		return nil
	}
	content, err := node.Content(consts.ISO9660_SECTOR_SIZE)
	if err != nil {
		return err
	}
	targetOffset := int64(node.PackedLocation) * consts.ISO9660_SECTOR_SIZE
	written, err := io.Copy(io.NewOffsetWriter(writer, targetOffset), content)
	if err != nil {
		return fmt.Errorf("failed to write content of %q: %w", node.FullPath(), err)
	}
	if written != int64(node.Size()) {
		return fmt.Errorf("short write for %q: wrote %d of %d bytes", node.FullPath(), written, node.Size())
	}
	if pad := int64(sectorsFor(node.Size()))*consts.ISO9660_SECTOR_SIZE - written; pad > 0 {
		if _, err := writer.WriteAt(make([]byte, pad), targetOffset+written); err != nil {
			return fmt.Errorf("failed to pad extent of %q: %w", node.FullPath(), err)
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
