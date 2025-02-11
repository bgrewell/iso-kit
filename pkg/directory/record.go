package directory

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/bgrewell/iso-kit/pkg/logging"
	"github.com/bgrewell/iso-kit/pkg/rockridge"
	"github.com/bgrewell/iso-kit/pkg/susp"
	"github.com/bgrewell/iso-kit/pkg/validation"
	"github.com/go-logr/logr"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
)

// BuildDirectoryRecords walks the given root path and builds a slice of DirectoryRecords.
// Note: For a complete ISO9660 image you will need to build a hierarchy (including . and .. entries)
// and assign extent locations. This example just creates a flat list.
func BuildDirectoryRecords(rootPath string, logger logr.Logger) ([]*DirectoryRecord, error) {
	var records []*DirectoryRecord

	err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Retrieve file info
		info, err := d.Info()
		if err != nil {
			return err
		}

		// Create a directory record for this file/directory.
		rec, err := NewDirectoryRecordFromFileInfo(logger, info)
		if err != nil {
			return err
		}

		// Optionally, you might want to store the full path or a relative path in your record.
		// For example, if you need to create a hierarchical directory structure.
		// rec.FullPath = path  // (Assuming you add such a field.)

		records = append(records, rec)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}
	return records, nil
}

// NewRecord creates a new DirectoryRecord instance.
func NewRecord(logger logr.Logger) *DirectoryRecord {
	return &DirectoryRecord{
		logger: logger,
	}
}

// NewDirectoryRecordFromFileInfo creates a DirectoryRecord for the given file or directory.
// Note: For a real ISO9660 image you will later need to assign LocationOfExtent and
// (for directories) compute DataLength based on the size of the directory record.
func NewDirectoryRecordFromFileInfo(logger logr.Logger, info os.FileInfo) (*DirectoryRecord, error) {
	dr := NewRecord(logger)
	// For now, set ExtendedAttributeRecord to zero.
	dr.ExtendedAttributeRecord = 0

	// LocationOfExtent and DataLength will be assigned later when laying out the image.
	dr.LocationOfExtent = 0
	if !info.IsDir() {
		dr.DataLength = uint32(info.Size())
	} else {
		// For directories, DataLength is the size (in bytes) of the directory record.
		// You may compute this later when you have built the complete directory.
		dr.DataLength = 0
	}

	// Set the recording date and time (7 bytes).
	dr.RecordingDateAndTime = FormatRecordingDateAndTime(info.ModTime())

	// Set file flags – at minimum mark as directory if applicable.
	dr.FileFlags = &FileFlags{}
	var flags uint8 = 0
	if info.IsDir() {
		flags |= 0x02 // Bit 1: Directory flag per ISO9660
	}
	// (Other flags like AssociatedFile, Protection, etc., remain false for now.)
	dr.FileFlags.Set(flags)

	// For simplicity, assume FileUnitSize and InterleaveGapSize are zero.
	dr.FileUnitSize = 0
	dr.InterleaveGapSize = 0

	// VolumeSequenceNumber is normally 1.
	dr.VolumeSequenceNumber = 1

	// Set the file identifier to the base name. (use upper-case variant of the name)
	id := strings.ToUpper(info.Name())
	valid := false
	if info.IsDir() {
		valid = validation.ValidISO9660DirIdentifier(id)
	} else {
		valid = validation.ValidISO9660FileIdentifier(id)
	}
	if !valid {
		return nil, fmt.Errorf("invalid file identifier: %s", id)
	}
	dr.FileIdentifier = id
	dr.FileIdentifierLength = uint8(len(id))

	// For now, we don’t include a PaddingField or any SystemUse.
	dr.PaddingField = nil
	dr.SystemUse = nil

	// Compute the overall record length.
	dr.computeLength()

	return dr, nil
}

