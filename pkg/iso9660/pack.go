package iso9660

import (
	"fmt"
	"io/fs"
	"time"

	"github.com/bgrewell/iso-kit/pkg/consts"
	"github.com/bgrewell/iso-kit/pkg/iso9660/directory"
	"github.com/bgrewell/iso-kit/pkg/iso9660/extensions"
	"github.com/bgrewell/iso-kit/pkg/iso9660/pathtable"
	"github.com/bgrewell/iso-kit/pkg/iso9660/tree"
)

const (
	// Fixed portion of a directory record preceding the file identifier:
	// length(1) + xattr(1) + extent(8) + data length(8) + timestamp(7) +
	// flags(1) + unit size(1) + gap size(1) + volume sequence(4) + id length(1).
	directoryRecordFixedSize = 33

	// A directory record's total length is stored in one byte.
	maxDirectoryRecordLen = 255

	// On-disk size of a SUSP "CE" continuation pointer entry.
	ceEntrySize = 28
)

// directoryRecordBaseLength returns the length of a directory record for
// the given identifier before any system use data: the fixed fields, the
// identifier, and its pad byte when the identifier length is even.
func directoryRecordBaseLength(identifierLen int) int {
	length := directoryRecordFixedSize + identifierLen
	if identifierLen%2 == 0 {
		length++
	}
	return length
}

// sectorsFor returns the number of whole sectors needed for size bytes.
func sectorsFor(size uint32) uint32 {
	return (size + consts.ISO9660_SECTOR_SIZE - 1) / consts.ISO9660_SECTOR_SIZE
}

// recordPlan describes one directory record to be written: its on-disk
// identifier and the SUSP entries destined for its system use field,
// split between the inline portion and a continuation area.
type recordPlan struct {
	identifier    string
	inlineEntries [][]byte
	contEntries   [][]byte
	// Absolute sector and byte offset of this record's continuation
	// chunk, assigned during layout when contEntries is non-empty.
	contBlock  uint32
	contOffset uint32
}

func entriesLen(entries [][]byte) int {
	total := 0
	for _, e := range entries {
		total += len(e)
	}
	return total
}

func (p *recordPlan) contLen() int {
	return entriesLen(p.contEntries)
}

// suLen returns the size of the record's inline system use field: the
// inline entries, a CE pointer when a continuation exists, and a pad byte
// keeping the record length even.
func (p *recordPlan) suLen() int {
	length := entriesLen(p.inlineEntries)
	if len(p.contEntries) > 0 {
		length += ceEntrySize
	}
	if length%2 != 0 {
		length++
	}
	return length
}

// recordLen returns the record's total on-disk length.
func (p *recordPlan) recordLen() int {
	return directoryRecordBaseLength(len(p.identifier)) + p.suLen()
}

// systemUse assembles the record's inline system use field. Continuation
// locations must already be assigned.
func (p *recordPlan) systemUse() []byte {
	var su []byte
	for _, e := range p.inlineEntries {
		su = append(su, e...)
	}
	if len(p.contEntries) > 0 {
		su = append(su, extensions.BuildCE(p.contBlock, p.contOffset, uint32(p.contLen()))...)
	}
	if len(su)%2 != 0 {
		su = append(su, 0x00)
	}
	return su
}

// continuationData returns the bytes of the record's continuation chunk.
func (p *recordPlan) continuationData() []byte {
	var out []byte
	for _, e := range p.contEntries {
		out = append(out, e...)
	}
	return out
}

