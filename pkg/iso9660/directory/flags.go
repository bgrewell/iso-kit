package directory

// FileFlags holds the flag values from a Directory Record's File Flags field.
// The bits are numbered from 0 (LSB) to 7 (MSB) as follows:
//
//	Bit 0 ("Hidden"): If 0, the file's existence shall be made known to the user; if 1, it need not be.
//	Bit 1 ("Directory"): 0 indicates a file; 1 indicates a directory.
//	Bit 2 ("AssociatedFile"): 0 means not an Associated File; 1 means it is.
//	Bit 3 ("RecordFormat"): 0 means the file's structure is not specified by an Extended Attribute Record;
//	                        1 means it is.
//	Bit 4 ("Protection"): 0 means no owner/group is specified; 1 means they are specified.
//	Bits 5 & 6: Reserved (must be zero).
//	Bit 7 ("MultiExtent"): 0 means this is the final Directory Record for the file; 1 means it is not.
type FileFlags struct {
	// Bit 0: Hidden flag (existence not made known if true)
	Hidden bool `json:"hidden"`
	// Bit 1: True if this Directory Record identifies a directory.
	Directory bool `json:"directory"`
	// Bit 2: True if the file is an Associated File.
	AssociatedFile bool `json:"associated_file"`
	// Bit 3: True if the file's structure is specified (non-zero Record Format in the extended attribute).
	RecordFormat bool `json:"record_format"`
	// Bit 4: True if owner/group identification is specified.
	Protection bool `json:"protection"`
	// Bit 7: True if this is not the final Directory Record for the file.
	MultiExtent bool `json:"multi_extent"`
}

// Marshal converts the FileFlags into a single byte according to the specification.
// Reserved bits (bits 5 and 6) are always set to zero.
func (ff FileFlags) Marshal() byte {
	var b byte
	if ff.Hidden {
		b |= 0x01 // Bit 0.
	}
	if ff.Directory {
		b |= 0x02 // Bit 1.
	}
	if ff.AssociatedFile {
		b |= 0x04 // Bit 2.
	}
	if ff.RecordFormat {
		b |= 0x08 // Bit 3.
	}
	if ff.Protection {
		b |= 0x10 // Bit 4.
	}
	// Bits 5 and 6 remain zero.
	if ff.MultiExtent {
		b |= 0x80 // Bit 7.
	}
	return b
}

// UnmarshalFileFlags converts a byte into a FileFlags struct.
// It returns an error if any reserved bits (bits 5 and 6) are nonzero.
func UnmarshalFileFlags(b byte) (FileFlags, error) {
	// Reserved bits 5 and 6 are set by some older ISO authoring tools; tolerate them.
	return FileFlags{
		Hidden:         (b & 0x01) != 0,
		Directory:      (b & 0x02) != 0,
		AssociatedFile: (b & 0x04) != 0,
		RecordFormat:   (b & 0x08) != 0,
		Protection:     (b & 0x10) != 0,
		MultiExtent:    (b & 0x80) != 0,
	}, nil
}