// DirectoryRecord represents a single Record in a directory.
type DirectoryRecord struct {
	LengthOfDirectoryRecord uint8
	ExtendedAttributeRecord uint8
	LocationOfExtent        uint32
	DataLength              uint32
	RecordingDateAndTime    []byte
	FileFlags               *FileFlags
	FileUnitSize            uint8
	InterleaveGapSize       uint8
	VolumeSequenceNumber    uint16
	FileIdentifierLength    uint8
	FileIdentifier          string
	PaddingField            []byte
	SystemUse               []byte
	SystemUseEntries        *susp.SystemUseEntries
	ExtensionRecords        []*susp.ExtensionRecord
	Joliet                  bool
	hasRockRidge            bool
	rockRidgeName           *string
	rockRidgePermissions    *rockridge.RockRidgePosixEntry
	rockRidgeTimestamps     *rockridge.RockRidgeTimestamps
	logger                  logr.Logger
}

// Unmarshal decodes a DirectoryRecord from binary form.
func (dr *DirectoryRecord) Unmarshal(data []byte, isoFile io.ReaderAt) error {
	if len(data) < 33 {
		return errors.New("invalid data length")
	}

	dr.logger.V(logging.TRACE).Info("Unmarshalling directory record")

	// Basic fields (no slice references here, just copying bytes into numeric fields)
	dr.LengthOfDirectoryRecord = data[0]
	dr.ExtendedAttributeRecord = data[1]
	dr.LocationOfExtent = binary.LittleEndian.Uint32(data[2:6])
	dr.DataLength = binary.LittleEndian.Uint32(data[10:14])

	// 1) Copy the RecordingDateAndTime bytes to avoid referencing the original buffer.
	//    data[18:25] has 7 bytes (per ISO spec).
	if len(data) < 25 {
		return fmt.Errorf("invalid data length for RecordingDateAndTime")
	}
	tempRDT := data[18:25] // 7 bytes
	dr.RecordingDateAndTime = make([]byte, len(tempRDT))
	copy(dr.RecordingDateAndTime, tempRDT)

	dr.FileFlags = &FileFlags{}
	dr.FileFlags.Set(data[25])
	dr.FileUnitSize = data[26]
	dr.InterleaveGapSize = data[27]
	dr.VolumeSequenceNumber = binary.LittleEndian.Uint16(data[28:30])
	dr.FileIdentifierLength = data[32]

	// Log basic fields
	dr.logger.V(logging.TRACE).Info("Length of directory record", "lengthOfDirectoryRecord", dr.LengthOfDirectoryRecord)
	dr.logger.V(logging.TRACE).Info("Extended attribute record", "extendedAttributeRecord", dr.ExtendedAttributeRecord)
	dr.logger.V(logging.TRACE).Info("Location of extent", "locationOfExtent", dr.LocationOfExtent)
	dr.logger.V(logging.TRACE).Info("Data length", "dataLength", dr.DataLength)
	dr.logger.V(logging.TRACE).Info("Recording date and time", "recordingDateAndTime", dr.RecordingDateAndTime)
	dr.logger.V(logging.TRACE).Info("File flags", "fileFlags", dr.FileFlags.String())
	dr.logger.V(logging.TRACE).Info("File unit size", "fileUnitSize", dr.FileUnitSize)
	dr.logger.V(logging.TRACE).Info("Interleave gap size", "interleaveGapSize", dr.InterleaveGapSize)
	dr.logger.V(logging.TRACE).Info("Volume sequence number", "volumeSequenceNumber", dr.VolumeSequenceNumber)
	dr.logger.V(logging.TRACE).Info("File identifier length", "fileIdentifierLength", dr.FileIdentifierLength)

	// 2) Handle file identifiers (Joliet vs. non-Joliet).
	//    We create a new string from the raw bytes, so it's automatically safe.
	if int(33+dr.FileIdentifierLength) > len(data) {
		return fmt.Errorf("file identifier extends beyond provided data")
	}
	rawIdentifier := data[33 : 33+dr.FileIdentifierLength]
	if dr.Joliet && dr.FileIdentifierLength != 1 {
		jolietName, err := DecodeJolietName(rawIdentifier)
		if err != nil {
			return fmt.Errorf("failed to decode Joliet name: %w", err)
		}
		dr.FileIdentifier = jolietName
	} else {
		// Converting to string already copies data in Go’s string internals
		dr.FileIdentifier = string(rawIdentifier)
	}

	// Special cases: root dir and parent dir
	identifier := dr.FileIdentifier
	switch identifier {
	case "\x00":
		identifier = "<root_dir>"
	case "\x01":
		identifier = "<parent>"
	}
	dr.logger.V(logging.TRACE).Info("File identifier", "identifier", identifier)

	// 3) Compute system-use start (may include a 1-byte padding if FileIdentifierLength is even).
	systemUseStart := 33 + dr.FileIdentifierLength
	if dr.FileIdentifierLength%2 == 0 {
		// Copy the 1-byte PaddingField if it’s within range
		if int(systemUseStart) >= len(data) {
			dr.logger.Error(nil, "Padding field offset out of range",
				"systemUseStart", systemUseStart, "dataLength", len(data))
			return nil // or return an error if desired
		}
		dr.PaddingField = make([]byte, 1)
		dr.PaddingField[0] = data[systemUseStart]
		dr.logger.V(logging.TRACE).Info("File identifier is even, padding field set",
			"paddingField", fmt.Sprintf("%x", dr.PaddingField))
		systemUseStart++
	} else {
		dr.PaddingField = nil
	}

	dr.logger.V(logging.TRACE).Info("System use start calculated", "systemUseStart", systemUseStart)
	if int(systemUseStart) > len(data) {
		dr.logger.Error(nil, "System use start is greater than data length",
			"systemUseStart", systemUseStart, "dataLength", len(data))
		// Return nil or error based on desired behavior
		return nil
	}

	// 4) Parse system use entries (SUSP, Rock Ridge, etc.). Make a copy.
	systemUse := data[systemUseStart:]
	if len(systemUse) > 0 {
		dr.SystemUse = make([]byte, len(systemUse))
		copy(dr.SystemUse, systemUse)

		dr.logger.V(logging.TRACE).Info("System use data",
			"hex", fmt.Sprintf("%x", dr.SystemUse), "length", len(dr.SystemUse))

		entries, err := susp.GetSystemUseEntries(dr.SystemUse, isoFile, dr.logger)
		if err != nil {
			return err
		}
		dr.SystemUseEntries = entries

		extensionRecords, err := dr.SystemUseEntries.GetExtensionRecords()
		if err != nil {
			return err
		}
		dr.ExtensionRecords = extensionRecords

		dr.hasRockRidge = dr.SystemUseEntries.HasRockRidge()
		if dr.hasRockRidge {
			if dr.FileIdentifier != "\u0000" && dr.FileIdentifier != "\u0001" {
				dr.rockRidgeName = dr.SystemUseEntries.RockRidgeName()
				if dr.rockRidgeName == nil {
					dr.logger.Error(nil, "WARNING: Rock Ridge name is nil", "identifier", dr.FileIdentifier)
				} else {
					dr.logger.V(logging.TRACE).Info("Rock Ridge name", "name", *dr.rockRidgeName)
				}
			}

			dr.rockRidgePermissions = dr.SystemUseEntries.RockRidgePermissions()
			if dr.rockRidgePermissions == nil {
				dr.logger.Error(nil, "Rock Ridge permissions are nil")
			} else {
				dr.logger.V(logging.TRACE).Info("Rock Ridge permissions", "permissions", dr.rockRidgePermissions)
			}

			dr.rockRidgeTimestamps = dr.SystemUseEntries.RockRidgeTimestamps()
		}
	} else {
		dr.logger.V(logging.TRACE).Info("System use is nil or empty")
	}

	dr.logger.V(logging.TRACE).Info("Directory record unmarshalled successfully")
	return nil
}

