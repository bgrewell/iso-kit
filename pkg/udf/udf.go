// Package udf implements read support for UDF (ECMA-167 / OSTA UDF)
// filesystems: volume recognition, volume descriptor sequence parsing,
// and file system traversal through the file set descriptor and ICB
// hierarchy. Write support is not yet implemented.
package udf

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bgrewell/iso-kit/pkg/filesystem"
	"github.com/bgrewell/iso-kit/pkg/iso9660/info"
	"github.com/bgrewell/iso-kit/pkg/logging"
	"github.com/bgrewell/iso-kit/pkg/option"
)

// ErrWriteUnsupported is returned by all mutation APIs: UDF support is
// currently read-only.
var ErrWriteUnsupported = errors.New("UDF write support is not implemented")

// IsUDF scans the Volume Recognition Sequence starting at sector 16 for
// an NSR descriptor (NSR02 or NSR03), the marker of an ECMA-167 file
// system. Bridge discs carrying both ISO 9660 and UDF also match.
func IsUDF(reader io.ReaderAt) bool {
	var buf [SectorSize]byte
	for sector := int64(vrsStartSector); sector < vrsStartSector+64; sector++ {
		if _, err := reader.ReadAt(buf[:], sector*SectorSize); err != nil {
			return false
		}
		id := string(buf[1:6])
		switch id {
		case "NSR02", "NSR03":
			return true
		case "BEA01", "BOOT2", "CD001", "CDW02", "TEA01":
			// Part of a recognition sequence; keep scanning. TEA01
			// terminates it, but an NSR must have appeared before that.
			if id == "TEA01" {
				return false
			}
		default:
			return false
		}
	}
	return false
}

// Open parses a UDF filesystem from the reader.
func Open(isoReader io.ReaderAt, opts ...option.OpenOption) (*UDF, error) {
	openOptions := &option.OpenOptions{
		Logger: logging.DefaultLogger(),
	}
	for _, opt := range opts {
		opt(openOptions)
	}
	if openOptions.Logger == nil {
		openOptions.Logger = logging.DefaultLogger()
	}

	if !IsUDF(isoReader) {
		return nil, errors.New("no UDF volume recognition sequence found")
	}

	udf := &UDF{
		reader: isoReader,
		logger: openOptions.Logger,
	}
	if err := udf.parse(); err != nil {
		return nil, err
	}
	return udf, nil
}

// Create is not yet supported for UDF filesystems.
func Create(filename string, opts ...option.CreateOption) (*UDF, error) {
	return nil, ErrWriteUnsupported
}

// UDF is a read-only view of a UDF filesystem.
type UDF struct {
	reader io.ReaderAt
	logger *logging.Logger

	pvd        *PrimaryVolumeDescriptor
	lvd        *LogicalVolumeDescriptor
	partitions map[uint16]*PartitionDescriptor
	fileSet    *FileSetDescriptor
	entries    []*filesystem.FileSystemEntry
}

// readSector reads one 2048-byte sector at the given absolute sector.
func (u *UDF) readSector(sector uint32) ([]byte, error) {
	buf := make([]byte, SectorSize)
	if _, err := u.reader.ReadAt(buf, int64(sector)*SectorSize); err != nil {
		return nil, fmt.Errorf("failed to read sector %d: %w", sector, err)
	}
	return buf, nil
}

// partitionSector translates a logical block within a partition to an
// absolute sector.
func (u *UDF) partitionSector(partition uint16, logicalBlock uint32) (uint32, error) {
	pd, ok := u.partitions[partition]
	if !ok {
		return 0, fmt.Errorf("reference to unknown partition %d", partition)
	}
	return pd.StartingSector + logicalBlock, nil
}

// parse walks the anchor, volume descriptor sequence, file set, and
// directory hierarchy.
func (u *UDF) parse() error {
	anchor, err := u.readSector(anchorSector)
	if err != nil {
		return fmt.Errorf("failed to read anchor volume descriptor pointer: %w", err)
	}
	avdp, err := parseAVDP(anchor)
	if err != nil {
		return err
	}

	// Walk the main VDS, falling back to the reserve on error.
	if err := u.parseVDS(avdp.MainVDS); err != nil {
		u.logger.Debug("Main volume descriptor sequence unusable, trying reserve", "error", err)
		if rerr := u.parseVDS(avdp.ReserveVDS); rerr != nil {
			return fmt.Errorf("failed to parse volume descriptor sequence: %w", err)
		}
	}
	if u.lvd == nil {
		return errors.New("no logical volume descriptor found")
	}
	if len(u.partitions) == 0 {
		return errors.New("no partition descriptor found")
	}
	if u.lvd.LogicalBlockSize != SectorSize {
		return fmt.Errorf("unsupported UDF logical block size %d", u.lvd.LogicalBlockSize)
	}

	// File Set Descriptor via the LVD's contents-use long_ad.
	fsdSector, err := u.partitionSector(u.lvd.FileSetLocation.Partition, u.lvd.FileSetLocation.LogicalBlock)
	if err != nil {
		return err
	}
	fsdData, err := u.readSector(fsdSector)
	if err != nil {
		return err
	}
	u.fileSet, err = parseFSD(fsdData)
	if err != nil {
		return fmt.Errorf("failed to parse file set descriptor: %w", err)
	}

	// Walk the directory hierarchy from the root ICB.
	return u.walkDirectory(u.fileSet.RootDirectoryICB, "", 0)
}