// splitEntries distributes SUSP entries between the inline system use
// field and a continuation area. lead entries are always inline (SP must
// stay in the record); forcedCont entries always go to the continuation
// (ER, which never fits). budget is the record's available system use
// space.
func splitEntries(lead, entries, forcedCont [][]byte, budget int) (inline, cont [][]byte, err error) {
	inline = append(inline, lead...)
	inlineLen := entriesLen(lead)

	// First try: everything inline, no CE needed.
	if len(forcedCont) == 0 && inlineLen+entriesLen(entries)+1 <= budget {
		return append(inline, entries...), nil, nil
	}

	// Otherwise reserve room for the CE pointer and overflow the rest.
	overflowed := false
	for _, e := range entries {
		if !overflowed && inlineLen+len(e)+ceEntrySize+1 <= budget {
			inline = append(inline, e)
			inlineLen += len(e)
		} else {
			overflowed = true
			cont = append(cont, e)
		}
	}
	cont = append(cont, forcedCont...)

	if inlineLen+ceEntrySize+1 > budget {
		return nil, nil, fmt.Errorf("system use lead entries (%d bytes) exceed record budget (%d)", inlineLen, budget)
	}
	if entriesLen(cont) > consts.ISO9660_SECTOR_SIZE {
		return nil, nil, fmt.Errorf("continuation area chunk (%d bytes) exceeds one sector", entriesLen(cont))
	}
	return inline, cont, nil
}

// dirPlan carries the record plans for one directory's extent: the "."
// and ".." records plus one per child.
type dirPlan struct {
	dot      *recordPlan
	dotdot   *recordPlan
	children map[*tree.Node]*recordPlan
}

// packLayout holds the sector assignments produced by Pack.
type packLayout struct {
	pvdSector        uint32
	terminatorSector uint32
	pathTableLSector uint32
	pathTableMSector uint32
	pathTableSize    uint32
	// Continuation area region for Rock Ridge data that does not fit
	// inline (always at least one sector when Rock Ridge is enabled, for
	// the ER entry).
	contBaseSector uint32
	contSectors    uint32
	totalSectors   uint32
	rockRidge      bool
	// dirs is the breadth-first directory list; index+1 is each
	// directory's path table number.
	dirs []*tree.Node
	// plans maps each directory to the record plans for its extent.
	plans map[*tree.Node]*dirPlan
	// identifiers maps every node to its on-disk identifier (path table
	// and directory records must agree).
	identifiers map[*tree.Node]string
	// files is every regular file node in tree walk order.
	files []*tree.Node
}

// rockRidgeWriteEnabled reports whether the rebuilt image should carry
// Rock Ridge extensions: created images follow the create option
// (default on); opened images preserve Rock Ridge when the source had it
// and parsing was enabled.
func (iso *ISO9660) rockRidgeWriteEnabled() bool {
	if iso.createOptions != nil {
		return iso.createOptions.RockRidgeEnabled
	}
	if iso.openOptions != nil {
		return iso.openOptions.RockRidgeEnabled && iso.HasRockRidge()
	}
	return false
}

// rrAttributeEntries builds the RR/PX/TF (and SL for symlinks) entries
// describing a node.
func rrAttributeEntries(node *tree.Node) ([][]byte, error) {
	mode := node.Mode()
	if node.IsDir() {
		mode |= fs.ModeDir
	}
	nlink := uint32(1)
	if node.IsDir() {
		nlink = 2
		for _, c := range node.Children() {
			if c.IsDir() {
				nlink++
			}
		}
	}

	var rrFlags byte = 0x01 | 0x80 // PX + TF
	var entries [][]byte
	entries = append(entries, extensions.BuildPX(mode, nlink, node.UID(), node.GID()))

	if node.IsSymlink() {
		rrFlags |= 0x04
		slEntries, err := extensions.BuildSL(node.SymlinkTarget())
		if err != nil {
			return nil, err
		}
		entries = append(entries, slEntries...)
	}

	tf, err := extensions.BuildTF(time.Time{}, recordingTime(node.ModTime()), time.Time{})
	if err != nil {
		return nil, err
	}
	entries = append(entries, tf)

	return append([][]byte{extensions.BuildRR(rrFlags)}, entries...), nil
}

