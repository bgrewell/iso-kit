package parser

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/bgrewell/iso-kit/pkg/consts"
	"github.com/bgrewell/iso-kit/pkg/filesystem"
	"github.com/bgrewell/iso-kit/pkg/iso9660/boot"
	"github.com/bgrewell/iso-kit/pkg/iso9660/descriptor"
	"github.com/bgrewell/iso-kit/pkg/iso9660/directory"
	"github.com/bgrewell/iso-kit/pkg/iso9660/extensions"
	"github.com/bgrewell/iso-kit/pkg/iso9660/extent"
	"github.com/bgrewell/iso-kit/pkg/iso9660/info"
	"github.com/bgrewell/iso-kit/pkg/iso9660/pathtable"
	"github.com/bgrewell/iso-kit/pkg/logging"
	"github.com/bgrewell/iso-kit/pkg/option"
	"io"
)

// NewParser creates a new Parser object with the provided reader and options.
func NewParser(reader io.ReaderAt, options *option.OpenOptions) *Parser {
	return &Parser{
		reader:  reader,
		options: options,
		logger:  options.Logger,
	}
}

// Parser is responsible for parsing ISO9660 images.
type Parser struct {
	reader  io.ReaderAt
	options *option.OpenOptions
	logger  *logging.Logger
	layout  *info.ISOLayout
}

// GetBootRecord reads and validates the ISO9660 boot record.
func (p *Parser) GetBootRecord() (*descriptor.BootRecordDescriptor, error) {
	const sectorSize = consts.ISO9660_SECTOR_SIZE
	// The Volume Descriptor Set starts at logical sector 16.
	sector := int64(consts.ISO9660_SYSTEM_AREA_SECTORS)
	p.logger.Trace("Searching for boot record", "sector", sector)
	var buf [2048]byte

	for {
		offset := sector * int64(sectorSize)
		n, err := p.reader.ReadAt(buf[:], offset)
		if err != nil {
			return nil, err
		}
		if n != len(buf) {
			return nil, errors.New("failed to read full sector")
		}

		// Unmarshal the Volume Descriptor Header (first 7 bytes)
		header := descriptor.VolumeDescriptorHeader{}
		if err = header.Unmarshal([7]byte(buf[:7])); err != nil {
			return nil, err
		}

		p.logger.Trace("Found volume descriptor", "type", header.VolumeDescriptorType)

		// A Volume Descriptor Set Terminator has type 255.
		if header.VolumeDescriptorType == descriptor.TYPE_TERMINATOR_DESCRIPTOR {
			p.logger.Debug("No boot record found")
			return nil, nil
		}

		// Validate the ISO9660 signature.
		if string(buf[1:6]) != "CD001" {
			return nil, errors.New("invalid ISO9660 signature")
		}

		// If this is a Boot Record (type 0), unmarshal and return it.
		if header.VolumeDescriptorType == descriptor.TYPE_BOOT_RECORD {
			p.logger.Info("Boot record found", "sector", sector, "offset", offset)
			bootRecord := &descriptor.BootRecordDescriptor{
				VolumeDescriptorHeader: header,
				BootRecordBody: descriptor.BootRecordBody{
					Logger:         p.logger,
					ObjectLocation: offset,
					ObjectSize:     uint32(n),
				},
			}
			if err = bootRecord.Unmarshal(buf); err != nil {
				return nil, err
			}

			return bootRecord, nil
		}

		// Otherwise, move to the next sector.
		sector++
	}
}

// GetElTorito reads and validates the El Torito boot catalog.
func (p *Parser) GetElTorito(bootRecord *descriptor.BootRecordDescriptor) (*boot.ElTorito, error) {
	catalogIndex := binary.LittleEndian.Uint32(bootRecord.BootSystemUse[:4])
	catalogOffset := int64(catalogIndex) * consts.ISO9660_SECTOR_SIZE
	catalogBytes := [consts.ISO9660_SECTOR_SIZE]byte{}
	p.logger.Info("Reading El Torito catalog", "index", catalogIndex, "offset", catalogOffset)
	if _, err := p.reader.ReadAt(catalogBytes[:], catalogOffset); err != nil {
		return nil, err
	}
	et := &boot.ElTorito{
		ObjectLocation: catalogOffset,
		ObjectSize:     consts.ISO9660_SECTOR_SIZE,
		Logger:         p.logger,
	}
	if err := et.UnmarshalBinary(catalogBytes[:]); err != nil {
		return nil, err
	}

	return et, nil
}

