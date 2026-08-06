package extensions

import (
	"fmt"
	"io/fs"
	"time"

	"github.com/bgrewell/iso-kit/pkg/iso9660/encoding"
)

// System Use Sharing Protocol (SUSP, IEEE P1281) and Rock Ridge
// Interchange Protocol (RRIP, IEEE P1282) entry builders. Every entry
// shares a 4-byte header: signature (2), length (1), version (1).

const (
	SUSP_VERSION = 1

	// RRIP ER identification strings (RRIP 1.09, as written by mkisofs
	// and libisofs).
	RRIP_ER_ID         = "RRIP_1991A"
	RRIP_ER_DESCRIPTOR = "THE ROCK RIDGE INTERCHANGE PROTOCOL PROVIDES SUPPORT FOR POSIX FILE SYSTEM SEMANTICS"
	RRIP_ER_SOURCE     = "PLEASE CONTACT DISC PUBLISHER FOR SPECIFICATION SOURCE.  SEE PUBLISHER IDENTIFIER IN PRIMARY VOLUME DESCRIPTOR FOR CONTACT INFORMATION."

	// TF flag bits (RRIP 4.1.6).
	TF_CREATION   = 0x01
	TF_MODIFY     = 0x02
	TF_ACCESS     = 0x04
	TF_ATTRIBUTES = 0x08
	TF_BACKUP     = 0x10
	TF_EXPIRATION = 0x20
	TF_EFFECTIVE  = 0x40
	TF_LONG_FORM  = 0x80

	// NM/SL component flag bits.
	NM_CONTINUE = 0x01
	NM_CURRENT  = 0x02
	NM_PARENT   = 0x04

	SL_COMPONENT_CONTINUE = 0x01
	SL_COMPONENT_CURRENT  = 0x02
	SL_COMPONENT_PARENT   = 0x04
	SL_COMPONENT_ROOT     = 0x08

	// The system use field lives inside a directory record, which is
	// itself capped at 255 bytes; entries must be built small enough to
	// fit either inline or in a continuation area within one sector.
	MAX_ENTRY_PAYLOAD = 240
)

func suspHeader(signature string, length int) []byte {
	return []byte{signature[0], signature[1], byte(length), SUSP_VERSION}
}

// BuildSP returns the SUSP "SP" entry (7 bytes) that announces System Use
// Sharing Protocol use. It must appear in the system use area of the root
// directory's "." record. skip is the number of bytes to skip at the start
// of each system use field (0 unless interoperating with legacy layouts).
func BuildSP(skip byte) []byte {
	entry := suspHeader("SP", 7)
	return append(entry, 0xBE, 0xEF, skip)
}

// BuildCE returns the SUSP "CE" entry (28 bytes) pointing at a
// continuation of the system use area: block is the logical block number
// of the continuation, offset the starting byte within that block, and
// length the number of continuation bytes.
func BuildCE(block, offset, length uint32) []byte {
	entry := suspHeader("CE", 28)
	blockBytes := encoding.MarshalBothByteOrders32(block)
	offsetBytes := encoding.MarshalBothByteOrders32(offset)
	lengthBytes := encoding.MarshalBothByteOrders32(length)
	entry = append(entry, blockBytes[:]...)
	entry = append(entry, offsetBytes[:]...)
	entry = append(entry, lengthBytes[:]...)
	return entry
}

// BuildER returns the SUSP "ER" entry identifying the Rock Ridge
// extensions in use. It is conventionally placed in a continuation area
// referenced from the root "." record, since at 237 bytes it rarely fits
// inline.
func BuildER() []byte {
	id, desc, src := RRIP_ER_ID, RRIP_ER_DESCRIPTOR, RRIP_ER_SOURCE
	length := 8 + len(id) + len(desc) + len(src)
	entry := suspHeader("ER", length)
	entry = append(entry, byte(len(id)), byte(len(desc)), byte(len(src)), 1)
	entry = append(entry, id...)
	entry = append(entry, desc...)
	entry = append(entry, src...)
	return entry
}

// BuildST returns the SUSP "ST" entry (4 bytes) terminating a system use
// area. Optional under SUSP 1.12; provided for completeness.
func BuildST() []byte {
	return suspHeader("ST", 4)
}

// BuildRR returns the legacy "RR" entry (5 bytes) from RRIP 1.09 whose
// flag byte advertises which RRIP entries follow in this system use area.
// Widely written by mkisofs for compatibility with older readers.
// Flag bits: 0=PX, 1=PN, 2=SL, 3=NM, 4=CL, 5=PL, 6=RE, 7=TF.
func BuildRR(flags byte) []byte {
	entry := suspHeader("RR", 5)
	return append(entry, flags)
}

// BuildPX returns the RRIP "PX" entry (36 bytes, RRIP 1.09 form without
// the file serial number) carrying POSIX mode, link count, uid, and gid.
func BuildPX(mode fs.FileMode, nlink, uid, gid uint32) []byte {
	entry := suspHeader("PX", 36)
	modeBytes := encoding.MarshalBothByteOrders32(PosixMode(mode))
	nlinkBytes := encoding.MarshalBothByteOrders32(nlink)
	uidBytes := encoding.MarshalBothByteOrders32(uid)
	gidBytes := encoding.MarshalBothByteOrders32(gid)
	entry = append(entry, modeBytes[:]...)
	entry = append(entry, nlinkBytes[:]...)
	entry = append(entry, uidBytes[:]...)
	entry = append(entry, gidBytes[:]...)
	return entry
}

