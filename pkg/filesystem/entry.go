package filesystem

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/bgrewell/iso-kit/pkg/consts"
	"github.com/bgrewell/iso-kit/pkg/iso9660/directory"
	"io"
	"os"
	"path/filepath"
	"time"
)

// NewFileSystemEntry initializes a FileSystemEntry with a reader
func NewFileSystemEntry(name, fullPath string, isDir bool, size uint64, location uint32, uid *uint32, gid *uint32, mode os.FileMode, createTime, modTime time.Time, record *directory.DirectoryRecord, reader io.ReaderAt) *FileSystemEntry {
	return &FileSystemEntry{
		Name:       name,
		FullPath:   fullPath,
		IsDir:      isDir,
		Size:       size,
		Location:   location,
		UID:        uid,
		GID:        gid,
		Mode:       mode,
		CreateTime: createTime,
		ModTime:    modTime,
		record:     record,
		reader:     reader,
	}
}

type FileSystemEntry struct {
	// The name of the file or directory and any extension
	Name string `json:"name"`
	// Full path, e.g., "dir/subdir/file.txt"
	FullPath string `json:"full_path"`
	// IsDir, true if it's a directory
	IsDir bool `json:"is_dir"`
	// Size of the file, 0 if it's a directory. Multi-extent files exceed
	// the 32-bit range of a single directory record.
	Size uint64 `json:"size"`
	// Location of the file in the iso. Zero for multi-extent files, whose
	// reader maps offset 0 to the start of the assembled content.
	Location uint32 `json:"location"`
	// UID, userid of the file/directory
	UID *uint32 `json:"uid"`
	// GID, groupid of the file/directory
	GID *uint32 `json:"gid"`
	// Mode, permissions of the file/directory
	Mode os.FileMode
	// CreateTime
	CreateTime time.Time
	// ModTime
	ModTime time.Time
	// RockRidge extended attributes
	HasRockRidge bool `json:"has_rock_ridge"`
	// SymlinkTarget is the Rock Ridge symbolic link target, empty for
	// regular files and directories.
	SymlinkTarget string `json:"symlink_target,omitempty"`
	// Segments lists the extents of a multi-extent file in order. Nil for
	// ordinary single-extent files.
	Segments []ExtentSegment `json:"segments,omitempty"`
	// Original DirectoryRecord
	record *directory.DirectoryRecord
	// A reference to the io.ReaderAt so that we can extract the file contents easily
	reader io.ReaderAt
}

// DirectoryRecord returns the original directory record for the entry
func (fse *FileSystemEntry) DirectoryRecord() *directory.DirectoryRecord {
	return fse.record
}

// ContentReader returns the entry's backing reader. For multi-extent
// files this reader maps offset 0 to the start of the assembled content.
func (fse *FileSystemEntry) ContentReader() io.ReaderAt {
	return fse.reader
}

// IsSymlink reports whether the entry is a Rock Ridge symbolic link.
func (fse *FileSystemEntry) IsSymlink() bool {
	return fse.SymlinkTarget != ""
}

// ReadAt is a wrapper that allows the FileSystemEntry to be used as an io.ReaderAt
func (fse *FileSystemEntry) ReadAt(p []byte, off int64) (n int, err error) {
	return fse.reader.ReadAt(p, off)
}

// Extract the entry to disk
func (fse *FileSystemEntry) ExtractToDisk(outputDir string) error {
	outputPath := filepath.Join(outputDir, fse.FullPath)

	if fse.IsDir {
		// Create directory and all parent directories
		return os.MkdirAll(outputPath, os.FileMode(fse.Mode))
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create parent directories for %s: %w", outputPath, err)
	}

	// Open output file for writing
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", outputPath, err)
	}
	defer outFile.Close()

	// Read file bytes and write to disk
	data, err := fse.GetBytes()
	if err != nil {
		return fmt.Errorf("failed to read file data for %s: %w", fse.FullPath, err)
	}

	if _, err := outFile.Write(data); err != nil {
		return fmt.Errorf("failed to write file %s: %w", outputPath, err)
	}

	// Set correct file permissions
	if err := os.Chmod(outputPath, os.FileMode(fse.Mode)); err != nil {
		return fmt.Errorf("failed to set permissions on %s: %w", outputPath, err)
	}

	// Set timestamps
	if err := os.Chtimes(outputPath, fse.ModTime, fse.ModTime); err != nil {
		return fmt.Errorf("failed to set timestamps on %s: %w", outputPath, err)
	}

	return nil
}

// Get the raw bytes of the file
func (fse *FileSystemEntry) GetBytes() ([]byte, error) {
	if fse.IsDir {
		return nil, fmt.Errorf("cannot get bytes for a directory: %s", fse.FullPath)
	}

	startOffset := int64(fse.Location) * int64(consts.ISO9660_SECTOR_SIZE)
	data := make([]byte, fse.Size)

	_, err := fse.reader.ReadAt(data, startOffset)
	if err != nil {
		return nil, fmt.Errorf("failed to read file data for %s: %w", fse.FullPath, err)
	}

	return data, nil
}

// Compute MD5 hash of the file
func (fse *FileSystemEntry) GetMD5() (string, error) {
	if fse.IsDir {
		return "", fmt.Errorf("cannot compute MD5 for a directory: %s", fse.FullPath)
	}

	data, err := fse.GetBytes()
	if err != nil {
		return "", err
	}

	md5Sum := md5.Sum(data)
	return hex.EncodeToString(md5Sum[:]), nil
}

// Compute SHA-256 hash of the file
func (fse *FileSystemEntry) GetSHA256() (string, error) {
	if fse.IsDir {
		return "", fmt.Errorf("cannot compute SHA-256 for a directory: %s", fse.FullPath)
	}

	data, err := fse.GetBytes()
	if err != nil {
		return "", err
	}

	sha256Sum := sha256.Sum256(data)
	return hex.EncodeToString(sha256Sum[:]), nil
}
