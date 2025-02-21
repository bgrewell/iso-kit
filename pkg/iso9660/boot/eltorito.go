package boot

import (
	"encoding/binary"
	"fmt"
	"github.com/bgrewell/iso-kit/pkg/consts"
	"github.com/bgrewell/iso-kit/pkg/filesystem"
	"github.com/bgrewell/iso-kit/pkg/logging"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// Logical sector 17 containing El-Torito boot catalog
	EL_TORITO_SECTOR = 0x11
	// Default catalog name for non-Rock Ridge filesystems
	EL_TORITO_DEFAULT_CATALOG = "BOOT.CAT"
	// Default catalog name for Rock Ridge filesystems
	EL_TORITO_DEFAULT_CATALOG_RR = "boot.catalog"
)

// PartitionType represents the type of partition in the boot image.
type PartitionType byte

// List of GUID partition types
const (
	Empty         PartitionType = 0x00
	Fat12         PartitionType = 0x01
	XenixRoot     PartitionType = 0x02
	XenixUsr      PartitionType = 0x03
	Fat16         PartitionType = 0x04
	ExtendedCHS   PartitionType = 0x05
	Fat16b        PartitionType = 0x06
	NTFS          PartitionType = 0x07
	CommodoreFAT  PartitionType = 0x08
	Fat32CHS      PartitionType = 0x0b
	Fat32LBA      PartitionType = 0x0c
	Fat16bLBA     PartitionType = 0x0e
	ExtendedLBA   PartitionType = 0x0f
	Linux         PartitionType = 0x83
	LinuxExtended PartitionType = 0x85
	LinuxLVM      PartitionType = 0x8e
	Iso9660       PartitionType = 0x96
	MacOSXUFS     PartitionType = 0xa8
	MacOSXBoot    PartitionType = 0xab
	HFS           PartitionType = 0xaf
	Solaris8Boot  PartitionType = 0xbe
	GPTProtective PartitionType = 0xef
	EFISystem     PartitionType = 0xef
	VMWareFS      PartitionType = 0xfb
	VMWareSwap    PartitionType = 0xfc
)

// Platform represents the target booting system for an El-Torito bootable ISO.
type Platform uint8

const (
	BIOS Platform = 0x0  // Classic PC-BIOS x86
	PPC  Platform = 0x1  // PowerPC
	Mac  Platform = 0x2  // Macintosh systems
	EFI  Platform = 0xef // Extensible Firmware Interface (EFI)
)

// Emulation represents the emulation mode used for booting.
type Emulation uint8

const (
	NoEmulation        Emulation = 0x0 // No emulation (default)
	Floppy12Emulation  Emulation = 0x1 // Emulate a 1.2 MB floppy
	Floppy144Emulation Emulation = 0x2 // Emulate a 1.44 MB floppy
	Floppy288Emulation Emulation = 0x3 // Emulate a 2.88 MB floppy
	HardDiskEmulation  Emulation = 0x4 // Emulate a hard disk
)

func (e Emulation) String() string {
	switch e {
	case NoEmulation:
		return "NoEmul"
	case Floppy12Emulation:
		return "1.2MFloppy"
	case Floppy144Emulation:
		return "1.44MFloppy"
	case Floppy288Emulation:
		return "2.88MFloppy"
	case HardDiskEmulation:
		return "HardDisk"
	default:
		return "Unknown"
	}
}

// ElTorito represents the El-Torito boot structure for a disk.
type ElTorito struct {
	BootCatalog     string           // Path to the boot catalog file
	HideBootCatalog bool             // Whether to hide the boot catalog in the filesystem
	Entries         []*ElToritoEntry // List of El-Torito boot entries
	Platform        Platform         // Target platform for booting
	Logger          *logging.Logger  // Logger for debug output
}

// UnmarshalBinary decodes an El-Torito Boot Catalog from binary form
func (et *ElTorito) UnmarshalBinary(data []byte) error {
	if len(data) < 32 {
		return fmt.Errorf("Boot Catalog: data too short")
	}

	// Parse Validation Entry
	if err := parseValidationEntry(data[:32]); err != nil {
		return fmt.Errorf("Boot Catalog: invalid Validation Entry: %w", err)
	}

	// Parse Boot Entries
	sectionCount := 0
	for offset := 32; offset < len(data); offset += 32 {
		entryData := data[offset : offset+32]

		// Check for End of Catalog
		if entryData[0] == 0x00 {
			break
		}

		// Handle Section Headers
		if entryData[0] == 0x90 || entryData[0] == 0x91 {
			sectionCount = int(binary.LittleEndian.Uint16(entryData[2:4]))
			continue
		}

		// Parse Section Entries
		if sectionCount > 0 {
			entry := parseSectionEntry(entryData)
			et.Entries = append(et.Entries, entry)
			sectionCount--
			continue
		}

		// Parse Initial/Default Entry
		entry := parseInitialEntry(entryData)
		et.Entries = append(et.Entries, entry)
	}
	return nil
}

// ElToritoEntry represents a single entry in an El-Torito boot catalog.
type ElToritoEntry struct {
	Platform      Platform      // Target platform
	Emulation     Emulation     // Emulation mode
	BootFile      string        // Path to the boot file
	HideBootFile  bool          // Whether to hide the boot file in the filesystem
	LoadSegment   uint16        // Open segment address
	PartitionType PartitionType // Partition type of the boot file
	size          uint16        // Size of the boot file in 512-byte blocks
	location      uint32        // Location of the boot file in 2048-byte sectors
}

