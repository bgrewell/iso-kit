package iso9660

import (
	"fmt"
	"time"

	"github.com/bgrewell/iso-kit/pkg/consts"
	"github.com/bgrewell/iso-kit/pkg/iso9660/directory"
	"github.com/bgrewell/iso-kit/pkg/iso9660/pathtable"
	"github.com/bgrewell/iso-kit/pkg/iso9660/tree"
)

const (
	// Fixed portion of a directory record preceding the file identifier:
	// length(1) + xattr(1) + extent(8) + data length(8) + timestamp(7) +
	// flags(1) + unit size(1) + gap size(1) + volume sequence(4) + id length(1).
	directoryRecordFixedSize = 33
)

// directoryRecordLength returns the on-disk length of a directory record
// for an identifier of the given byte length. A pad byte follows the
// identifier when its length is even, keeping the record length even.
func directoryRecordLength(identifierLen int) int {
	length := directoryRecordFixedSize + identifierLen
	if identifierLen%2 == 0 {
		length++
	}
	return length
}

// isoFileIdentifier returns the on-disk identifier for a node: files carry
// the ISO 9660 ";1" version suffix, directories do not.
func isoFileIdentifier(node *tree.Node) string {
	if node.IsDir() {
		return node.Name()
	}
	return node.Name() + ";1"
}

// sectorsFor returns the number of whole sectors needed for size bytes.
func sectorsFor(size uint32) uint32 {
	return (size + consts.ISO9660_SECTOR_SIZE - 1) / consts.ISO9660_SECTOR_SIZE
}

// directoryExtentSize computes the size in bytes of a directory's extent:
// the "." and ".." records plus one record per child, laid out so no record
// crosses a sector boundary, rounded up to a whole sector.
func directoryExtentSize(dir *tree.Node) uint32 {
	offset := directoryRecordLength(1) * 2 // "." and ".."
	for _, child := range dir.Children() {
		recLen := directoryRecordLength(len(isoFileIdentifier(child)))
		if offset%consts.ISO9660_SECTOR_SIZE+recLen > consts.ISO9660_SECTOR_SIZE {
			offset = (offset/consts.ISO9660_SECTOR_SIZE + 1) * consts.ISO9660_SECTOR_SIZE
		}
		offset += recLen
	}
	return sectorsFor(uint32(offset)) * consts.ISO9660_SECTOR_SIZE
}

// pathTableSize computes the byte size of a path table over the given
// breadth-first directory list. Each record is 8 bytes plus the identifier,
// plus a pad byte when the identifier length is odd. Both the L and M
// tables have the same size.
func pathTableSize(dirs []*tree.Node) uint32 {
	var size uint32
	for _, dir := range dirs {
		idLen := 1 // root identifier is a single 0x00 byte
		if !dir.IsRoot() {
			idLen = len(dir.Name())
		}
		recLen := 8 + idLen
		if idLen%2 != 0 {
			recLen++
		}
		size += uint32(recLen)
	}
	return size
}

// packLayout holds the sector assignments produced by Pack.
type packLayout struct {
	pvdSector        uint32
	terminatorSector uint32
	pathTableLSector uint32
	pathTableMSector uint32
	pathTableSize    uint32
	totalSectors     uint32
	// dirs is the breadth-first directory list; index+1 is each
	// directory's path table number.
	dirs []*tree.Node
	// files is every file node in tree walk order.
	files []*tree.Node
}

// Pack assigns a sector location to every structure in the image: volume
// descriptors, path tables, directory extents, and file extents. It updates
// the PVD's size and location cross-references so a subsequent Save writes
// a consistent image. The layout is:
//
//	sectors 0-15   system area
//	sector  16     primary volume descriptor
//	sector  17     volume descriptor set terminator
//	next           L path table, then M path table
//	next           directory extents in breadth-first order (root first)
//	next           file extents
//
// Note: supplementary (Joliet) descriptors are not yet written; a source
// image's SVDs are dropped from the rebuilt output.
func (iso *ISO9660) Pack() error {
	if iso.root == nil {
		return fmt.Errorf("no directory tree to pack")
	}
	pvd := iso.volumeDescriptorSet.Primary
	if pvd == nil {
		return fmt.Errorf("primary volume descriptor is missing")
	}

	layout := &packLayout{
		pvdSector:        consts.ISO9660_SYSTEM_AREA_SECTORS,
		terminatorSector: consts.ISO9660_SYSTEM_AREA_SECTORS + 1,
		dirs:             iso.root.Directories(),
	}

	// Directory extent sizes are independent of location, so they can be
	// computed before sectors are assigned.
	for _, dir := range layout.dirs {
		dir.PackedSize = directoryExtentSize(dir)
	}
	layout.pathTableSize = pathTableSize(layout.dirs)

	next := layout.terminatorSector + 1
	layout.pathTableLSector = next
	next += sectorsFor(layout.pathTableSize)
	layout.pathTableMSector = next
	next += sectorsFor(layout.pathTableSize)

	for _, dir := range layout.dirs {
		dir.PackedLocation = next
		next += sectorsFor(dir.PackedSize)
	}

	err := iso.root.Walk(func(node *tree.Node) error {
		if node.IsDir() {
			return nil
		}
		node.PackedLocation = next
		node.PackedSize = node.Size()
		next += sectorsFor(node.Size())
		layout.files = append(layout.files, node)
		return nil
	})
	if err != nil {
		return err
	}

	layout.totalSectors = next

	// Update the PVD cross-references.
	pvd.VolumeSpaceSize = layout.totalSectors
	pvd.PrimaryVolumeDescriptorBody.PathTableSize = layout.pathTableSize
	pvd.LocationOfTypeLPathTable = layout.pathTableLSector
	pvd.LocationOfOptionalTypeLPathTable = 0
	pvd.LocationOfTypeMPathTable = layout.pathTableMSector
	pvd.LocationOfOptionalTypeMPathTable = 0
	pvd.ObjectLocation = int64(layout.pvdSector) * consts.ISO9660_SECTOR_SIZE
	pvd.ObjectSize = consts.ISO9660_SECTOR_SIZE
	if pvd.LogicalBlockSize == 0 {
		pvd.LogicalBlockSize = consts.ISO9660_SECTOR_SIZE
	}
	if pvd.VolumeSetSize == 0 {
		pvd.VolumeSetSize = 1
	}
	if pvd.VolumeSequenceNumber == 0 {
		pvd.VolumeSequenceNumber = 1
	}

	// The root directory record embedded in the PVD points at the root
	// extent.
	pvd.RootDirectoryRecord = buildDirectoryRecord(iso.root, "\x00", iso.root)

	iso.layout = layout
	iso.isPacked = true
	return nil
}