// GetPrimaryVolumeDescriptor reads and validates the ISO9660 PVD.
func (p *Parser) GetPrimaryVolumeDescriptor() (*descriptor.PrimaryVolumeDescriptor, error) {
	var buf [2048]byte
	offset := int64(consts.ISO9660_SYSTEM_AREA_SECTORS * consts.ISO9660_SECTOR_SIZE)
	n, err := p.reader.ReadAt(buf[:], offset)
	if err != nil {
		return nil, err
	}
	p.logger.Info("Reading primary volume descriptor", "offset", offset)

	// Unmarshal the VolumeDescriptorHeader
	header := descriptor.VolumeDescriptorHeader{}
	if err = header.Unmarshal([7]byte(buf[:7])); err != nil {
		return nil, err
	}

	// Validate ISO9660 signature
	if string(buf[1:6]) != "CD001" {
		return nil, errors.New("invalid ISO9660 signature")
	}

	// Create a new PrimaryVolumeDescriptor
	pvd := &descriptor.PrimaryVolumeDescriptor{
		VolumeDescriptorHeader: header,
		PrimaryVolumeDescriptorBody: descriptor.PrimaryVolumeDescriptorBody{
			ObjectLocation: offset,
			ObjectSize:     uint32(n),
			Logger:         p.logger,
		},
	}

	// Unmarshal the rest of the buffer
	if err = pvd.Unmarshal([2048]byte(buf[:])); err != nil {
		return nil, err
	}

	return pvd, nil
}

// GetSupplementaryVolumeDescriptors reads and validates the ISO9660 SVD.
func (p *Parser) GetSupplementaryVolumeDescriptors() ([]*descriptor.SupplementaryVolumeDescriptor, error) {
	const sectorSize = consts.ISO9660_SECTOR_SIZE
	// The Volume Descriptor Set starts at logical sector 16.
	sector := int64(consts.ISO9660_SYSTEM_AREA_SECTORS)
	var buf [2048]byte

	// Create a slice to hold the SupplementaryVolumeDescriptors
	var svds []*descriptor.SupplementaryVolumeDescriptor

	for {
		offset := sector * int64(sectorSize)
		n, err := p.reader.ReadAt(buf[:], offset)
		if err != nil {
			return nil, err
		}
		if n != len(buf) {
			return nil, errors.New("failed to read full sector")
		}

		// Unmarshal the Volume Descriptor Header (first 7 bytes)
		header := descriptor.VolumeDescriptorHeader{}
		if err = header.Unmarshal([7]byte(buf[:7])); err != nil {
			return nil, err
		}

		// A Volume Descriptor Set Terminator has type 255.
		if header.VolumeDescriptorType == descriptor.TYPE_TERMINATOR_DESCRIPTOR {
			if len(svds) == 0 {
				p.logger.Debug("No supplementary volume descriptors found")
				return nil, nil
			}
			return svds, nil
		}

		// If this is a Supplementary Volume Descriptor, unmarshal it and add to the collection.
		if header.VolumeDescriptorType == descriptor.TYPE_SUPPLEMENTARY_DESCRIPTOR {
			p.logger.Info("Reading supplementary volume descriptor", "offset", consts.ISO9660_SYSTEM_AREA_SECTORS*consts.ISO9660_SECTOR_SIZE)
			svd := &descriptor.SupplementaryVolumeDescriptor{
				VolumeDescriptorHeader: header,
				SupplementaryVolumeDescriptorBody: descriptor.SupplementaryVolumeDescriptorBody{
					ObjectLocation: offset,
					ObjectSize:     uint32(n),
					Logger:         p.logger,
				},
			}

			if err = svd.Unmarshal(buf); err != nil {
				return nil, err
			}

			svds = append(svds, svd)
		}

		// Otherwise, move to the next sector.
		sector++
	}
}

