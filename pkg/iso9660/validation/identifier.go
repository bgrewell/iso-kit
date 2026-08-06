package validation

import (
	"fmt"
	"strings"
)

// Interchange levels defined by ECMA-119 / ISO 9660 section 10. Level 0
// is used by this library to mean "relaxed": no identifier validation is
// applied.
const (
	InterchangeLevelRelaxed = 0
	// Level 1: file names are at most 8 d-characters with an extension of
	// at most 3; directory identifiers are at most 8 d-characters. Each
	// file is a single extent.
	InterchangeLevel1 = 1
	// Level 2: identifiers may use the full 30/31-character budget; each
	// file is still a single extent.
	InterchangeLevel2 = 2
	// Level 3: as level 2, but files may consist of multiple extents.
	InterchangeLevel3 = 3
)

// Maximum length of a file identifier (file name + separator + extension,
// excluding the version suffix) at interchange levels 2 and 3.
const maxIdentifierLenLevel2 = 31

// ValidateFileIdentifier checks an on-disk identifier (without the ";1"
// version suffix) against the constraints of the given interchange level.
// Level 0 (relaxed) accepts anything.
func ValidateFileIdentifier(identifier string, isDir bool, level int) error {
	if level == InterchangeLevelRelaxed {
		return nil
	}
	if identifier == "" {
		return fmt.Errorf("empty identifier")
	}

	if isDir {
		if err := ValidateDCharacters(identifier, false); err != nil {
			return fmt.Errorf("directory identifier %q: %w", identifier, err)
		}
		max := maxIdentifierLenLevel2
		if level == InterchangeLevel1 {
			max = 8
		}
		if len(identifier) > max {
			return fmt.Errorf("directory identifier %q exceeds %d characters (interchange level %d)", identifier, max, level)
		}
		return nil
	}

	name, ext := identifier, ""
	if i := strings.LastIndexByte(identifier, '.'); i >= 0 {
		name, ext = identifier[:i], identifier[i+1:]
	}
	if err := ValidateDCharacters(name, false); err != nil {
		return fmt.Errorf("file identifier %q: %w", identifier, err)
	}
	if err := ValidateDCharacters(ext, false); err != nil {
		return fmt.Errorf("file identifier %q: %w", identifier, err)
	}

	switch level {
	case InterchangeLevel1:
		if len(name) > 8 || len(ext) > 3 {
			return fmt.Errorf("file identifier %q exceeds 8.3 format (interchange level 1)", identifier)
		}
	default:
		if len(identifier) > maxIdentifierLenLevel2 {
			return fmt.Errorf("file identifier %q exceeds %d characters (interchange level %d)", identifier, maxIdentifierLenLevel2, level)
		}
	}
	return nil
}
