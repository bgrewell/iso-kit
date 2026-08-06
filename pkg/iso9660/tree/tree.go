// Package tree provides a mutable in-memory representation of an ISO 9660
// directory hierarchy. It is the single source of truth for filesystem state
// between Open/Create and Save: entries parsed from an existing image and
// entries added in memory coexist in the same structure, and the layout
// engine walks it to assign sector locations before writing.
package tree

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// Node is a single file or directory in the tree.
//
// File content comes from exactly one of two sources: Data (in-memory,
// pending) or a backing io.ReaderAt plus sector Location (content that
// already lives in an opened image). Directories have neither.
type Node struct {
	name     string
	isDir    bool
	parent   *Node
	children map[string]*Node

	// In-memory content for files added or replaced since Open/Create.
	data []byte
	// Backing reader for content already recorded in an existing image.
	reader io.ReaderAt
	// Sector location of the extent in the backing reader.
	location uint32
	// Content size in bytes. For reader-backed files this is the extent
	// data length; for in-memory files it mirrors len(data).
	size uint32

	mode    os.FileMode
	modTime time.Time
	uid     uint32
	gid     uint32

	// Symlink target; non-empty only for symbolic link nodes.
	symlinkTarget string

	// PackedLocation and PackedSize are assigned by the layout engine
	// before writing. PackedSize for a directory is the extent size in
	// bytes (a multiple of the sector size); for a file it equals size.
	PackedLocation uint32
	PackedSize     uint32
}

// NewRoot returns an empty root directory node.
func NewRoot() *Node {
	return &Node{
		isDir:    true,
		children: map[string]*Node{},
		mode:     0o755,
		modTime:  time.Now(),
	}
}

// Name returns the node's name. The root has an empty name.
func (n *Node) Name() string { return n.name }

// IsDir reports whether the node is a directory.
func (n *Node) IsDir() bool { return n.isDir }

// IsRoot reports whether the node is the tree root.
func (n *Node) IsRoot() bool { return n.parent == nil }

// Parent returns the parent node, or nil for the root.
func (n *Node) Parent() *Node { return n.parent }

// Size returns the content size in bytes. Directories return 0.
func (n *Node) Size() uint32 { return n.size }

// Mode returns the POSIX file mode.
func (n *Node) Mode() os.FileMode { return n.mode }

// ModTime returns the modification time.
func (n *Node) ModTime() time.Time { return n.modTime }

// SetMode sets the POSIX file mode.
func (n *Node) SetMode(mode os.FileMode) { n.mode = mode }

// SetModTime sets the modification time.
func (n *Node) SetModTime(t time.Time) { n.modTime = t }

// UID returns the POSIX user ID (0 unless set).
func (n *Node) UID() uint32 { return n.uid }

// GID returns the POSIX group ID (0 unless set).
func (n *Node) GID() uint32 { return n.gid }

// SetOwnership sets the POSIX user and group IDs.
func (n *Node) SetOwnership(uid, gid uint32) { n.uid, n.gid = uid, gid }

// IsSymlink reports whether the node is a symbolic link.
func (n *Node) IsSymlink() bool { return n.symlinkTarget != "" }

// SymlinkTarget returns the symlink target path, or "" for non-links.
func (n *Node) SymlinkTarget() string { return n.symlinkTarget }

// IsPending reports whether the node's content lives in memory rather than
// in a backing image.
func (n *Node) IsPending() bool { return n.data != nil }

// FullPath returns the slash-separated path from the root, e.g.
// "dir/subdir/file.txt". The root returns "".
func (n *Node) FullPath() string {
	if n.parent == nil {
		return ""
	}
	parts := []string{}
	for cur := n; cur.parent != nil; cur = cur.parent {
		parts = append(parts, cur.name)
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, "/")
}

