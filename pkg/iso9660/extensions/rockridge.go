package extensions

import (
	"errors"
	"github.com/bgrewell/iso-kit/pkg/iso9660/encoding"
	"io/fs"
	"os"
	"time"
)

const (
	ROCK_RIDGE_IDENTIFIER = "RRIP_1991A"
	ROCK_RIDGE_VERSION    = 1
)

type RockRidgeEntryType string

const (
	//POSIX file permissions (owner, group, other)
	POSIX_FILE_PERMS RockRidgeEntryType = "PX"
	//Device numbers for block/character device nodes (major/minor)
	POSIX_DEVICE_NUM RockRidgeEntryType = "PN"
	//Symbolic link data (path components,flags)
	SYMBOLIC_LINK RockRidgeEntryType = "SL"
	//AlternateName (used for long filenames, case preservation, etc.)
	ALTERNATE_NAME RockRidgeEntryType = "NM"
	//ChildLink (used for directory relocation chains)
	CHILD_LINK RockRidgeEntryType = "CL"
	//ParentLink (links a relocated directory back to its parent)
	PARENT_LINK RockRidgeEntryType = "PL"
	//Marks a directory that has been relocated
	RELOCATED_DIR RockRidgeEntryType = "RE"
	//Time stamp information (creation, modification, access,etc)
	TIME_STAMPS RockRidgeEntryType = "TF"
	//Sparse file information (less commonly used)
	SPARSE_FILE RockRidgeEntryType = "SF"
	//An older “Rock Ridge” extension signature (now typically replaced by ER).
	ROCK_RIDGE RockRidgeEntryType = "RR"
)

type NameEntryFlags struct {
	Continue  bool // Bit 0: Alternate Name continues in the next "NM" entry
	Current   bool // Bit 1: Alternate Name refers to the current directory ("." in POSIX)
	Parent    bool // Bit 2: Alternate Name refers to the parent directory (".." in POSIX)
	Reserved1 bool // Bit 3: Reserved, should be ZERO
	Reserved2 bool // Bit 4: Reserved, should be ZERO
	Reserved3 bool // Bit 5: Historically contains the network node name
	Reserved4 bool // Bit 6: Unused, reserved for future use
	Reserved5 bool // Bit 7: Unused, reserved for future use
}

type RockRidgeExtensions struct {
	// PX - POSIX file permissions (UID, GID, Mode)
	UID         *uint32      // User ID
	GID         *uint32      // Group ID
	Permissions *fs.FileMode // File permissions

	// PN - Device number (if block/char device)
	Major *uint32
	Minor *uint32

	// SL - Symbolic link target path
	SymlinkTarget *string
	SymlinkFlags  *byte // Stores flags for symlink interpretation

	// NM - Alternate name (long filename or case-sensitive filename)
	AlternateNameFlags *NameEntryFlags
	AlternateName      *string

	// CL - Child link LBA (for relocated directories)
	ChildLinkLBA *uint32

	// PL - Parent link LBA (if directory was relocated)
	ParentLinkLBA *uint32

	// RE - Relocated directory flag
	IsRelocated *bool

	// TF - Time stamps (creation, modification, access)
	CreationTime     *time.Time
	ModificationTime *time.Time
	AccessTime       *time.Time

	// SF - Sparse file info (if applicable)
	IsSparse *bool

	// CE - Continuation of the system use area elsewhere on the volume.
	// The parser follows this to merge entries recorded in the
	// continuation area; it is not itself a Rock Ridge attribute.
	Continuation *ContinuationArea
}

// ContinuationArea locates a SUSP continuation of a system use field, as
// carried by a "CE" entry.
type ContinuationArea struct {
	// Logical block number of the continuation area
	Block uint32
	// Byte offset of the continuation within the block
	Offset uint32
	// Length in bytes of the continuation
	Length uint32
}

// HasRockRidge determines if any Rock Ridge extensions were set.
func (r *RockRidgeExtensions) HasRockRidge() bool {
	return r.UID != nil || r.GID != nil || r.Permissions != nil ||
		r.Major != nil || r.Minor != nil || r.SymlinkTarget != nil ||
		r.AlternateName != nil || r.ChildLinkLBA != nil || r.ParentLinkLBA != nil ||
		r.IsRelocated != nil || r.CreationTime != nil || r.ModificationTime != nil ||
		r.AccessTime != nil || r.IsSparse != nil
}