// HasRockRidge returns true if the directory record has Rock Ridge extensions.
func (dr DirectoryRecord) HasRockRidge() bool {
	return dr.hasRockRidge
}

// RockRidgeName returns the Rock Ridge name of the directory record.
func (dr DirectoryRecord) RockRidgeName() *string {
	return dr.rockRidgeName
}

// RockRidgePermissions returns the Rock Ridge permissions of the directory record.
func (dr DirectoryRecord) RockRidgePermissions() *rockridge.RockRidgePosixEntry {
	return dr.rockRidgePermissions
}

// RockRidgeTimestamps returns the Rock Ridge timestamps of the directory record.
func (dr DirectoryRecord) RockRidgeTimestamps() *rockridge.RockRidgeTimestamps {
	return dr.rockRidgeTimestamps
}

// computeLength computes and sets the LengthOfDirectoryRecord field.
// According to ISO9660, the record length is:
// 33 bytes (fixed fields) + FileIdentifier length + optional 1-byte padding (if FileIdentifier length is even)
func (dr *DirectoryRecord) computeLength() {
	idLen := len(dr.FileIdentifier)
	recLen := 33 + idLen
	if idLen%2 == 0 {
		recLen++ // add 1 byte for padding if needed
	}
	dr.LengthOfDirectoryRecord = uint8(recLen)
}