// BuildPN returns the RRIP "PN" entry (20 bytes) carrying the major and
// minor device numbers of a block or character device node.
func BuildPN(major, minor uint32) []byte {
	entry := suspHeader("PN", 20)
	majorBytes := encoding.MarshalBothByteOrders32(major)
	minorBytes := encoding.MarshalBothByteOrders32(minor)
	entry = append(entry, majorBytes[:]...)
	entry = append(entry, minorBytes[:]...)
	return entry
}

// BuildNM returns one or more RRIP "NM" entries carrying the alternate
// (POSIX) name. Names longer than a single entry's capacity are split
// across multiple entries chained with the continue flag.
func BuildNM(name string) [][]byte {
	var entries [][]byte
	remaining := []byte(name)
	for {
		chunk := remaining
		flags := byte(0)
		if len(chunk) > MAX_ENTRY_PAYLOAD {
			chunk = chunk[:MAX_ENTRY_PAYLOAD]
			flags = NM_CONTINUE
		}
		entry := suspHeader("NM", 5+len(chunk))
		entry = append(entry, flags)
		entry = append(entry, chunk...)
		entries = append(entries, entry)
		remaining = remaining[len(chunk):]
		if len(remaining) == 0 {
			return entries
		}
	}
}

// BuildSL returns one or more RRIP "SL" entries encoding a symbolic link
// target as component records. Each component is (flags, length, content);
// "/" produces a root component, "." current, ".." parent.
func BuildSL(target string) ([][]byte, error) {
	var components [][]byte
	appendComponent := func(flags byte, content string) {
		components = append(components, append([]byte{flags, byte(len(content))}, content...))
	}

	rest := target
	if len(rest) > 0 && rest[0] == '/' {
		appendComponent(SL_COMPONENT_ROOT, "")
		for len(rest) > 0 && rest[0] == '/' {
			rest = rest[1:]
		}
	}
	for _, part := range splitSlash(rest) {
		switch part {
		case "":
			continue
		case ".":
			appendComponent(SL_COMPONENT_CURRENT, "")
		case "..":
			appendComponent(SL_COMPONENT_PARENT, "")
		default:
			if len(part) > MAX_ENTRY_PAYLOAD {
				return nil, fmt.Errorf("symlink path component too long (%d bytes): %q", len(part), part)
			}
			appendComponent(0, part)
		}
	}

	// Pack whole components into SL entries; a component never splits
	// across entries here, so each entry's flag is either 0 (final) or
	// continue.
	var entries [][]byte
	var payload []byte
	flush := func(more bool) {
		flags := byte(0)
		if more {
			flags = SL_COMPONENT_CONTINUE
		}
		entry := suspHeader("SL", 5+len(payload))
		entry = append(entry, flags)
		entry = append(entry, payload...)
		entries = append(entries, entry)
		payload = nil
	}
	for _, comp := range components {
		if len(payload)+len(comp) > MAX_ENTRY_PAYLOAD {
			flush(true)
		}
		payload = append(payload, comp...)
	}
	flush(false)
	return entries, nil
}

// BuildCL returns the RRIP "CL" entry (12 bytes) linking a relocated
// directory's placeholder to its actual extent.
func BuildCL(lba uint32) []byte {
	entry := suspHeader("CL", 12)
	lbaBytes := encoding.MarshalBothByteOrders32(lba)
	return append(entry, lbaBytes[:]...)
}

// BuildPL returns the RRIP "PL" entry (12 bytes) linking a relocated
// directory's ".." record back to its original parent.
func BuildPL(lba uint32) []byte {
	entry := suspHeader("PL", 12)
	lbaBytes := encoding.MarshalBothByteOrders32(lba)
	return append(entry, lbaBytes[:]...)
}

// BuildRE returns the RRIP "RE" entry (4 bytes) marking a relocated
// directory.
func BuildRE() []byte {
	return suspHeader("RE", 4)
}

// BuildTF returns the RRIP "TF" entry carrying timestamps in the 7-byte
// recording date/time form. Zero times are omitted from the entry.
func BuildTF(creation, modification, access time.Time) ([]byte, error) {
	var flags byte
	var stamps []time.Time
	add := func(t time.Time, bit byte) {
		if !t.IsZero() {
			flags |= bit
			stamps = append(stamps, t)
		}
	}
	add(creation, TF_CREATION)
	add(modification, TF_MODIFY)
	add(access, TF_ACCESS)

	entry := suspHeader("TF", 5+7*len(stamps))
	entry = append(entry, flags)
	for _, t := range stamps {
		stamp, err := encoding.MarshalRecordingDateTime(t)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal TF timestamp: %w", err)
		}
		entry = append(entry, stamp[:]...)
	}
	return entry, nil
}

// PosixMode converts an fs.FileMode into POSIX st_mode bits (file type and
// permissions), the inverse of parseFileMode.
func PosixMode(mode fs.FileMode) uint32 {
	var m uint32

	switch {
	case mode&fs.ModeSymlink != 0:
		m = 0xA000 // S_IFLNK
	case mode&fs.ModeDir != 0:
		m = 0x4000 // S_IFDIR
	case mode&fs.ModeSocket != 0:
		m = 0xC000 // S_IFSOCK
	case mode&fs.ModeCharDevice != 0:
		m = 0x2000 // S_IFCHR
	case mode&fs.ModeDevice != 0:
		m = 0x6000 // S_IFBLK
	case mode&fs.ModeNamedPipe != 0:
		m = 0x1000 // S_IFIFO
	default:
		m = 0x8000 // S_IFREG
	}

	m |= uint32(mode.Perm())
	if mode&fs.ModeSetuid != 0 {
		m |= 0x0800
	}
	if mode&fs.ModeSetgid != 0 {
		m |= 0x0400
	}
	if mode&fs.ModeSticky != 0 {
		m |= 0x0200
	}
	return m
}

func splitSlash(s string) []string {
	var parts []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '/' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return parts
}