// UnmarshalRockRidge parses a system use field into Rock Ridge extensions.
// If the field ends with a CE entry, the returned extensions carry a
// Continuation pointer the caller must follow (see ParseInto) to pick up
// the remaining entries.
func UnmarshalRockRidge(data []byte) (*RockRidgeExtensions, error) {
	if len(data) < 4 {
		return nil, errors.New("invalid Rock Ridge data")
	}
	rr := &RockRidgeExtensions{}
	if err := rr.ParseInto(data); err != nil {
		return nil, err
	}
	return rr, nil
}

// ParseInto parses a system use area (or a continuation of one) and merges
// the entries into the receiver. NM entries with the continue flag append
// to the accumulated alternate name, as do SL component continuations.
func (rr *RockRidgeExtensions) ParseInto(data []byte) error {
	offset := 0
	// A fresh CE in this area replaces any CE already consumed by the
	// caller; clear it so the caller can detect whether this area chains
	// further.
	rr.Continuation = nil

	for offset+4 <= len(data) {
		sig := string(data[offset : offset+2])
		length := int(data[offset+2])

		// A zero length or an ST entry terminates the area; unknown
		// trailing pad bytes also stop cleanly here.
		if length < 4 || offset+length > len(data) {
			break
		}
		payload := data[offset+4 : offset+length]
		offset += length

		switch RockRidgeEntryType(sig) {
		case POSIX_FILE_PERMS: // PX: mode, nlink, uid, gid (+ optional serial)
			if len(payload) >= 32 {
				if mode, err := encoding.UnmarshalUint32LSBMSB([8]byte(payload[0:8])); err == nil {
					permissions := parseFileMode(mode)
					rr.Permissions = &permissions
				}
				if uid, err := encoding.UnmarshalUint32LSBMSB([8]byte(payload[16:24])); err == nil {
					rr.UID = &uid
				}
				if gid, err := encoding.UnmarshalUint32LSBMSB([8]byte(payload[24:32])); err == nil {
					rr.GID = &gid
				}
			}

		case POSIX_DEVICE_NUM: // PN: major, minor
			if len(payload) >= 16 {
				if major, err := encoding.UnmarshalUint32LSBMSB([8]byte(payload[0:8])); err == nil {
					rr.Major = &major
				}
				if minor, err := encoding.UnmarshalUint32LSBMSB([8]byte(payload[8:16])); err == nil {
					rr.Minor = &minor
				}
			}

		case TIME_STAMPS: // TF: flag byte, then timestamps in flag-bit order
			if len(payload) < 1 {
				break
			}
			flags := payload[0]
			stampLen := 7
			longForm := flags&0x80 != 0
			if longForm {
				stampLen = 17
			}
			pos := 1
			readStamp := func() (time.Time, bool) {
				if pos+stampLen > len(payload) {
					return time.Time{}, false
				}
				var t time.Time
				var err error
				if longForm {
					t, err = encoding.UnmarshalDateTime([17]byte(payload[pos : pos+17]))
				} else {
					t, err = encoding.UnmarshalRecordingDateTime([7]byte(payload[pos : pos+7]))
				}
				pos += stampLen
				return t, err == nil
			}
			// Timestamps appear in flag-bit order: creation, modify,
			// access, attributes, backup, expiration, effective.
			if flags&0x01 != 0 {
				if t, ok := readStamp(); ok {
					rr.CreationTime = &t
				}
			}
			if flags&0x02 != 0 {
				if t, ok := readStamp(); ok {
					rr.ModificationTime = &t
				}
			}
			if flags&0x04 != 0 {
				if t, ok := readStamp(); ok {
					rr.AccessTime = &t
				}
			}
			// Remaining timestamp types (attributes, backup, expiration,
			// effective) are consumed implicitly by stopping here.

		case ALTERNATE_NAME: // NM: flags byte then name content
			if len(payload) < 1 {
				break
			}
			flags := payload[0]
			nameFlags := &NameEntryFlags{
				Continue:  flags&0x01 > 0,
				Current:   flags&0x02 > 0,
				Parent:    flags&0x04 > 0,
				Reserved1: flags&0x08 > 0,
				Reserved2: flags&0x10 > 0,
				Reserved3: flags&0x20 > 0,
				Reserved4: flags&0x40 > 0,
				Reserved5: flags&0x80 > 0,
			}
			// A prior NM with the continue flag set means this content
			// appends to the accumulated name.
			if rr.AlternateName != nil && rr.AlternateNameFlags != nil && rr.AlternateNameFlags.Continue {
				appended := *rr.AlternateName + string(payload[1:])
				rr.AlternateName = &appended
			} else {
				name := string(payload[1:])
				rr.AlternateName = &name
			}
			rr.AlternateNameFlags = nameFlags

		case SYMBOLIC_LINK: // SL: flags byte then component records
			if len(payload) < 1 {
				break
			}
			continued := rr.SymlinkTarget != nil && rr.SymlinkFlags != nil && *rr.SymlinkFlags&0x01 != 0
			prefix := ""
			if continued {
				prefix = *rr.SymlinkTarget
			}
			target := joinSLComponents(prefix, parseSLComponents(payload[1:]))
			flags := payload[0]
			rr.SymlinkFlags = &flags
			rr.SymlinkTarget = &target

		case CHILD_LINK: // CL: LBA of relocated directory
			if len(payload) >= 8 {
				if lba, err := encoding.UnmarshalUint32LSBMSB([8]byte(payload[0:8])); err == nil {
					rr.ChildLinkLBA = &lba
				}
			}

		case PARENT_LINK: // PL: LBA of original parent
			if len(payload) >= 8 {
				if lba, err := encoding.UnmarshalUint32LSBMSB([8]byte(payload[0:8])); err == nil {
					rr.ParentLinkLBA = &lba
				}
			}

		case RELOCATED_DIR: // RE: marker only
			relocated := true
			rr.IsRelocated = &relocated

		case SPARSE_FILE: // SF: marker; virtual size details are not retained
			sparse := true
			rr.IsSparse = &sparse

		case "CE": // SUSP continuation area pointer
			if len(payload) >= 24 {
				block, errB := encoding.UnmarshalUint32LSBMSB([8]byte(payload[0:8]))
				areaOffset, errO := encoding.UnmarshalUint32LSBMSB([8]byte(payload[8:16]))
				areaLen, errL := encoding.UnmarshalUint32LSBMSB([8]byte(payload[16:24]))
				if errB == nil && errO == nil && errL == nil {
					rr.Continuation = &ContinuationArea{Block: block, Offset: areaOffset, Length: areaLen}
				}
			}

		case "ST": // SUSP terminator
			return nil

			// SP, ER, ES, and unknown entries are skipped.
		}
	}

	return nil
}

