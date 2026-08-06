package iso9660

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// saveToTempFile saves the image to a fresh temp file and returns the path.
func saveToTempFile(t *testing.T, iso *ISO9660) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "isokit-*.iso")
	require.NoError(t, err)
	require.NoError(t, iso.Save(f))
	require.NoError(t, f.Close())
	return f.Name()
}

func TestCreateSaveOpenRoundTrip(t *testing.T) {
	iso, err := Create("TESTVOL")
	require.NoError(t, err)

	require.NoError(t, iso.AddFile("hello.txt", []byte("hello world\n")))
	require.NoError(t, iso.AddFile("dir/nested/data.bin", []byte{0xde, 0xad, 0xbe, 0xef}))
	require.NoError(t, iso.AddDirectory("empty"))

	path := saveToTempFile(t, iso)

	// The image size must match the PVD's volume space size exactly.
	fi, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, int64(iso.GetVolumeSize())*2048, fi.Size())

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	reopened, err := Open(f)
	require.NoError(t, err)
	require.Equal(t, "TESTVOL", reopened.GetVolumeID())

	data, err := reopened.ReadFile("hello.txt")
	require.NoError(t, err)
	require.Equal(t, []byte("hello world\n"), data)

	data, err = reopened.ReadFile("dir/nested/data.bin")
	require.NoError(t, err)
	require.Equal(t, []byte{0xde, 0xad, 0xbe, 0xef}, data)

	dirs, err := reopened.ListDirectories()
	require.NoError(t, err)
	var dirPaths []string
	for _, d := range dirs {
		dirPaths = append(dirPaths, d.FullPath)
	}
	require.Contains(t, dirPaths, "/dir")
	require.Contains(t, dirPaths, "/dir/nested")
	require.Contains(t, dirPaths, "/empty")

	files, err := reopened.ListFiles()
	require.NoError(t, err)
	require.Len(t, files, 2)
}

func TestOpenModifySaveRoundTrip(t *testing.T) {
	// Build a base image.
	iso, err := Create("MODVOL")
	require.NoError(t, err)
	require.NoError(t, iso.AddFile("keep.txt", []byte("keep me")))
	require.NoError(t, iso.AddFile("remove.txt", []byte("remove me")))
	basePath := saveToTempFile(t, iso)

	// Open it, modify it, save it to a second file.
	base, err := os.Open(basePath)
	require.NoError(t, err)
	defer base.Close()

	opened, err := Open(base)
	require.NoError(t, err)

	require.NoError(t, opened.AddFile("added.txt", []byte("new content")))
	require.NoError(t, opened.RemoveFile("remove.txt"))
	modPath := saveToTempFile(t, opened)

	// Reopen the modified image and verify the changes.
	mod, err := os.Open(modPath)
	require.NoError(t, err)
	defer mod.Close()

	final, err := Open(mod)
	require.NoError(t, err)

	// The kept file's content survived a relocation: it was copied from
	// the base image's extent into the rebuilt layout.
	data, err := final.ReadFile("keep.txt")
	require.NoError(t, err)
	require.Equal(t, []byte("keep me"), data)

	data, err = final.ReadFile("added.txt")
	require.NoError(t, err)
	require.Equal(t, []byte("new content"), data)

	_, err = final.ReadFile("remove.txt")
	require.Error(t, err)
}

func TestCreateLargeDirectory(t *testing.T) {
	// Enough children that the root directory extent spans multiple
	// sectors, exercising the sector-boundary placement rule.
	iso, err := Create("BIGDIR")
	require.NoError(t, err)

	const fileCount = 200
	for i := 0; i < fileCount; i++ {
		require.NoError(t, iso.AddFile(
			filepath.ToSlash(filepath.Join("files", fmtName(i))),
			[]byte{byte(i)},
		))
	}

	path := saveToTempFile(t, iso)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	reopened, err := Open(f)
	require.NoError(t, err)

	files, err := reopened.ListFiles()
	require.NoError(t, err)
	require.Len(t, files, fileCount)

	// Spot-check content addressing across the extent.
	for _, i := range []int{0, 99, 199} {
		data, err := reopened.ReadFile("files/" + fmtName(i))
		require.NoError(t, err)
		require.Equal(t, []byte{byte(i)}, data)
	}
}