// Children returns the node's children sorted by name. Sorting makes the
// on-disk record order deterministic and satisfies the ECMA-119 requirement
// that directory records be ordered by file identifier.
func (n *Node) Children() []*Node {
	out := make([]*Node, 0, len(n.children))
	for _, c := range n.children {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// splitPath normalizes a slash-separated path into components. Leading and
// trailing slashes are ignored, as are empty components.
func splitPath(path string) []string {
	var parts []string
	for _, p := range strings.Split(path, "/") {
		if p != "" && p != "." {
			parts = append(parts, p)
		}
	}
	return parts
}

// StripVersion removes an ISO 9660 file version suffix (";1", ";2", ...)
// from a name if present.
func StripVersion(name string) string {
	if i := strings.LastIndexByte(name, ';'); i >= 0 {
		version := name[i+1:]
		if len(version) > 0 {
			numeric := true
			for _, r := range version {
				if r < '0' || r > '9' {
					numeric = false
					break
				}
			}
			if numeric {
				return name[:i]
			}
		}
	}
	return name
}

// Lookup returns the node at the given path, or nil if not present. A file
// version suffix on the final component is ignored, so "A.TXT;1" and
// "A.TXT" resolve to the same node.
func (n *Node) Lookup(path string) *Node {
	cur := n
	for _, part := range splitPath(path) {
		if cur == nil || !cur.isDir {
			return nil
		}
		next := cur.children[part]
		if next == nil {
			next = cur.children[StripVersion(part)]
		}
		cur = next
	}
	return cur
}

// mkdirs walks the path components, creating intermediate directories as
// needed, and returns the final directory node. Returns an error if a
// component exists as a file.
func (n *Node) mkdirs(parts []string) (*Node, error) {
	cur := n
	for _, part := range parts {
		child := cur.children[part]
		if child == nil {
			child = &Node{
				name:     part,
				isDir:    true,
				parent:   cur,
				children: map[string]*Node{},
				mode:     0o755,
				modTime:  time.Now(),
			}
			cur.children[part] = child
		} else if !child.isDir {
			return nil, fmt.Errorf("path component %q is a file, not a directory", child.FullPath())
		}
		cur = child
	}
	return cur, nil
}

// AddDirectory creates a directory at the given path, along with any
// missing parents, and returns its node. Adding an existing directory is a
// no-op that returns the existing node.
func (n *Node) AddDirectory(path string) (*Node, error) {
	parts := splitPath(path)
	if len(parts) == 0 {
		return n, nil
	}
	return n.mkdirs(parts)
}

// AddFile inserts a file with in-memory content at the given path, creating
// parent directories as needed. An existing file at the path is replaced;
// an existing directory is an error.
func (n *Node) AddFile(path string, data []byte) (*Node, error) {
	parts := splitPath(path)
	if len(parts) == 0 {
		return nil, fmt.Errorf("file path is empty")
	}
	name := StripVersion(parts[len(parts)-1])
	if name == "" {
		return nil, fmt.Errorf("file name is empty")
	}
	dir, err := n.mkdirs(parts[:len(parts)-1])
	if err != nil {
		return nil, err
	}
	if existing := dir.children[name]; existing != nil && existing.isDir {
		return nil, fmt.Errorf("path %q is a directory", existing.FullPath())
	}
	node := &Node{
		name:    name,
		parent:  dir,
		data:    data,
		size:    uint32(len(data)),
		mode:    0o644,
		modTime: time.Now(),
	}
	dir.children[name] = node
	return node, nil
}

// AddSymlink inserts a symbolic link node at the given path pointing at
// target, creating parent directories as needed. The link itself carries
// no content; the target is recorded verbatim (Rock Ridge SL entry on
// write).
func (n *Node) AddSymlink(path, target string) (*Node, error) {
	if target == "" {
		return nil, fmt.Errorf("symlink target is empty")
	}
	parts := splitPath(path)
	if len(parts) == 0 {
		return nil, fmt.Errorf("symlink path is empty")
	}
	name := StripVersion(parts[len(parts)-1])
	dir, err := n.mkdirs(parts[:len(parts)-1])
	if err != nil {
		return nil, err
	}
	if existing := dir.children[name]; existing != nil && existing.isDir {
		return nil, fmt.Errorf("path %q is a directory", existing.FullPath())
	}
	node := &Node{
		name:          name,
		parent:        dir,
		symlinkTarget: target,
		mode:          os.ModeSymlink | 0o777,
		modTime:       time.Now(),
	}
	dir.children[name] = node
	return node, nil
}

// AddExistingFile inserts a file whose content is backed by a reader at a
// sector location, used when building the tree from a parsed image.
func (n *Node) AddExistingFile(path string, reader io.ReaderAt, location, size uint32, mode os.FileMode, modTime time.Time) (*Node, error) {
	parts := splitPath(path)
	if len(parts) == 0 {
		return nil, fmt.Errorf("file path is empty")
	}
	name := StripVersion(parts[len(parts)-1])
	dir, err := n.mkdirs(parts[:len(parts)-1])
	if err != nil {
		return nil, err
	}
	if existing := dir.children[name]; existing != nil && existing.isDir {
		return nil, fmt.Errorf("path %q is a directory", existing.FullPath())
	}
	node := &Node{
		name:     name,
		parent:   dir,
		reader:   reader,
		location: location,
		size:     size,
		mode:     mode,
		modTime:  modTime,
	}
	dir.children[name] = node
	return node, nil
}

// Remove deletes the node at the given path. Removing a directory removes
// its entire subtree. Removing the root or a missing path is an error.
func (n *Node) Remove(path string) error {
	target := n.Lookup(path)
	if target == nil {
		return fmt.Errorf("path not found: %s", path)
	}
	if target.parent == nil {
		return fmt.Errorf("cannot remove the root directory")
	}
	delete(target.parent.children, target.name)
	return nil
}

// Walk visits every node under n (excluding n itself) in depth-first
// order, directories before their contents. Returning an error from fn
// stops the walk.
func (n *Node) Walk(fn func(node *Node) error) error {
	for _, child := range n.Children() {
		if err := fn(child); err != nil {
			return err
		}
		if child.isDir {
			if err := child.Walk(fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// Directories returns all directory nodes in breadth-first order starting
// with (and including) n. This is the ordering required for path table
// records: each parent appears before any of its children.
func (n *Node) Directories() []*Node {
	dirs := []*Node{n}
	for i := 0; i < len(dirs); i++ {
		for _, child := range dirs[i].Children() {
			if child.isDir {
				dirs = append(dirs, child)
			}
		}
	}
	return dirs
}

// Content returns a reader over the file's content, streaming from the
// backing image when the content is not in memory. Calling Content on a
// directory is an error.
func (n *Node) Content(sectorSize int64) (io.Reader, error) {
	if n.isDir {
		return nil, fmt.Errorf("cannot read content of directory %q", n.FullPath())
	}
	if n.data != nil {
		return bytes.NewReader(n.data), nil
	}
	if n.reader == nil {
		if n.size == 0 {
			return bytes.NewReader(nil), nil
		}
		return nil, fmt.Errorf("file %q has no content source", n.FullPath())
	}
	return io.NewSectionReader(n.reader, int64(n.location)*sectorSize, int64(n.size)), nil
}

// ContentReaderAt returns an io.ReaderAt over the file's content with
// offset 0 at the start of the file, regardless of where the content is
// stored. Calling ContentReaderAt on a directory is an error.
func (n *Node) ContentReaderAt(sectorSize int64) (io.ReaderAt, error) {
	if n.isDir {
		return nil, fmt.Errorf("cannot read content of directory %q", n.FullPath())
	}
	if n.data != nil {
		return bytes.NewReader(n.data), nil
	}
	if n.reader == nil {
		if n.size == 0 {
			return bytes.NewReader(nil), nil
		}
		return nil, fmt.Errorf("file %q has no content source", n.FullPath())
	}
	return io.NewSectionReader(n.reader, int64(n.location)*sectorSize, int64(n.size)), nil
}

// ReadData returns the file's content, either from memory or from the
// backing reader. Calling ReadData on a directory is an error.
func (n *Node) ReadData(sectorSize int64) ([]byte, error) {
	if n.isDir {
		return nil, fmt.Errorf("cannot read data of directory %q", n.FullPath())
	}
	if n.data != nil {
		return n.data, nil
	}
	if n.reader == nil {
		if n.size == 0 {
			return []byte{}, nil
		}
		return nil, fmt.Errorf("file %q has no content source", n.FullPath())
	}
	buf := make([]byte, n.size)
	if n.size == 0 {
		return buf, nil
	}
	if _, err := n.reader.ReadAt(buf, int64(n.location)*sectorSize); err != nil {
		return nil, fmt.Errorf("failed to read content of %q: %w", n.FullPath(), err)
	}
	return buf, nil
}