// rrChildEntries builds the full entry set for a child record: attributes
// plus the NM entry carrying the POSIX name.
func rrChildEntries(node *tree.Node) ([][]byte, error) {
	entries, err := rrAttributeEntries(node)
	if err != nil {
		return nil, err
	}
	// The RR entry leads; splice NM's flag bit into it and append the NM
	// entries after PX (order: RR, PX, [SL], NM, TF).
	entries[0][4] |= 0x08
	nm := extensions.BuildNM(node.Name())
	tf := entries[len(entries)-1]
	entries = append(entries[:len(entries)-1], nm...)
	entries = append(entries, tf)
	return entries, nil
}

// buildPlans computes identifiers and system use layouts for every record
// in every directory extent. Location-independent: entry contents that
// need locations (CE) are sized here and filled in after assignment.
func buildPlans(dirs []*tree.Node, rockRidge bool) (map[*tree.Node]*dirPlan, map[*tree.Node]string, error) {
	plans := make(map[*tree.Node]*dirPlan, len(dirs))
	identifiers := make(map[*tree.Node]string)

	for _, dir := range dirs {
		dp := &dirPlan{children: map[*tree.Node]*recordPlan{}}
		plans[dir] = dp

		parent := dir.Parent()
		if parent == nil {
			parent = dir
		}

		dotBudget := maxDirectoryRecordLen - directoryRecordBaseLength(1)

		if rockRidge {
			// "." record: the root's carries SP first and defers the ER
			// entry to a continuation area.
			dotAttrs, err := rrAttributeEntries(dir)
			if err != nil {
				return nil, nil, err
			}
			var lead, forced [][]byte
			if dir.IsRoot() {
				lead = [][]byte{extensions.BuildSP(0)}
				forced = [][]byte{extensions.BuildER()}
			}
			inline, cont, err := splitEntries(lead, dotAttrs, forced, dotBudget)
			if err != nil {
				return nil, nil, fmt.Errorf("planning %q: %w", dir.FullPath(), err)
			}
			dp.dot = &recordPlan{identifier: "\x00", inlineEntries: inline, contEntries: cont}

			dotdotAttrs, err := rrAttributeEntries(parent)
			if err != nil {
				return nil, nil, err
			}
			inline, cont, err = splitEntries(nil, dotdotAttrs, nil, dotBudget)
			if err != nil {
				return nil, nil, fmt.Errorf("planning %q: %w", dir.FullPath(), err)
			}
			dp.dotdot = &recordPlan{identifier: "\x01", inlineEntries: inline, contEntries: cont}
		} else {
			dp.dot = &recordPlan{identifier: "\x00"}
			dp.dotdot = &recordPlan{identifier: "\x01"}
		}

		children := dir.Children()
		ids := assignIdentifiers(children, rockRidge)
		for _, child := range children {
			id := ids[child]
			identifiers[child] = id
			plan := &recordPlan{identifier: id}
			if rockRidge {
				entries, err := rrChildEntries(child)
				if err != nil {
					return nil, nil, err
				}
				budget := maxDirectoryRecordLen - directoryRecordBaseLength(len(id))
				inline, cont, err := splitEntries(nil, entries, nil, budget)
				if err != nil {
					return nil, nil, fmt.Errorf("planning %q: %w", child.FullPath(), err)
				}
				plan.inlineEntries = inline
				plan.contEntries = cont
			}
			dp.children[child] = plan
		}
	}

	return plans, identifiers, nil
}

// eachPlan visits every record plan of a directory in extent order.
func (dp *dirPlan) eachPlan(dir *tree.Node, fn func(target *tree.Node, plan *recordPlan) error) error {
	parent := dir.Parent()
	if parent == nil {
		parent = dir
	}
	if err := fn(dir, dp.dot); err != nil {
		return err
	}
	if err := fn(parent, dp.dotdot); err != nil {
		return err
	}
	for _, child := range dir.Children() {
		if err := fn(child, dp.children[child]); err != nil {
			return err
		}
	}
	return nil
}

