package iso9660

import (
	"os"
	"testing"

	"github.com/bgrewell/iso-kit/pkg/option"
	"github.com/stretchr/testify/require"
)

// TestCreatedImageGetters exercises the descriptor-backed getters on a
// freshly created (never saved) image — historically a nil-panic minefield.
func TestCreatedImageGetters(t *testing.T) {
	iso, err := Create("GETTERS")
	require.NoError(t, err)

	require.Equal(t, "GETTERS", iso.GetVolumeID())
	require.NotEmpty(t, iso.GetDataPreparerID())
	require.Empty(t, iso.GetPublisherID())
	require.Empty(t, iso.GetApplicationID())
	require.Empty(t, iso.GetCopyrightID())
	require.Empty(t, iso.GetAbstractID())
	require.Empty(t, iso.GetBibliographicID())
	require.Empty(t, iso.GetSystemID())
	require.Empty(t, iso.GetVolumeSetID())
	require.False(t, iso.GetCreationDateTime().IsZero())
	require.False(t, iso.GetModificationDateTime().IsZero())
	require.True(t, iso.GetExpirationDateTime().IsZero())
	require.True(t, iso.GetEffectiveDateTime().IsZero())
	require.Zero(t, iso.GetVolumeSize(), "unsaved image has no assigned size")
	require.False(t, iso.HasJoliet())
	require.False(t, iso.HasRockRidge(), "no parsed records yet")
	require.False(t, iso.HasElTorito())
	require.Zero(t, iso.RootDirectoryLocation())

	entries, err := iso.ListBootEntries()
	require.NoError(t, err)
	require.Nil(t, entries)

	require.NotNil(t, iso.GetLogger())
	require.NoError(t, iso.Close())
}

func TestGettersPreferJoliet(t *testing.T) {
	iso, err := Create("PRIMARYID", option.WithJolietEnabled(true))
	require.NoError(t, err)
	require.NoError(t, iso.AddFile("a.txt", []byte("x")))
	path := saveToTempFile(t, iso)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	viaJoliet, err := Open(f, option.WithPreferJoliet(true))
	require.NoError(t, err)
	require.Equal(t, "PRIMARYID", viaJoliet.GetVolumeID())
	require.NotZero(t, viaJoliet.RootDirectoryLocation())
	require.False(t, viaJoliet.GetCreationDateTime().IsZero())
}

func TestGetLayoutAndObjects(t *testing.T) {
	iso, err := Create("LAYOUT")
	require.NoError(t, err)
	require.NoError(t, iso.AddFile("f.txt", []byte("data")))
	path := saveToTempFile(t, iso)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	reopened, err := Open(f)
	require.NoError(t, err)

	layout := reopened.GetLayout()
	require.NotNil(t, layout)
	require.NotEmpty(t, layout.Objects)

	// Layout objects include the system area and the PVD.
	var names []string
	for _, obj := range layout.Objects {
		names = append(names, obj.Name())
	}
	require.Contains(t, names, "System Area")
}

func TestPristinePassthroughSave(t *testing.T) {
	iso, err := Create("PRISTINE2")
	require.NoError(t, err)
	require.NoError(t, iso.AddFile("keep.txt", []byte("original")))
	basePath := saveToTempFile(t, iso)

	base, err := os.Open(basePath)
	require.NoError(t, err)
	defer base.Close()

	opened, err := Open(base)
	require.NoError(t, err)
	require.False(t, opened.isDirty, "opened image starts clean")

	// An untouched image takes the passthrough path (no repack).
	resavePath := saveToTempFile(t, opened)
	require.False(t, opened.isDirty)

	re, err := os.Open(resavePath)
	require.NoError(t, err)
	defer re.Close()
	reopened, err := Open(re)
	require.NoError(t, err)
	data, err := reopened.ReadFile("keep.txt")
	require.NoError(t, err)
	require.Equal(t, []byte("original"), data)
}

func TestReadFileErrors(t *testing.T) {
	iso, err := Create("ERRS")
	require.NoError(t, err)
	require.NoError(t, iso.AddDirectory("adir"))

	_, err = iso.ReadFile("missing.txt")
	require.Error(t, err)

	_, err = iso.ReadFile("adir")
	require.Error(t, err, "reading a directory should fail")

	require.Error(t, iso.RemoveFile("missing.txt"))
	require.Error(t, iso.RemoveFile("adir"), "RemoveFile on a directory should fail")
	require.Error(t, iso.RemoveDirectory("missing"))
	require.NoError(t, iso.AddFile("afile", []byte("x")))
	require.Error(t, iso.RemoveDirectory("afile"), "RemoveDirectory on a file should fail")
	require.NoError(t, iso.RemoveDirectory("adir"))
}

func TestExtractCreatedImage(t *testing.T) {
	// Extract must work on a created-then-reopened image and preserve
	// mode bits through Rock Ridge.
	iso, err := Create("EXTRACT2")
	require.NoError(t, err)
	require.NoError(t, iso.AddFile("bin/tool.sh", []byte("#!/bin/sh\nexit 0\n")))
	node := isoLookup(t, iso, "bin/tool.sh")
	node.SetMode(0o755)
	path := saveToTempFile(t, iso)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	reopened, err := Open(f)
	require.NoError(t, err)

	outDir := t.TempDir()
	require.NoError(t, reopened.Extract(outDir))

	fi, err := os.Stat(outDir + "/bin/tool.sh")
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), fi.Mode().Perm())
}

func TestAddLocalDirectory(t *testing.T) {
	srcDir := t.TempDir()
	require.NoError(t, os.MkdirAll(srcDir+"/nested", 0o755))
	require.NoError(t, os.WriteFile(srcDir+"/top.txt", []byte("top"), 0o600))
	require.NoError(t, os.WriteFile(srcDir+"/nested/deep.txt", []byte("deep"), 0o644))
	require.NoError(t, os.Symlink("top.txt", srcDir+"/link"))

	iso, err := Create("LOCALDIR")
	require.NoError(t, err)
	require.NoError(t, iso.AddLocalDirectory(srcDir, "/imported"))

	data, err := iso.ReadFile("imported/top.txt")
	require.NoError(t, err)
	require.Equal(t, []byte("top"), data)

	data, err = iso.ReadFile("imported/nested/deep.txt")
	require.NoError(t, err)
	require.Equal(t, []byte("deep"), data)

	link := iso.root.Lookup("imported/link")
	require.NotNil(t, link)
	require.True(t, link.IsSymlink())
	require.Equal(t, "top.txt", link.SymlinkTarget())

	// Imported permissions survive.
	top := isoLookup(t, iso, "imported/top.txt")
	require.Equal(t, os.FileMode(0o600), top.Mode().Perm())
}