// GetVolumePartitionDescriptors reads and validates ISO9660 Volume Partition Descriptors (VPDs).
func (p *Parser) GetVolumePartitionDescriptors() ([]*descriptor.VolumePartitionDescriptor, error) {
	const sectorSize = consts.ISO9660_SECTOR_SIZE
	// The Volume Descriptor Set starts at logical sector 16.
	sector := int64(consts.ISO9660_SYSTEM_AREA_SECTORS)
	var buf [sectorSize]byte

	// Slice to store detected Volume Partition Descriptors
	var vpds []*descriptor.VolumePartitionDescriptor

	for {
		offset := sector * int64(sectorSize)
		n, err := p.reader.ReadAt(buf[:], offset)
		if err != nil {
			return nil, err
		}
		if n != len(buf) {
			return nil, errors.New("failed to read full sector")
		}

		// Unmarshal the Volume Descriptor Header (first 7 bytes)
		header := descriptor.VolumeDescriptorHeader{}
		if err = header.Unmarshal([7]byte(buf[:7])); err != nil {
			return nil, err
		}

		// A Volume Descriptor Set Terminator has type 255.
		if header.VolumeDescriptorType == descriptor.TYPE_TERMINATOR_DESCRIPTOR {
			p.logger.Debug("No volume partition descriptors found")
			return vpds, nil
		}

		// If this is a Volume Partition Descriptor, unmarshal it and add it to the collection.
		if header.VolumeDescriptorType == descriptor.TYPE_PARTITION_DESCRIPTOR {
			p.logger.Info("Reading volume partition descriptor", "offset", consts.ISO9660_SYSTEM_AREA_SECTORS*consts.ISO9660_SECTOR_SIZE)
			vpd := &descriptor.VolumePartitionDescriptor{
				VolumeDescriptorHeader: header,
				VolumePartitionDescriptorBody: descriptor.VolumePartitionDescriptorBody{
					ObjectLocation: offset,
					ObjectSize:     uint32(n),
					Logger:         p.logger,
				},
			}

			if err = vpd.Unmarshal(buf); err != nil {
				return nil, err
			}

			vpds = append(vpds, vpd)
		}

		// Move to the next sector.
		sector++
	}
}

// GetVolumeDescriptorSetTerminator reads and validates the ISO9660 Volume Descriptor Set Terminator.
func (p *Parser) GetVolumeDescriptorSetTerminator() (*descriptor.VolumeDescriptorSetTerminator, error) {
	const sectorSize = consts.ISO9660_SECTOR_SIZE
	// The Volume Descriptor Set starts at logical sector 16.
	sector := int64(consts.ISO9660_SYSTEM_AREA_SECTORS)
	p.logger.Trace("Searching for volume descriptor set terminator record", "sector", sector)
	var buf [2048]byte

	for {
		offset := sector * int64(sectorSize)
		n, err := p.reader.ReadAt(buf[:], offset)
		if err != nil {
			return nil, err
		}
		if n != len(buf) {
			return nil, errors.New("failed to read full sector")
		}

		// Unmarshal the Volume Descriptor Header (first 7 bytes)
		header := descriptor.VolumeDescriptorHeader{}
		if err = header.Unmarshal([7]byte(buf[:7])); err != nil {
			return nil, err
		}

		p.logger.Trace("Found volume descriptor", "type", header.VolumeDescriptorType)

		// A Volume Descriptor Set Terminator has type 255.
		if header.VolumeDescriptorType == descriptor.TYPE_TERMINATOR_DESCRIPTOR {
			p.logger.Info("Volume descriptor set terminator found", "sector", sector, "offset", offset)

			// Validate the ISO9660 signature.
			if string(buf[1:6]) != "CD001" {
				return nil, errors.New("invalid ISO9660 signature")
			}

			vdst := &descriptor.VolumeDescriptorSetTerminator{
				VolumeDescriptorHeader: header,
				VolumeDescriptorSetTerminatorBody: descriptor.VolumeDescriptorSetTerminatorBody{
					ObjectLocation: offset,
					ObjectSize:     uint32(n),
					Logger:         p.logger,
				},
			}

			return vdst, nil
		}

		// Otherwise, move to the next sector.
		sector++
	}
}

