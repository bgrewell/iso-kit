package filesystem

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

import "github.com/stretchr/testify/require"

func newTestEntry(t *testing.T, content []byte) *FileSystemEntry {
	t.Helper()
	// Content at sector 2 of a backing image; entry addressed by location.
	backing := make([]byte, 4*2048)
	copy(backing[2*2048:], content)
	return NewFileSystemEntry(
		"file.txt", "/file.txt", false, uint64(len(content)), 2,
		nil, nil, 0o644,
		time.Now(), time.Now(), nil, bytes.NewReader(backing),
	)
}

func TestGetBytes(t *testing.T) {
	content := []byte("some file content")
	entry := newTestEntry(t, content)

	data, err := entry.GetBytes()
	require.NoError(t, err)
	require.Equal(t, content, data)
}

func TestGetBytesDirectoryFails(t *testing.T) {
	dir := NewFileSystemEntry("d", "/d", true, 0, 0, nil, nil, 0o755, time.Now(), time.Now(), nil, nil)
	_, err := dir.GetBytes()
	require.Error(t, err)
	_, err = dir.GetMD5()
	require.Error(t, err)
	_, err = dir.GetSHA256()
	require.Error(t, err)
}

func TestHashes(t *testing.T) {
	content := []byte("hash me")
	entry := newTestEntry(t, content)

	md5sum, err := entry.GetMD5()
	require.NoError(t, err)
	want := md5.Sum(content)
	require.Equal(t, hex.EncodeToString(want[:]), md5sum)

	shasum, err := entry.GetSHA256()
	require.NoError(t, err)
	wantSHA := sha256.Sum256(content)
	require.Equal(t, hex.EncodeToString(wantSHA[:]), shasum)
}

func TestExtractToDisk(t *testing.T) {
	content := []byte("extract me")
	entry := newTestEntry(t, content)

	outDir := t.TempDir()
	require.NoError(t, entry.ExtractToDisk(outDir))

	data, err := os.ReadFile(filepath.Join(outDir, "file.txt"))
	require.NoError(t, err)
	require.Equal(t, content, data)

	fi, err := os.Stat(filepath.Join(outDir, "file.txt"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o644), fi.Mode().Perm())
}

func TestExtractToDiskDirectory(t *testing.T) {
	dir := NewFileSystemEntry("sub", "/nested/sub", true, 0, 0, nil, nil, 0o755, time.Now(), time.Now(), nil, nil)
	outDir := t.TempDir()
	require.NoError(t, dir.ExtractToDisk(outDir))
	fi, err := os.Stat(filepath.Join(outDir, "nested", "sub"))
	require.NoError(t, err)
	require.True(t, fi.IsDir())
}

func TestIsSymlink(t *testing.T) {
	entry := newTestEntry(t, []byte("x"))
	require.False(t, entry.IsSymlink())
	entry.SymlinkTarget = "somewhere"
	require.True(t, entry.IsSymlink())
}

func TestMultiExtentReaderSingleSegment(t *testing.T) {
	backing := make([]byte, 3*2048)
	copy(backing[2048:], "single segment content")

	r := NewMultiExtentReader(bytes.NewReader(backing), 2048, []ExtentSegment{{Location: 1, Size: 22}})
	require.Equal(t, int64(22), r.TotalSize())

	buf := make([]byte, 22)
	n, err := r.ReadAt(buf, 0)
	require.NoError(t, err)
	require.Equal(t, 22, n)
	require.Equal(t, "single segment content", string(buf))

	// Negative offsets error.
	_, err = r.ReadAt(buf, -1)
	require.Error(t, err)
}