// recordingTime returns a timestamp valid for the 7-byte recording
// date/time format, substituting the current time for zero values.
func recordingTime(t time.Time) time.Time {
	if t.IsZero() || t.Year() < 1900 || t.Year() > 2155 {
		return time.Now()
	}
	return t
}

// buildDirectoryRecord constructs the on-disk directory record describing
// target, using the given identifier. The record's extent fields come from
// target's packed location and size, so Pack must run first.
func buildDirectoryRecord(target *tree.Node, identifier string, self *tree.Node) *directory.DirectoryRecord {
	return &directory.DirectoryRecord{
		LocationOfExtent:       target.PackedLocation,
		DataLength:             target.PackedSize,
		RecordingDateAndTime:   recordingTime(self.ModTime()),
		FileFlags:              directory.FileFlags{Directory: target.IsDir()},
		VolumeSequenceNumber:   1,
		LengthOfFileIdentifier: uint8(len(identifier)),
		FileIdentifier:         identifier,
	}
}

// marshalDirectoryExtent serializes a directory's extent: the "." and ".."
// records followed by one record per child, zero-padding whenever a record
// would cross a sector boundary. The result is exactly dir.PackedSize bytes.
func marshalDirectoryExtent(dir *tree.Node) ([]byte, error) {
	buf := make([]byte, dir.PackedSize)
	offset := 0

	parent := dir.Parent()
	if parent == nil {
		parent = dir // the root's ".." points at itself
	}

	records := []*directory.DirectoryRecord{
		buildDirectoryRecord(dir, "\x00", dir),
		buildDirectoryRecord(parent, "\x01", dir),
	}
	for _, child := range dir.Children() {
		records = append(records, buildDirectoryRecord(child, isoFileIdentifier(child), child))
	}

	for _, rec := range records {
		data, err := rec.Marshal()
		if err != nil {
			return nil, fmt.Errorf("failed to marshal directory record %q in %q: %w",
				rec.FileIdentifier, dir.FullPath(), err)
		}
		if offset%consts.ISO9660_SECTOR_SIZE+len(data) > consts.ISO9660_SECTOR_SIZE {
			offset = (offset/consts.ISO9660_SECTOR_SIZE + 1) * consts.ISO9660_SECTOR_SIZE
		}
		if offset+len(data) > len(buf) {
			return nil, fmt.Errorf("directory extent overflow in %q: computed size %d too small",
				dir.FullPath(), dir.PackedSize)
		}
		copy(buf[offset:], data)
		offset += len(data)
	}

	return buf, nil
}

// buildPathTables produces the L (little-endian) and M (big-endian) path
// tables from the packed directory list. Directory numbering follows the
// breadth-first order of layout.dirs, so a directory's path table number is
// its index plus one.
func (iso *ISO9660) buildPathTables() (*pathtable.PathTable, *pathtable.PathTable, error) {
	if iso.layout == nil {
		return nil, nil, fmt.Errorf("image is not packed")
	}

	dirNumber := make(map[*tree.Node]uint16, len(iso.layout.dirs))
	for i, dir := range iso.layout.dirs {
		dirNumber[dir] = uint16(i + 1)
	}

	ptL := pathtable.NewEmptyPathTable("Primary", true)
	ptM := pathtable.NewEmptyPathTable("Primary", false)
	for _, dir := range iso.layout.dirs {
		identifier := "\x00"
		parentNumber := uint16(1)
		if !dir.IsRoot() {
			identifier = dir.Name()
			parentNumber = dirNumber[dir.Parent()]
		}
		ptL.AddRecord(identifier, dir.PackedLocation, parentNumber)
		ptM.AddRecord(identifier, dir.PackedLocation, parentNumber)
	}

	ptL.SetLocation(iso.layout.pathTableLSector, iso.layout.pathTableSize)
	ptM.SetLocation(iso.layout.pathTableMSector, iso.layout.pathTableSize)

	return ptL, ptM, nil
}