func fmtName(i int) string {
	return "FILE" + string(rune('A'+i/26%26)) + string(rune('A'+i%26)) + itoa(i) + ".DAT"
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

func TestEmptyImage(t *testing.T) {
	iso, err := Create("EMPTY")
	require.NoError(t, err)

	path := saveToTempFile(t, iso)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	reopened, err := Open(f)
	require.NoError(t, err)
	require.Equal(t, "EMPTY", reopened.GetVolumeID())

	files, err := reopened.ListFiles()
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestPristineResaveUnchanged(t *testing.T) {
	iso, err := Create("PRISTINE")
	require.NoError(t, err)
	require.NoError(t, iso.AddFile("a.txt", []byte("alpha")))
	basePath := saveToTempFile(t, iso)

	base, err := os.Open(basePath)
	require.NoError(t, err)
	defer base.Close()

	opened, err := Open(base)
	require.NoError(t, err)

	// Unmodified: the rebuild path should still produce an equivalent,
	// readable image (the pristine passthrough only rewrites parsed
	// structures, so verify via a full reopen either way).
	resavePath := saveToTempFile(t, opened)

	re, err := os.Open(resavePath)
	require.NoError(t, err)
	defer re.Close()

	reopened, err := Open(re)
	require.NoError(t, err)
	data, err := reopened.ReadFile("a.txt")
	require.NoError(t, err)
	require.Equal(t, []byte("alpha"), data)
}

// TestInterop validates the produced image with an external ISO tool
// (isoinfo or xorriso) when one is available, ensuring other tools can
// read what iso-kit writes.
func TestInterop(t *testing.T) {
	iso, err := Create("INTEROP")
	require.NoError(t, err)
	require.NoError(t, iso.AddFile("HELLO.TXT", []byte("interop test\n")))
	require.NoError(t, iso.AddFile("SUB/NESTED.TXT", []byte("nested\n")))
	path := saveToTempFile(t, iso)

	if isoinfo, err := exec.LookPath("isoinfo"); err == nil {
		out, err := exec.Command(isoinfo, "-d", "-i", path).CombinedOutput()
		require.NoError(t, err, "isoinfo -d failed: %s", out)
		require.Contains(t, string(out), "INTEROP")

		out, err = exec.Command(isoinfo, "-f", "-i", path).CombinedOutput()
		require.NoError(t, err, "isoinfo -f failed: %s", out)
		require.Contains(t, string(out), "HELLO.TXT")
		require.Contains(t, string(out), "NESTED.TXT")

		out, err = exec.Command(isoinfo, "-x", "/HELLO.TXT;1", "-i", path).CombinedOutput()
		require.NoError(t, err, "isoinfo -x failed: %s", out)
		require.Equal(t, "interop test\n", string(out))
		return
	}

	if xorriso, err := exec.LookPath("xorriso"); err == nil {
		out, err := exec.Command(xorriso, "-indev", path, "-find", "/", "-exec", "lsdl").CombinedOutput()
		require.NoError(t, err, "xorriso -find failed: %s", out)
		require.Contains(t, string(out), "INTEROP")
		require.Contains(t, string(out), "HELLO.TXT")
		require.Contains(t, string(out), "NESTED.TXT")

		extracted := filepath.Join(t.TempDir(), "extracted.txt")
		out, err = exec.Command(xorriso, "-osirrox", "on", "-indev", path,
			"-extract", "/HELLO.TXT", extracted).CombinedOutput()
		require.NoError(t, err, "xorriso -extract failed: %s", out)
		data, err := os.ReadFile(extracted)
		require.NoError(t, err)
		require.Equal(t, "interop test\n", string(data))
		return
	}

	t.Skip("no external ISO tool (isoinfo/xorriso) available")
}