// parseVDS walks a volume descriptor sequence extent.
func (u *UDF) parseVDS(extent ExtentAD) error {
	if extent.Length == 0 {
		return errors.New("empty volume descriptor sequence extent")
	}
	u.partitions = map[uint16]*PartitionDescriptor{}
	sectors := (extent.Length + SectorSize - 1) / SectorSize
	for i := uint32(0); i < sectors; i++ {
		data, err := u.readSector(extent.Location + i)
		if err != nil {
			return err
		}
		tag, err := parseTag(data)
		if err != nil {
			return err
		}
		switch tag.ID {
		case TAG_PRIMARY_VOLUME_DESCRIPTOR:
			u.pvd = parsePVD(data)
		case TAG_PARTITION_DESCRIPTOR:
			pd := parsePD(data)
			u.partitions[pd.PartitionNumber] = pd
		case TAG_LOGICAL_VOLUME_DESCRIPTOR:
			u.lvd = parseLVD(data)
		case TAG_TERMINATING_DESCRIPTOR:
			return nil
		}
	}
	return nil
}

// readICB reads and parses the File Entry at an ICB address.
func (u *UDF) readICB(icb LongAD) (*FileEntry, error) {
	sector, err := u.partitionSector(icb.Partition, icb.LogicalBlock)
	if err != nil {
		return nil, err
	}
	data, err := u.readSector(sector)
	if err != nil {
		return nil, err
	}
	return parseFileEntry(data)
}

// contentSegments converts a file entry's allocation descriptors into
// absolute extent segments.
func (u *UDF) contentSegments(fe *FileEntry, icbPartition uint16) ([]filesystem.ExtentSegment, error) {
	var segments []filesystem.ExtentSegment
	switch fe.AllocationType {
	case 0: // short_ad: blocks within the ICB's own partition
		for _, ad := range parseShortADs(fe.AllocationData) {
			sector, err := u.partitionSector(icbPartition, ad.LogicalBlock)
			if err != nil {
				return nil, err
			}
			segments = append(segments, filesystem.ExtentSegment{Location: sector, Size: ad.Length})
		}
	case 1: // long_ad
		for _, ad := range parseLongADs(fe.AllocationData) {
			sector, err := u.partitionSector(ad.Partition, ad.LogicalBlock)
			if err != nil {
				return nil, err
			}
			segments = append(segments, filesystem.ExtentSegment{Location: sector, Size: ad.Length})
		}
	case 3: // inline data in the file entry
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported allocation descriptor type %d", fe.AllocationType)
	}
	return segments, nil
}

// readContent returns a reader over a file entry's content along with
// its size.
func (u *UDF) readContent(fe *FileEntry, icbPartition uint16) (io.ReaderAt, uint64, error) {
	if fe.AllocationType == 3 {
		data := fe.AllocationData
		if uint64(len(data)) > fe.InformationLength {
			data = data[:fe.InformationLength]
		}
		return &sliceReaderAt{data}, uint64(len(data)), nil
	}
	segments, err := u.contentSegments(fe, icbPartition)
	if err != nil {
		return nil, 0, err
	}
	reader := filesystem.NewMultiExtentReader(u.reader, SectorSize, segments)
	size := uint64(reader.TotalSize())
	if size > fe.InformationLength {
		size = fe.InformationLength
	}
	return reader, size, nil
}

type sliceReaderAt struct{ data []byte }