// directoryExtentSize computes the byte size of a directory's extent from
// its record plans, applying the rule that records never cross a sector
// boundary, rounded up to a whole sector.
func directoryExtentSize(dir *tree.Node, dp *dirPlan) uint32 {
	offset := 0
	_ = dp.eachPlan(dir, func(_ *tree.Node, plan *recordPlan) error {
		recLen := plan.recordLen()
		if offset%consts.ISO9660_SECTOR_SIZE+recLen > consts.ISO9660_SECTOR_SIZE {
			offset = (offset/consts.ISO9660_SECTOR_SIZE + 1) * consts.ISO9660_SECTOR_SIZE
		}
		offset += recLen
		return nil
	})
	return sectorsFor(uint32(offset)) * consts.ISO9660_SECTOR_SIZE
}

// pathTableSize computes the byte size of a path table over the given
// breadth-first directory list using the assigned identifiers. Both the L
// and M tables have the same size.
func pathTableSize(dirs []*tree.Node, identifiers map[*tree.Node]string) uint32 {
	var size uint32
	for _, dir := range dirs {
		idLen := 1 // root identifier is a single 0x00 byte
		if !dir.IsRoot() {
			idLen = len(identifiers[dir])
		}
		recLen := 8 + idLen
		if idLen%2 != 0 {
			recLen++
		}
		size += uint32(recLen)
	}
	return size
}

// assignContinuationOffsets lays continuation chunks into the
// continuation region: sequential, never crossing a sector boundary
// (SUSP requires a continuation area to lie within one logical block).
// Returns the number of sectors used.
func assignContinuationOffsets(dirs []*tree.Node, plans map[*tree.Node]*dirPlan, baseSector uint32) uint32 {
	block := uint32(0)
	offset := uint32(0)
	for _, dir := range dirs {
		_ = plans[dir].eachPlan(dir, func(_ *tree.Node, plan *recordPlan) error {
			chunk := uint32(plan.contLen())
			if chunk == 0 {
				return nil
			}
			if offset+chunk > consts.ISO9660_SECTOR_SIZE {
				block++
				offset = 0
			}
			plan.contBlock = baseSector + block
			plan.contOffset = offset
			offset += chunk
			return nil
		})
	}
	if offset == 0 && block == 0 {
		return 0
	}
	return block + 1
}