// SectionHeader represents a header for grouping entries in the boot catalog.
type SectionHeader struct {
	Indicator byte     // Indicator byte (0x90 or 0x91 for the last section)
	Platform  Platform // Target platform
	Entries   uint16   // Number of entries in the section
}

// SelectionCriteria represents optional vendor-specific selection criteria.
type SelectionCriteria struct {
	Type       byte   // Selection criteria type
	VendorData []byte // Vendor-specific data
}

// ValidationEntry represents the validation entry at the start of the boot catalog.
type ValidationEntry struct {
	Platform    Platform // Target platform
	Identifier  string   // Identifier string
	Checksum    uint16   // Validation checksum
	KeyByte55AA uint16   // Fixed 0x55AA marker
}

// BuildBootImageEntries constructs a list of FileSystemEntry objects for all boot images.
func (et *ElTorito) BuildBootImageEntries() ([]*filesystem.FileSystemEntry, error) {
	var entries []*filesystem.FileSystemEntry

	for i, entry := range et.Entries {
		// Skip non-bootable entries
		if entry.size == 0 || entry.location == 0 {
			continue
		}

		// Construct a synthetic file name for the boot image
		filename := fmt.Sprintf("%d-Boot-%s.img", i+1, entry.Emulation)

		// TODO: The directory should be user configurable, for now we use the same default as 7z
		// Create a FileSystemEntry for the boot image
		fsEntry := &filesystem.FileSystemEntry{
			Name:       filename,
			FullPath:   "/[BOOT]/" + filename, // Logical path inside the ISO
			IsDir:      false,
			Size:       uint32(entry.size * 512), // Convert 512-byte block size
			Location:   entry.location,
			Mode:       0444,        // Read-only boot image
			CreateTime: time.Time{}, // No real timestamp in El Torito
			ModTime:    time.Time{},
			UID:        nil,
			GID:        nil,
		}

		entries = append(entries, fsEntry)
	}

	return entries, nil
}

// ExtractBootImages extracts all bootable images to the specified directory.
func (et *ElTorito) ExtractBootImages(ra io.ReaderAt, outputDir string) error {
	for i, entry := range et.Entries {
		// Skip non-bootable entries
		if entry.size == 0 || entry.location == 0 {
			continue
		}

		// Create the file name
		filename := fmt.Sprintf("%d-Boot-%s.img", i+1, entry.Emulation)
		outputPath := filepath.Join(outputDir, filename)

		// Open the output file for writing
		outFile, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("failed to create file %s: %w", outputPath, err)
		}
		defer outFile.Close()

		// Read the boot image data
		startOffset := int64(entry.location) * int64(consts.ISO9660_SECTOR_SIZE)
		data := make([]byte, int64(entry.size)*512) // Size is in 512-byte blocks
		if _, err := ra.ReadAt(data, startOffset); err != nil {
			return fmt.Errorf("failed to read boot image at offset %d: %w", startOffset, err)
		}

		// Write the data to the file
		if _, err := outFile.Write(data); err != nil {
			return fmt.Errorf("failed to write boot image to file %s: %w", outputPath, err)
		}

		// Save the boot file path in the entry
		entry.BootFile = outputPath
	}
	return nil
}

func IsElTorito(bootSystemIdentifier string) bool {
	trimmed := strings.TrimRight(bootSystemIdentifier, "\x00")
	return trimmed == consts.EL_TORITO_BOOT_SYSTEM_ID
}

func parseInitialEntry(data []byte) *ElToritoEntry {
	return &ElToritoEntry{
		Platform:      Platform(data[1]),
		Emulation:     Emulation(data[2]),
		LoadSegment:   binary.LittleEndian.Uint16(data[4:6]),
		PartitionType: PartitionType(data[4]),
		size:          binary.LittleEndian.Uint16(data[6:8]),
		location:      binary.LittleEndian.Uint32(data[8:12]),
	}
}

func parseSectionEntry(data []byte) *ElToritoEntry {
	return &ElToritoEntry{
		Platform:      Platform(data[1]),
		Emulation:     Emulation(data[2]),
		LoadSegment:   binary.LittleEndian.Uint16(data[4:6]),
		PartitionType: PartitionType(data[4]),
		size:          binary.LittleEndian.Uint16(data[6:8]),
		location:      binary.LittleEndian.Uint32(data[8:12]),
	}
}

func parseValidationEntry(data []byte) error {
	if len(data) < 32 {
		return fmt.Errorf("Validation Entry: data too short")
	}
	if data[0] != 0x01 {
		return fmt.Errorf("Validation Entry: invalid header ID %x", data[0])
	}
	checksum := uint16(0)
	for i := 0; i < 32; i += 2 {
		checksum += binary.LittleEndian.Uint16(data[i : i+2])
	}
	if checksum != 0 {
		return fmt.Errorf("Validation Entry: checksum invalid")
	}
	if data[0x1E] != 0x55 || data[0x1F] != 0xAA {
		return fmt.Errorf("Validation Entry: invalid key bytes %x%x", data[0x1E], data[0x1F])
	}
	return nil
}