// parseSLComponents decodes SL component records into path components.
// A root component is represented as "/".
func parseSLComponents(data []byte) []string {
	var components []string
	pos := 0
	for pos+2 <= len(data) {
		flags := data[pos]
		clen := int(data[pos+1])
		pos += 2
		if pos+clen > len(data) {
			break
		}
		content := string(data[pos : pos+clen])
		pos += clen

		switch {
		case flags&0x08 != 0: // root
			components = append(components, "/")
		case flags&0x02 != 0: // current directory
			components = append(components, ".")
		case flags&0x04 != 0: // parent directory
			components = append(components, "..")
		default:
			components = append(components, content)
		}
	}
	return components
}

// joinSLComponents assembles a symlink target from a previously
// accumulated prefix (from an SL entry with the continue flag) and the
// components of the current entry.
func joinSLComponents(prefix string, components []string) string {
	out := prefix
	for _, c := range components {
		switch {
		case c == "/":
			out += "/"
		case out == "" || out == "/":
			out += c
		default:
			out += "/" + c
		}
	}
	return out
}

// MarshalRockRidge serializes Rock Ridge extension fields into a sequence
// of correctly encoded SUSP/RRIP entries. The result is a flat byte
// sequence; callers responsible for directory records must handle
// splitting into continuation areas when the result does not fit the
// record's system use field (see the pack layout engine).
func MarshalRockRidge(rr *RockRidgeExtensions) ([]byte, error) {
	entries, err := BuildRockRidgeEntries(rr)
	if err != nil {
		return nil, err
	}
	var out []byte
	for _, e := range entries {
		out = append(out, e...)
	}
	return out, nil
}

