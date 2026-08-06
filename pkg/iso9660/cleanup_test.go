package iso9660

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bgrewell/iso-kit/pkg/filesystem"
	"github.com/bgrewell/iso-kit/pkg/iso9660/directory"
	"github.com/bgrewell/iso-kit/pkg/iso9660/parser"
	"github.com/bgrewell/iso-kit/pkg/logging"
	"github.com/bgrewell/iso-kit/pkg/option"
	"github.com/stretchr/testify/require"
)

func TestExtractMaterializesSymlinks(t *testing.T) {
	iso, err := Create("SYMEXT")
	require.NoError(t, err)
	require.NoError(t, iso.AddFile("target.txt", []byte("content")))
	_, err = iso.root.AddSymlink("link.txt", "target.txt")
	require.NoError(t, err)
	iso.markDirty()
	path := saveToTempFile(t, iso)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	reopened, err := Open(f)
	require.NoError(t, err)

	outDir := t.TempDir()
	require.NoError(t, reopened.Extract(outDir))

	linkPath := filepath.Join(outDir, "link.txt")
	fi, err := os.Lstat(linkPath)
	require.NoError(t, err)
	require.NotZero(t, fi.Mode()&os.ModeSymlink, "extracted entry should be a symlink")

	target, err := os.Readlink(linkPath)
	require.NoError(t, err)
	require.Equal(t, "target.txt", target)

	// Following the link resolves to the extracted file content.
	data, err := os.ReadFile(linkPath)
	require.NoError(t, err)
	require.Equal(t, []byte("content"), data)
}

func TestInterchangeLevel1Mangling(t *testing.T) {
	iso, err := Create("LEVEL1", option.WithInterchangeLevel(1))
	require.NoError(t, err)
	require.NoError(t, iso.AddFile("a-rather-long-file-name.text", []byte("x")))
	path := saveToTempFile(t, iso)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	// Raw identifier must be 8.3.
	raw, err := Open(f, option.WithRockRidgeEnabled(false))
	require.NoError(t, err)
	files, err := raw.ListFiles()
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "A_RATHER.TEX;1", files[0].Name)
}

func TestInterchangeLevelValidationWithoutRockRidge(t *testing.T) {
	iso, err := Create("STRICT",
		option.WithCreateRockRidgeEnabled(false),
		option.WithInterchangeLevel(1))
	require.NoError(t, err)
	require.NoError(t, iso.AddFile("lowercase-name.txt", []byte("x")))

	// Save must reject the invalid identifier: no Rock Ridge means no
	// automatic mangling.
	f, err := os.CreateTemp(t.TempDir(), "strict-*.iso")
	require.NoError(t, err)
	defer f.Close()
	err = iso.Save(f)
	require.Error(t, err)
	require.Contains(t, err.Error(), "enable Rock Ridge")
}

func TestMultiExtentReader(t *testing.T) {
	// Backing "image": sector 1 holds 2048 'A's, sector 2 holds 100 'B's.
	backing := make([]byte, 3*2048)
	for i := 2048; i < 4096; i++ {
		backing[i] = 'A'
	}
	for i := 4096; i < 4196; i++ {
		backing[i] = 'B'
	}

	r := filesystem.NewMultiExtentReader(bytes.NewReader(backing), 2048, []filesystem.ExtentSegment{
		{Location: 1, Size: 2048},
		{Location: 2, Size: 100},
	})
	require.Equal(t, int64(2148), r.TotalSize())

	// Full read.
	full := make([]byte, 2148)
	n, err := r.ReadAt(full, 0)
	require.NoError(t, err)
	require.Equal(t, 2148, n)
	require.Equal(t, byte('A'), full[0])
	require.Equal(t, byte('A'), full[2047])
	require.Equal(t, byte('B'), full[2048])
	require.Equal(t, byte('B'), full[2147])

	// Read spanning the segment boundary.
	span := make([]byte, 4)
	n, err = r.ReadAt(span, 2046)
	require.NoError(t, err)
	require.Equal(t, 4, n)
	require.Equal(t, []byte{'A', 'A', 'B', 'B'}, span)

	// Read past EOF.
	_, err = r.ReadAt(make([]byte, 1), 2148)
	require.Error(t, err)

	// Short read at the tail.
	tail := make([]byte, 10)
	n, _ = r.ReadAt(tail, 2144)
	require.Equal(t, 4, n)
}

// TestParserMergesMultiExtentRecords crafts a minimal directory extent
// with a file split across two records (the first carrying the
// MultiExtent flag) and verifies the parser assembles one entry.
func TestParserMergesMultiExtentRecords(t *testing.T) {
	const sectorSize = 2048
	image := make([]byte, 4*sectorSize)

	// Sector 1: 2048 bytes of 'A' (first extent).
	for i := sectorSize; i < 2*sectorSize; i++ {
		image[i] = 'A'
	}
	// Sector 2: 100 bytes of 'B' (final extent).
	for i := 2 * sectorSize; i < 2*sectorSize+100; i++ {
		image[i] = 'B'
	}

	// Sector 0: root directory extent with ".", "..", and the two
	// records for BIG.DAT.
	makeRecord := func(identifier string, location, length uint32, dir, multiExtent bool) []byte {
		rec := &directory.DirectoryRecord{
			LocationOfExtent:       location,
			DataLength:             length,
			RecordingDateAndTime:   time.Now(),
			FileFlags:              directory.FileFlags{Directory: dir, MultiExtent: multiExtent},
			VolumeSequenceNumber:   1,
			LengthOfFileIdentifier: uint8(len(identifier)),
			FileIdentifier:         identifier,
		}
		data, err := rec.Marshal()
		require.NoError(t, err)
		return data
	}

	offset := 0
	for _, rec := range [][]byte{
		makeRecord("\x00", 0, sectorSize, true, false),
		makeRecord("\x01", 0, sectorSize, true, false),
		makeRecord("BIG.DAT;1", 1, sectorSize, false, true),
		makeRecord("BIG.DAT;1", 2, 100, false, false),
	} {
		copy(image[offset:], rec)
		offset += len(rec)
	}

	rootRecord := &directory.DirectoryRecord{
		LocationOfExtent:       0,
		DataLength:             sectorSize,
		FileFlags:              directory.FileFlags{Directory: true},
		LengthOfFileIdentifier: 1,
		FileIdentifier:         "\x00",
	}

	p := parser.NewParser(bytes.NewReader(image), &option.OpenOptions{
		RockRidgeEnabled: true,
		Logger:           logging.DefaultLogger(),
	})

	entries, err := p.BuildFileSystemEntries(rootRecord, false)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	entry := entries[0]
	require.Equal(t, uint64(sectorSize+100), entry.Size)
	require.Len(t, entry.Segments, 2)

	data, err := entry.GetBytes()
	require.NoError(t, err)
	require.Len(t, data, sectorSize+100)
	require.Equal(t, byte('A'), data[0])
	require.Equal(t, byte('A'), data[sectorSize-1])
	require.Equal(t, byte('B'), data[sectorSize])
	require.Equal(t, byte('B'), data[sectorSize+99])
}