// Pack assigns a sector location to every structure in the image: volume
// descriptors, path tables, Rock Ridge continuation areas, directory
// extents, and file extents. It updates the PVD's size and location
// cross-references so a subsequent Save writes a consistent image. The
// layout is:
//
//	sectors 0-15   system area
//	sector  16     primary volume descriptor
//	sector  17     volume descriptor set terminator
//	next           L path table, then M path table
//	next           Rock Ridge continuation area (when enabled)
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

	rockRidge := iso.rockRidgeWriteEnabled()
	dirs := iso.root.Directories()
	plans, identifiers, err := buildPlans(dirs, rockRidge)
	if err != nil {
		return err
	}

	layout := &packLayout{
		pvdSector:        consts.ISO9660_SYSTEM_AREA_SECTORS,
		terminatorSector: consts.ISO9660_SYSTEM_AREA_SECTORS + 1,
		rockRidge:        rockRidge,
		dirs:             dirs,
		plans:            plans,
		identifiers:      identifiers,
	}

	// Directory extent sizes are independent of location, so they can be
	// computed before sectors are assigned.
	for _, dir := range dirs {
		dir.PackedSize = directoryExtentSize(dir, plans[dir])
	}
	layout.pathTableSize = pathTableSize(dirs, identifiers)

	next := layout.terminatorSector + 1
	layout.pathTableLSector = next
	next += sectorsFor(layout.pathTableSize)
	layout.pathTableMSector = next
	next += sectorsFor(layout.pathTableSize)

	// The continuation region's size is location-independent; assign its
	// base here and lay chunks into it.
	layout.contBaseSector = next
	layout.contSectors = assignContinuationOffsets(dirs, plans, layout.contBaseSector)
	next += layout.contSectors

	for _, dir := range dirs {
		dir.PackedLocation = next
		next += sectorsFor(dir.PackedSize)
	}

	err = iso.root.Walk(func(node *tree.Node) error {
		if node.IsDir() || node.IsSymlink() {
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
	// extent. It carries no system use data.
	pvd.RootDirectoryRecord = buildDirectoryRecord(iso.root, "\x00", nil, iso.root)

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
// target, using the given identifier and system use field. The record's
// extent fields come from target's packed location and size, so Pack must
// run first.
func buildDirectoryRecord(target *tree.Node, identifier string, systemUse []byte, timeSource *tree.Node) *directory.DirectoryRecord {
	return &directory.DirectoryRecord{
		LocationOfExtent:       target.PackedLocation,
		DataLength:             target.PackedSize,
		RecordingDateAndTime:   recordingTime(timeSource.ModTime()),
		FileFlags:              directory.FileFlags{Directory: target.IsDir()},
		VolumeSequenceNumber:   1,
		LengthOfFileIdentifier: uint8(len(identifier)),
		FileIdentifier:         identifier,
		SystemUse:              systemUse,
	}
}

// marshalDirectoryExtent serializes a directory's extent: the "." and ".."
// records followed by one record per child, zero-padding whenever a record
// would cross a sector boundary. The result is exactly dir.PackedSize bytes.
func marshalDirectoryExtent(dir *tree.Node, dp *dirPlan) ([]byte, error) {
	buf := make([]byte, dir.PackedSize)
	offset := 0

	err := dp.eachPlan(dir, func(target *tree.Node, plan *recordPlan) error {
		rec := buildDirectoryRecord(target, plan.identifier, plan.systemUse(), target)
		data, err := rec.Marshal()
		if err != nil {
			return fmt.Errorf("failed to marshal directory record %q in %q: %w",
				plan.identifier, dir.FullPath(), err)
		}
		if offset%consts.ISO9660_SECTOR_SIZE+len(data) > consts.ISO9660_SECTOR_SIZE {
			offset = (offset/consts.ISO9660_SECTOR_SIZE + 1) * consts.ISO9660_SECTOR_SIZE
		}
		if offset+len(data) > len(buf) {
			return fmt.Errorf("directory extent overflow in %q: computed size %d too small",
				dir.FullPath(), dir.PackedSize)
		}
		copy(buf[offset:], data)
		offset += len(data)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return buf, nil
}

// marshalContinuationArea serializes the Rock Ridge continuation region.
// Returns nil when no continuation data exists.
func (iso *ISO9660) marshalContinuationArea() []byte {
	if iso.layout.contSectors == 0 {
		return nil
	}
	buf := make([]byte, iso.layout.contSectors*consts.ISO9660_SECTOR_SIZE)
	for _, dir := range iso.layout.dirs {
		_ = iso.layout.plans[dir].eachPlan(dir, func(_ *tree.Node, plan *recordPlan) error {
			if plan.contLen() == 0 {
				return nil
			}
			start := (plan.contBlock-iso.layout.contBaseSector)*consts.ISO9660_SECTOR_SIZE + plan.contOffset
			copy(buf[start:], plan.continuationData())
			return nil
		})
	}
	return buf
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
			identifier = iso.layout.identifiers[dir]
			parentNumber = dirNumber[dir.Parent()]
		}
		ptL.AddRecord(identifier, dir.PackedLocation, parentNumber)
		ptM.AddRecord(identifier, dir.PackedLocation, parentNumber)
	}

	ptL.SetLocation(iso.layout.pathTableLSector, iso.layout.pathTableSize)
	ptM.SetLocation(iso.layout.pathTableMSector, iso.layout.pathTableSize)

	return ptL, ptM, nil
}