// GetPathTables reads and validates the ISO9660 Path Tables.
func (p *Parser) GetPathTables(vd descriptor.VolumeDescriptor) ([]*pathtable.PathTable, error) {
	ptL, err := pathtable.NewPathTable(p.reader, vd.LocationOfPathTableL(), int(vd.PathTableSize()), vd.DescriptorType().String(), true)
	if err != nil {
		return nil, err
	}
	ptM, err := pathtable.NewPathTable(p.reader, vd.LocationOfPathTableM(), int(vd.PathTableSize()), vd.DescriptorType().String(), false)
	if err != nil {
		return nil, err
	}
	return []*pathtable.PathTable{ptL, ptM}, nil
}

// BuildFileSystemEntries walks the directory tree and converts entries into FileSystemEntry objects.
func (p *Parser) BuildFileSystemEntries(rootDir *directory.DirectoryRecord, RockRidgeEnabled bool) ([]*filesystem.FileSystemEntry, error) {
	if rootDir == nil {
		return nil, errors.New("rootDir cannot be nil")
	}

	visited := make(map[uint32]bool) // Prevent infinite recursion
	var entries []*filesystem.FileSystemEntry

	var walk func(dir *directory.DirectoryRecord, parentPath string) error
	walk = func(dir *directory.DirectoryRecord, parentPath string) error {
		if visited[dir.LocationOfExtent] {
			return nil
		}
		visited[dir.LocationOfExtent] = true

		// Read directory records
		dirRecords, err := p.ReadDirectoryRecords(dir.LocationOfExtent, dir.DataLength, rootDir.Joliet)
		p.logger.Trace("Finished reading directory records", "dir", dir.GetBestName(RockRidgeEnabled), "records", len(dirRecords))
		if err != nil {
			return err
		}

		for _, record := range dirRecords {
			// Build full path
			fullPath := parentPath + "/" + record.GetBestName(RockRidgeEnabled)

			// Retrieve file attributes
			permissions := record.GetPermissions(RockRidgeEnabled)
			uid, gid := record.GetOwnership(RockRidgeEnabled)
			creationTime, modificationTime := record.GetTimestamps(RockRidgeEnabled)

			// Create FileSystemEntry
			entry := filesystem.NewFileSystemEntry(
				record.GetBestName(RockRidgeEnabled),
				fullPath,
				record.IsDirectory(),
				record.DataLength,
				record.LocationOfExtent,
				uid,
				gid,
				permissions,
				creationTime,
				modificationTime,
				record,
				p.reader,
			)
			p.logger.Trace("Created FileSystemEntry", "path", fullPath, "location", record.LocationOfExtent)

			// Filter out root and parent entries4
			if len(record.FileIdentifier) == 0 || record.FileIdentifier[0] == 0x00 || record.FileIdentifier[0] == 0x01 {
				continue
			}

			entries = append(entries, entry)

			// Recursively walk directories
			if record.IsDirectory() && !record.IsSpecial() {
				if err = walk(record, fullPath); err != nil {
					return err
				}
			}
		}
		return nil
	}

	// Start walking from the root directory
	p.logger.Trace("Starting directory walk", "root", rootDir.GetBestName(RockRidgeEnabled))
	if err := walk(rootDir, ""); err != nil {
		return nil, err
	}

	return entries, nil
}

// TODO: Should this not be exported?
// WalkDirectoryRecords recursively walks the directory tree from a given directory record
// and returns a slice of fully populated DirectoryRecord pointers.
func (p *Parser) WalkDirectoryRecords(rootDir *directory.DirectoryRecord) ([]*directory.DirectoryRecord, error) {
	if rootDir == nil {
		return nil, errors.New("rootDir cannot be nil")
	}

	visited := make(map[uint32]bool) // Prevent infinite recursion
	var records []*directory.DirectoryRecord

	var walk func(dir *directory.DirectoryRecord) error
	walk = func(dir *directory.DirectoryRecord) error {
		// Prevent revisiting the same directory
		if visited[dir.LocationOfExtent] {
			return nil
		}
		visited[dir.LocationOfExtent] = true

		// Read directory records from this LBA
		dirRecords, err := p.ReadDirectoryRecords(dir.LocationOfExtent, dir.DataLength, rootDir.Joliet)
		if err != nil {
			return err
		}
		p.logger.Trace("Finished reading directory records", "dir", dir.GetBestName(false), "records", len(dirRecords))

		for _, record := range dirRecords {
			records = append(records, record)

			// If the record is a directory (excluding `.` and `..` entries), recurse
			if record.IsDirectory() && !record.IsSpecial() {
				if err = walk(record); err != nil {
					return err
				}
			} else {

				fe := &extent.FileExtent{
					FileIdentifier: record.GetBestName(p.options.RockRidgeEnabled),
					LocationOfFile: record.LocationOfExtent,
					SizeOfFile:     record.DataLength,
					Reader:         p.reader,
				}
				record.FileExtent = fe

			}
		}
		return nil
	}

	// Start walking from the provided root directory record
	p.logger.Trace("Starting directory walk", "root", rootDir.GetBestName(false))
	if err := walk(rootDir); err != nil {
		return nil, err
	}

	return records, nil
}