// BuildRockRidgeEntries produces the individual SUSP/RRIP entries for the
// given extensions, in canonical order (RR, PX, PN, SL, NM, CL, PL, RE,
// TF). Entries are returned separately so a layout engine can split them
// between a record's system use field and a continuation area.
func BuildRockRidgeEntries(rr *RockRidgeExtensions) ([][]byte, error) {
	var entries [][]byte
	var rrFlags byte

	if rr.Permissions != nil {
		rrFlags |= 0x01
		var uid, gid uint32
		if rr.UID != nil {
			uid = *rr.UID
		}
		if rr.GID != nil {
			gid = *rr.GID
		}
		entries = append(entries, BuildPX(*rr.Permissions, 1, uid, gid))
	}
	if rr.Major != nil && rr.Minor != nil {
		rrFlags |= 0x02
		entries = append(entries, BuildPN(*rr.Major, *rr.Minor))
	}
	if rr.SymlinkTarget != nil {
		rrFlags |= 0x04
		slEntries, err := BuildSL(*rr.SymlinkTarget)
		if err != nil {
			return nil, err
		}
		entries = append(entries, slEntries...)
	}
	if rr.AlternateName != nil {
		rrFlags |= 0x08
		entries = append(entries, BuildNM(*rr.AlternateName)...)
	}
	if rr.ChildLinkLBA != nil {
		rrFlags |= 0x10
		entries = append(entries, BuildCL(*rr.ChildLinkLBA))
	}
	if rr.ParentLinkLBA != nil {
		rrFlags |= 0x20
		entries = append(entries, BuildPL(*rr.ParentLinkLBA))
	}
	if rr.IsRelocated != nil && *rr.IsRelocated {
		rrFlags |= 0x40
		entries = append(entries, BuildRE())
	}

	var creation, modification, access time.Time
	if rr.CreationTime != nil {
		creation = *rr.CreationTime
	}
	if rr.ModificationTime != nil {
		modification = *rr.ModificationTime
	}
	if rr.AccessTime != nil {
		access = *rr.AccessTime
	}
	if !creation.IsZero() || !modification.IsZero() || !access.IsZero() {
		rrFlags |= 0x80
		tf, err := BuildTF(creation, modification, access)
		if err != nil {
			return nil, err
		}
		entries = append(entries, tf)
	}

	if len(entries) == 0 {
		return nil, nil
	}
	// The legacy RR entry announcing which fields follow leads the area.
	return append([][]byte{BuildRR(rrFlags)}, entries...), nil
}

// parseFileMode converts a 32-bit unsigned integer into an fs.FileMode struct
func parseFileMode(mode uint32) fs.FileMode {
	var fileMode fs.FileMode

	// File type bits
	switch mode & 0xF000 {
	case 0xC000:
		fileMode |= fs.ModeSocket
	case 0xA000:
		fileMode |= fs.ModeSymlink
	case 0x8000:
		// Regular file, no specific fs flag needed
	case 0x6000:
		fileMode |= fs.ModeDevice
	case 0x2000:
		fileMode |= fs.ModeCharDevice
	case 0x4000:
		fileMode |= fs.ModeDir
	case 0x1000:
		fileMode |= fs.ModeNamedPipe
	}

	// Permission bits
	if mode&0x0100 != 0 {
		fileMode |= 0400 // S_IRUSR
	}
	if mode&0x0080 != 0 {
		fileMode |= 0200 // S_IWUSR
	}
	if mode&0x0040 != 0 {
		fileMode |= 0100 // S_IXUSR
	}
	if mode&0x0020 != 0 {
		fileMode |= 0040 // S_IRGRP
	}
	if mode&0x0010 != 0 {
		fileMode |= 0020 // S_IWGRP
	}
	if mode&0x0008 != 0 {
		fileMode |= 0010 // S_IXGRP
	}
	if mode&0x0004 != 0 {
		fileMode |= 0004 // S_IROTH
	}
	if mode&0x0002 != 0 {
		fileMode |= 0002 // S_IWOTH
	}
	if mode&0x0001 != 0 {
		fileMode |= 0001 // S_IXOTH
	}

	// Special mode bits
	if mode&0x0800 != 0 {
		fileMode |= os.ModeSetuid
	}
	if mode&0x0400 != 0 {
		fileMode |= os.ModeSetgid
	}
	if mode&0x0200 != 0 {
		fileMode |= os.ModeSticky
	}

	return fileMode
}