// DecodeJolietName converts a Joliet file identifier (UTF-16BE) into a Go string.
func DecodeJolietName(data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil // Empty name
	}

	// Special case: Root, Current, and Parent Directory Identifiers
	if len(data) == 1 {
		switch data[0] {
		case 0x00:
			return ".", nil // Root or Current Directory
		case 0x01:
			return "..", nil // Parent Directory
		default:
			return "", fmt.Errorf("invalid single-byte directory identifier: 0x%02X", data[0])
		}
	}

	// Ensure data length is even for UTF-16 decoding
	if len(data)%2 != 0 {
		return "", fmt.Errorf("invalid Joliet file identifier: odd byte length")
	}

	// Read as UTF-16 big-endian
	utf16Chars := make([]uint16, len(data)/2)
	err := binary.Read(bytes.NewReader(data), binary.BigEndian, &utf16Chars)
	if err != nil {
		return "", fmt.Errorf("failed to read UTF-16BE: %w", err)
	}

	// Convert UTF-16 to Go string
	name := string(utf16.Decode(utf16Chars))

	// Joliet allows null-padded names, trim null padding
	name = trimNullPadding(name)

	// Validate allowed character set per Joliet spec
	if err := validateJolietCharacters(name); err != nil {
		return "", err
	}

	return name, nil
}

// trimNullPadding removes trailing null characters (U+0000) from the string.
func trimNullPadding(s string) string {
	for len(s) > 0 && s[len(s)-1] == '\x00' {
		s = s[:len(s)-1]
	}
	return s
}

// validateJolietCharacters ensures the decoded name complies with allowed UCS-2 characters.
func validateJolietCharacters(name string) error {
	for _, r := range name {
		if r <= 0x001F || r == 0x002A || r == 0x002F || r == 0x003A ||
			r == 0x003B || r == 0x003F || r == 0x005C {
			return fmt.Errorf("invalid character 0x%04X in Joliet file identifier", r)
		}
	}
	return nil
}

// FormatRecordingDateAndTime converts a time.Time to a 7-byte array per ISO9660 spec.
// Byte 0: Year since 1900, Byte 1: Month, Byte 2: Day, Byte 3: Hour,
// Byte 4: Minute, Byte 5: Second, Byte 6: GMT offset in 15-minute intervals.
func FormatRecordingDateAndTime(t time.Time) []byte {
	year := t.Year() - 1900
	month := int(t.Month())
	day := t.Day()
	hour := t.Hour()
	minute := t.Minute()
	second := t.Second()
	_, offsetSeconds := t.Zone()
	// Convert offset (in seconds) to number of 15-minute intervals.
	offset15 := int8(offsetSeconds / 60 / 15)
	return []byte{
		byte(year),
		byte(month),
		byte(day),
		byte(hour),
		byte(minute),
		byte(second),
		byte(offset15),
	}
}
