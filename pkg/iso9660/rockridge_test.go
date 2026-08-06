package iso9660

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/bgrewell/iso-kit/pkg/iso9660/tree"
	"github.com/bgrewell/iso-kit/pkg/option"
	"github.com/stretchr/testify/require"
)

func TestRockRidgeNamePreservation(t *testing.T) {
	iso, err := Create("RRNAMES")
	require.NoError(t, err)

	// Names that ISO 9660 identifiers cannot represent.
	longName := strings.Repeat("long-name-segment-", 5) + "tail.dat"
	require.NoError(t, iso.AddFile("MixedCase File.txt", []byte("a")))
	require.NoError(t, iso.AddFile(longName, []byte("b")))
	require.NoError(t, iso.AddFile("dir with spaces/inner.txt", []byte("c")))
	path := saveToTempFile(t, iso)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	reopened, err := Open(f)
	require.NoError(t, err)

	// POSIX names come back exactly via NM entries.
	data, err := reopened.ReadFile("MixedCase File.txt")
	require.NoError(t, err)
	require.Equal(t, []byte("a"), data)

	data, err = reopened.ReadFile(longName)
	require.NoError(t, err)
	require.Equal(t, []byte("b"), data)

	data, err = reopened.ReadFile("dir with spaces/inner.txt")
	require.NoError(t, err)
	require.Equal(t, []byte("c"), data)
}

func TestRockRidgeIdentifiersAreMangled(t *testing.T) {
	iso, err := Create("RRMANGLE")
	require.NoError(t, err)
	require.NoError(t, iso.AddFile("lower case.txt", []byte("x")))
	path := saveToTempFile(t, iso)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	// With Rock Ridge parsing disabled, the raw ISO identifier shows:
	// uppercase, invalid characters replaced.
	reopened, err := Open(f, option.WithRockRidgeEnabled(false))
	require.NoError(t, err)

	files, err := reopened.ListFiles()
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "LOWER_CASE.TXT;1", files[0].Name)
}

func TestRockRidgeModesAndTimes(t *testing.T) {
	iso, err := Create("RRMETA")
	require.NoError(t, err)

	require.NoError(t, iso.AddFile("script.sh", []byte("#!/bin/sh\n")))
	node := isoLookup(t, iso, "script.sh")
	node.SetMode(0o755)
	modTime := time.Date(2020, 6, 15, 12, 30, 45, 0, time.UTC)
	node.SetModTime(modTime)
	node.SetOwnership(1000, 1000)

	path := saveToTempFile(t, iso)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	reopened, err := Open(f)
	require.NoError(t, err)

	files, err := reopened.ListFiles()
	require.NoError(t, err)
	require.Len(t, files, 1)
	entry := files[0]

	require.Equal(t, os.FileMode(0o755), entry.Mode.Perm())
	require.NotNil(t, entry.UID)
	require.Equal(t, uint32(1000), *entry.UID)
	require.NotNil(t, entry.GID)
	require.Equal(t, uint32(1000), *entry.GID)
	require.True(t, entry.ModTime.Equal(modTime), "mod time mismatch: got %v want %v", entry.ModTime, modTime)
}

func TestRockRidgeSymlink(t *testing.T) {
	iso, err := Create("RRLINK")
	require.NoError(t, err)

	require.NoError(t, iso.AddFile("target.txt", []byte("data")))
	_, err = iso.root.AddSymlink("link.txt", "target.txt")
	require.NoError(t, err)
	_, err = iso.root.AddSymlink("abs-link", "/usr/share/doc")
	require.NoError(t, err)
	_, err = iso.root.AddSymlink("rel-link", "../up/../over/./there")
	require.NoError(t, err)
	iso.markDirty()

	path := saveToTempFile(t, iso)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	reopened, err := Open(f)
	require.NoError(t, err)

	for name, want := range map[string]string{
		"link.txt": "target.txt",
		"abs-link": "/usr/share/doc",
		"rel-link": "../up/../over/./there",
	} {
		node := reopened.root.Lookup(name)
		require.NotNil(t, node, "symlink %s not found", name)
		require.True(t, node.IsSymlink(), "%s should be a symlink", name)
		require.Equal(t, want, node.SymlinkTarget(), "target mismatch for %s", name)
	}
}

func TestRockRidgeSurvivesModifyRoundTrip(t *testing.T) {
	iso, err := Create("RRMOD")
	require.NoError(t, err)
	require.NoError(t, iso.AddFile("Original Name.txt", []byte("one")))
	basePath := saveToTempFile(t, iso)

	base, err := os.Open(basePath)
	require.NoError(t, err)
	defer base.Close()

	opened, err := Open(base)
	require.NoError(t, err)
	require.NoError(t, opened.AddFile("Another File.txt", []byte("two")))
	modPath := saveToTempFile(t, opened)

	mod, err := os.Open(modPath)
	require.NoError(t, err)
	defer mod.Close()

	final, err := Open(mod)
	require.NoError(t, err)

	data, err := final.ReadFile("Original Name.txt")
	require.NoError(t, err)
	require.Equal(t, []byte("one"), data)
	data, err = final.ReadFile("Another File.txt")
	require.NoError(t, err)
	require.Equal(t, []byte("two"), data)
}

func TestRockRidgeDisabled(t *testing.T) {
	iso, err := Create("NORR", option.WithCreateRockRidgeEnabled(false))
	require.NoError(t, err)
	require.NoError(t, iso.AddFile("PLAIN.TXT", []byte("plain")))
	path := saveToTempFile(t, iso)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	reopened, err := Open(f)
	require.NoError(t, err)
	require.False(t, reopened.HasRockRidge())

	data, err := reopened.ReadFile("PLAIN.TXT")
	require.NoError(t, err)
	require.Equal(t, []byte("plain"), data)
}

// TestRockRidgeInteropXorriso verifies that xorriso recognizes the Rock
// Ridge extensions and reports the POSIX names and modes.
func TestRockRidgeInteropXorriso(t *testing.T) {
	xorriso, err := exec.LookPath("xorriso")
	if err != nil {
		t.Skip("xorriso not available")
	}

	iso, err := Create("RRINTEROP")
	require.NoError(t, err)
	require.NoError(t, iso.AddFile("Mixed Case Name.txt", []byte("rr interop\n")))
	node := isoLookup(t, iso, "Mixed Case Name.txt")
	node.SetMode(0o640)
	_, err = iso.root.AddSymlink("the-link", "Mixed Case Name.txt")
	require.NoError(t, err)
	iso.markDirty()
	path := saveToTempFile(t, iso)

	out, err := exec.Command(xorriso, "-indev", path, "-find", "/", "-exec", "lsdl").CombinedOutput()
	require.NoError(t, err, "xorriso failed: %s", out)
	text := string(out)

	// Rock Ridge recognized: POSIX name and symlink reported.
	require.Contains(t, text, "Mixed Case Name.txt")
	require.Contains(t, text, "the-link")
	require.Contains(t, text, "-rw-r-----", "expected 0640 mode from PX entry")
	require.Contains(t, text, "->", "expected symlink arrow in listing")
}

// isoLookup returns the tree node at path, failing the test if missing.
func isoLookup(t *testing.T, iso *ISO9660, path string) *tree.Node {
	t.Helper()
	node := iso.root.Lookup(path)
	require.NotNil(t, node, "path not found in tree: %s", path)
	return node
}