func (s *sliceReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(s.data)) {
		return 0, io.EOF
	}
	n := copy(p, s.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// walkDirectory recursively builds filesystem entries from a directory
// ICB. depth guards against reference cycles in corrupt images.
func (u *UDF) walkDirectory(icb LongAD, parentPath string, depth int) error {
	if depth > 64 {
		return errors.New("directory hierarchy too deep (possible cycle)")
	}
	fe, err := u.readICB(icb)
	if err != nil {
		return err
	}
	if fe.FileType != FILE_TYPE_DIRECTORY {
		return fmt.Errorf("ICB at %d:%d is not a directory", icb.Partition, icb.LogicalBlock)
	}

	// Directory content: the sequence of file identifier descriptors.
	reader, size, err := u.readContent(fe, icb.Partition)
	if err != nil {
		return err
	}
	dirData := make([]byte, size)
	if size > 0 {
		if _, err := reader.ReadAt(dirData, 0); err != nil && err != io.EOF {
			return fmt.Errorf("failed to read directory content: %w", err)
		}
	}
	fids, err := parseFileIdentifiers(dirData)
	if err != nil {
		return err
	}

	for _, fid := range fids {
		if fid.IsParent() || fid.IsDeleted() || fid.Name == "" {
			continue
		}
		childPath := parentPath + "/" + fid.Name

		childFE, err := u.readICB(fid.ICB)
		if err != nil {
			u.logger.Debug("Skipping unreadable ICB", "path", childPath, "error", err)
			continue
		}

		mode := os.FileMode(posixPermsFromUDF(childFE.Permissions))
		uid, gid := childFE.UID, childFE.GID
		modTime := childFE.ModificationTime

		if fid.IsDirectory() {
			entry := filesystem.NewFileSystemEntry(
				fid.Name, childPath, true, 0, 0, &uid, &gid,
				mode|os.ModeDir, modTime, modTime, nil, nil,
			)
			u.entries = append(u.entries, entry)
			if err := u.walkDirectory(fid.ICB, childPath, depth+1); err != nil {
				return err
			}
			continue
		}

		contentReader, size, err := u.readContent(childFE, fid.ICB.Partition)
		if err != nil {
			return fmt.Errorf("failed to resolve content of %s: %w", childPath, err)
		}
		entry := filesystem.NewFileSystemEntry(
			fid.Name, childPath, false, size, 0, &uid, &gid,
			mode, modTime, modTime, nil, contentReader,
		)
		if childFE.FileType == FILE_TYPE_SYMLINK {
			// Symlink targets are path component sequences in the file
			// content; decode best-effort.
			raw := make([]byte, size)
			if _, err := contentReader.ReadAt(raw, 0); err == nil || err == io.EOF {
				entry.SymlinkTarget = decodePathComponents(raw)
			}
		}
		u.entries = append(u.entries, entry)
	}
	return nil
}

// posixPermsFromUDF converts ECMA-167 file entry permissions (5 bits per
// class: execute, write, read, chattr, delete) into POSIX rwx bits.
func posixPermsFromUDF(perms uint32) uint32 {
	var mode uint32
	// Other: bits 0-4, group: 5-9, owner: 10-14.
	if perms&0x0001 != 0 {
		mode |= 0o001
	}
	if perms&0x0002 != 0 {
		mode |= 0o002
	}
	if perms&0x0004 != 0 {
		mode |= 0o004
	}
	if perms&0x0020 != 0 {
		mode |= 0o010
	}
	if perms&0x0040 != 0 {
		mode |= 0o020
	}
	if perms&0x0080 != 0 {
		mode |= 0o040
	}
	if perms&0x0400 != 0 {
		mode |= 0o100
	}
	if perms&0x0800 != 0 {
		mode |= 0o200
	}
	if perms&0x1000 != 0 {
		mode |= 0o400
	}
	return mode
}

// decodePathComponents decodes an ECMA-167 4/14.16 path component
// sequence into a slash-separated path.
func decodePathComponents(data []byte) string {
	var parts []string
	offset := 0
	prefix := ""
	for offset+4 <= len(data) {
		componentType := data[offset]
		length := int(data[offset+1])
		if offset+4+length > len(data) {
			break
		}
		content := data[offset+4 : offset+4+length]
		switch componentType {
		case 2: // root
			prefix = "/"
		case 3: // parent
			parts = append(parts, "..")
		case 4: // current
			parts = append(parts, ".")
		case 5: // named component
			parts = append(parts, decodeDString(content))
		}
		offset += 4 + length
	}
	return prefix + strings.Join(parts, "/")
}

// --- ISO interface implementation (read-only) ---

func (u *UDF) GetVolumeID() string {
	if u.lvd != nil && u.lvd.LogicalVolumeIdentifier != "" {
		return u.lvd.LogicalVolumeIdentifier
	}
	if u.pvd != nil {
		return u.pvd.VolumeIdentifier
	}
	return ""
}

func (u *UDF) GetSystemID() string { return "" }

func (u *UDF) GetVolumeSetID() string {
	if u.pvd != nil {
		return u.pvd.VolumeSetIdentifier
	}
	return ""
}

func (u *UDF) GetPublisherID() string     { return "" }
func (u *UDF) GetDataPreparerID() string  { return "" }
func (u *UDF) GetApplicationID() string   { return "" }
func (u *UDF) GetCopyrightID() string     { return "" }
func (u *UDF) GetAbstractID() string      { return "" }
func (u *UDF) GetBibliographicID() string { return "" }

func (u *UDF) GetCreationDateTime() time.Time {
	if u.pvd != nil {
		return u.pvd.RecordingDateTime
	}
	return time.Time{}
}

func (u *UDF) GetModificationDateTime() time.Time { return u.GetCreationDateTime() }
func (u *UDF) GetExpirationDateTime() time.Time   { return time.Time{} }
func (u *UDF) GetEffectiveDateTime() time.Time    { return time.Time{} }

func (u *UDF) GetVolumeSize() uint32 {
	var total uint32
	for _, pd := range u.partitions {
		if end := pd.StartingSector + pd.Length; end > total {
			total = end
		}
	}
	return total
}

func (u *UDF) RootDirectoryLocation() uint32 {
	if u.fileSet == nil {
		return 0
	}
	sector, err := u.partitionSector(u.fileSet.RootDirectoryICB.Partition, u.fileSet.RootDirectoryICB.LogicalBlock)
	if err != nil {
		return 0
	}
	return sector
}

func (u *UDF) HasJoliet() bool    { return false }
func (u *UDF) HasRockRidge() bool { return false }
func (u *UDF) HasElTorito() bool  { return false }

func (u *UDF) ListBootEntries() ([]*filesystem.FileSystemEntry, error) { return nil, nil }

func (u *UDF) ListFiles() ([]*filesystem.FileSystemEntry, error) {
	files := make([]*filesystem.FileSystemEntry, 0)
	for _, e := range u.entries {
		if !e.IsDir {
			files = append(files, e)
		}
	}
	return files, nil
}

func (u *UDF) ListDirectories() ([]*filesystem.FileSystemEntry, error) {
	dirs := make([]*filesystem.FileSystemEntry, 0)
	for _, e := range u.entries {
		if e.IsDir {
			dirs = append(dirs, e)
		}
	}
	return dirs, nil
}

func (u *UDF) ReadFile(path string) ([]byte, error) {
	cleaned := "/" + strings.Trim(path, "/")
	for _, e := range u.entries {
		if e.FullPath == cleaned && !e.IsDir {
			return e.GetBytes()
		}
	}
	return nil, fmt.Errorf("file not found: %s", path)
}

func (u *UDF) AddFile(path string, data []byte) error { return ErrWriteUnsupported }
func (u *UDF) RemoveFile(path string) error           { return ErrWriteUnsupported }
func (u *UDF) Save(writer io.WriterAt) error          { return ErrWriteUnsupported }

func (u *UDF) CreateDirectories(path string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", path, err)
	}
	dirs, _ := u.ListDirectories()
	for _, entry := range dirs {
		if err := os.MkdirAll(filepath.Join(path, entry.FullPath), 0755); err != nil {
			return err
		}
	}
	return nil
}

func (u *UDF) Extract(path string) error {
	if err := u.CreateDirectories(path); err != nil {
		return err
	}
	files, _ := u.ListFiles()
	for _, entry := range files {
		outputPath := filepath.Join(path, entry.FullPath)
		if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			return err
		}
		if entry.IsSymlink() {
			if err := os.Remove(outputPath); err != nil && !os.IsNotExist(err) {
				return err
			}
			if err := os.Symlink(entry.SymlinkTarget, outputPath); err != nil {
				return fmt.Errorf("failed to create symlink %s: %w", outputPath, err)
			}
			continue
		}
		data, err := entry.GetBytes()
		if err != nil {
			return err
		}
		if err := os.WriteFile(outputPath, data, entry.Mode.Perm()|0400); err != nil {
			return err
		}
	}
	return nil
}

func (u *UDF) SetLogger(logger *logging.Logger) { u.logger = logger }
func (u *UDF) GetLogger() *logging.Logger       { return u.logger }

func (u *UDF) GetLayout() *info.ISOLayout {
	return &info.ISOLayout{}
}

func (u *UDF) Close() error {
	if f, ok := u.reader.(*os.File); ok {
		return f.Close()
	}
	return nil
}