// ReadDirectoryRecords reads directory records from a given LBA (logical block address)
// and processes Rock Ridge extensions if present.
func (p *Parser) ReadDirectoryRecords(lba uint32, dataLength uint32, joliet bool) ([]*directory.DirectoryRecord, error) {

	sectorSize := consts.ISO9660_SECTOR_SIZE
	offset := int64(lba) * int64(sectorSize)
	totalBytes := int(dataLength)

	buf := make([]byte, totalBytes)
	_, err := p.reader.ReadAt(buf, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory sector at LBA %d: %w", lba, err)
	}

	var records []*directory.DirectoryRecord
	index := 0
	sectorBoundary := sectorSize
	p.logger.Info("Reading directory records", "sector", lba, "length", dataLength)

	for index < totalBytes {
		// Read length of this directory record (first byte)
		length := buf[index]
		p.logger.Trace("Reading directory record", "sector", lba, "index", index, "length", length)

		// Stop at padding (zero-filled area)
		if length == 0 {
			// Move to the next sector boundary
			nextSector := ((index / sectorSize) + 1) * sectorSize
			if nextSector >= totalBytes {
				break // End of directory data
			}
			p.logger.Trace("Moving to next sector", "sector", nextSector)
			index = nextSector
			sectorBoundary = nextSector + sectorSize
			continue
		}

		// Ensure record does not cross sector boundary
		if index+int(length) > sectorBoundary {
			// Move to next sector and retry
			index = sectorBoundary
			sectorBoundary += sectorSize
			p.logger.Trace("Moving to next sector boundary", "sector", sectorBoundary)
			continue
		}

		recordData := buf[index : index+int(length)]
		dr := &directory.DirectoryRecord{
			Joliet: joliet,
			Logger: p.logger,
		}
		err = dr.Unmarshal(recordData)
		if err != nil {
			return nil, fmt.Errorf("failed to parse directory record: %w", err)
		}

		dr.ObjectLocation = int64(index) + offset
		dr.ObjectSize = dr.DataLength

		// **Parse Rock Ridge extensions if present**
		if len(dr.SystemUse) > 0 {
			rr, rrErr := extensions.UnmarshalRockRidge(dr.SystemUse)
			if rrErr == nil {
				// Follow SUSP CE continuation areas, merging entries
				// recorded outside the directory record. Bounded to
				// guard against cyclic chains in corrupt images.
				for hops := 0; rr.Continuation != nil && hops < 8; hops++ {
					cont := rr.Continuation
					if cont.Length == 0 || cont.Length > consts.ISO9660_SECTOR_SIZE {
						break
					}
					contData := make([]byte, cont.Length)
					contOffset := int64(cont.Block)*int64(sectorSize) + int64(cont.Offset)
					if _, err := p.reader.ReadAt(contData, contOffset); err != nil {
						p.logger.Debug("Failed to read Rock Ridge continuation area", "offset", contOffset, "error", err)
						break
					}
					if err := rr.ParseInto(contData); err != nil {
						p.logger.Debug("Failed to parse Rock Ridge continuation area", "offset", contOffset, "error", err)
						break
					}
				}
				dr.RockRidge = rr
			}
		}

		records = append(records, dr)

		// Move to the next record
		index += int(length)

	}

	p.logger.Debug("Finished reading directory records", "sector", lba, "records", len(records))
	return records, nil
}
