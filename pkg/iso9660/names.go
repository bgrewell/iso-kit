package iso9660

import (
	"fmt"
	"strings"

	"github.com/bgrewell/iso-kit/pkg/iso9660/encoding"
	"github.com/bgrewell/iso-kit/pkg/iso9660/tree"
)

// Maximum identifier length (excluding the ";1" version suffix) when
// mangling for Rock Ridge images. ISO 9660 Level 2 allows up to 31
// characters; the original name is preserved in the NM entry.
const mangledIdentifierMaxLen = 31

// mangleISOName converts a POSIX name into a valid ISO 9660 identifier:
// uppercase d-characters, at most one "." separator (files only), length
// capped at mangledIdentifierMaxLen. Used when Rock Ridge is enabled, in
// which case the original name travels in the NM entry.
func mangleISOName(name string, isDir bool) string {
	base, ext := name, ""
	if !isDir {
		if i := strings.LastIndexByte(name, '.'); i > 0 && i < len(name)-1 {
			base, ext = name[:i], name[i+1:]
		}
	}

	mangle := func(s string) string {
		out := make([]byte, 0, len(s))
		for _, r := range strings.ToUpper(s) {
			switch {
			case r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
				out = append(out, byte(r))
			default:
				out = append(out, '_')
			}
		}
		return string(out)
	}

	base = mangle(base)
	ext = mangle(ext)

	if isDir || ext == "" {
		if len(base) > mangledIdentifierMaxLen {
			base = base[:mangledIdentifierMaxLen]
		}
		if base == "" {
			base = "_"
		}
		return base
	}

	// Keep the extension (up to 3 characters is conventional; longer is
	// tolerated by Level 2 readers, but trim to keep the total in range).
	if len(ext) > 3 {
		ext = ext[:3]
	}
	maxBase := mangledIdentifierMaxLen - len(ext) - 1
	if len(base) > maxBase {
		base = base[:maxBase]
	}
	if base == "" {
		base = "_"
	}
	return base + "." + ext
}

// assignIdentifiers produces the on-disk identifier for every child of a
// directory. With mangling enabled (Rock Ridge), names are converted to
// valid identifiers and collisions are resolved deterministically with a
// numeric tail; otherwise names are used as-is. File identifiers carry the
// ";1" version suffix.
func assignIdentifiers(children []*tree.Node, mangled bool) map[*tree.Node]string {
	out := make(map[*tree.Node]string, len(children))
	used := make(map[string]bool, len(children))

	for _, child := range children {
		var id string
		if mangled {
			id = mangleISOName(child.Name(), child.IsDir())
			if used[id] {
				id = dedupeIdentifier(id, child.IsDir(), used)
			}
		} else {
			id = child.Name()
		}
		used[id] = true
		if !child.IsDir() {
			id += ";1"
		}
		out[child] = id
	}
	return out
}

// Joliet allows 64 UCS-2 characters per identifier.
const jolietNameMaxLen = 64

// jolietName sanitizes a name for a Joliet directory record: the
// characters forbidden by the Joliet specification are replaced with '_'
// and the name is truncated to 64 UTF-16 code units. Case is preserved.
func jolietName(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r < 0x20, r == '*', r == '/', r == ':', r == ';', r == '?', r == '\\':
			out = append(out, '_')
		default:
			out = append(out, r)
		}
	}
	// Truncation is by UTF-16 units; supplementary-plane runes cost two.
	units := 0
	for i, r := range out {
		cost := 1
		if r > 0xFFFF {
			cost = 2
		}
		if units+cost > jolietNameMaxLen {
			out = out[:i]
			break
		}
		units += cost
	}
	if len(out) == 0 {
		return "_"
	}
	return string(out)
}

// assignJolietIdentifiers produces the UCS-2-encoded on-disk identifier
// for every child of a directory, deduplicating collisions caused by
// sanitization or truncation. File identifiers carry the ";1" version
// suffix before encoding.
func assignJolietIdentifiers(children []*tree.Node) map[*tree.Node]string {
	out := make(map[*tree.Node]string, len(children))
	used := make(map[string]bool, len(children))

	for _, child := range children {
		name := jolietName(child.Name())
		if used[name] {
			for n := 1; ; n++ {
				tail := fmt.Sprintf("~%d", n)
				trimmed := name
				if len(trimmed)+len(tail) > jolietNameMaxLen {
					trimmed = trimmed[:jolietNameMaxLen-len(tail)]
				}
				if candidate := trimmed + tail; !used[candidate] {
					name = candidate
					break
				}
			}
		}
		used[name] = true
		if !child.IsDir() {
			name += ";1"
		}
		out[child] = string(encoding.EncodeUCS2BigEndian(name))
	}
	return out
}

// dedupeIdentifier resolves an identifier collision by replacing the tail
// of the base name with ~N, preserving the extension.
func dedupeIdentifier(id string, isDir bool, used map[string]bool) string {
	base, ext := id, ""
	if !isDir {
		if i := strings.LastIndexByte(id, '.'); i > 0 {
			base, ext = id[:i], id[i:]
		}
	}
	for n := 1; ; n++ {
		tail := fmt.Sprintf("~%d", n)
		trimmed := base
		if len(trimmed)+len(tail)+len(ext) > mangledIdentifierMaxLen {
			trimmed = trimmed[:mangledIdentifierMaxLen-len(tail)-len(ext)]
		}
		candidate := trimmed + tail + ext
		if !used[candidate] {
			return candidate
		}
	}
}
