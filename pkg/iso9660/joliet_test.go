package iso9660

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/bgrewell/iso-kit/pkg/option"
	"github.com/stretchr/testify/require"
)

func createJolietImage(t *testing.T) string {
	t.Helper()
	iso, err := Create("JOLIETVOL", option.WithJolietEnabled(true))
	require.NoError(t, err)
	require.NoError(t, iso.AddFile("Mixed Case Document.txt", []byte("joliet content\n")))
	require.NoError(t, iso.AddFile("Ünïcödé Nämé.txt", []byte("unicode\n")))
	require.NoError(t, iso.AddFile("nested/Deep Dir Name/file.bin", []byte{1, 2, 3}))
	return saveToTempFile(t, iso)
}

func TestJolietCreateAndReopenPrimary(t *testing.T) {
	path := createJolietImage(t)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	// Default open reads the primary (Rock Ridge) hierarchy; names come
	// back exactly and the image reports Joliet present.
	reopened, err := Open(f)
	require.NoError(t, err)
	require.True(t, reopened.HasJoliet(), "reopened image should report Joliet")

	data, err := reopened.ReadFile("Mixed Case Document.txt")
	require.NoError(t, err)
	require.Equal(t, []byte("joliet content\n"), data)

	data, err = reopened.ReadFile("nested/Deep Dir Name/file.bin")
	require.NoError(t, err)
	require.Equal(t, []byte{1, 2, 3}, data)
}

func TestJolietCreateAndReopenJolietHierarchy(t *testing.T) {
	path := createJolietImage(t)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	// PreferJoliet walks the SVD hierarchy: UCS-2 names decode back to
	// the original strings, and file content resolves through the shared
	// extents.
	reopened, err := Open(f, option.WithPreferJoliet(true))
	require.NoError(t, err)
	require.Equal(t, "JOLIETVOL", reopened.GetVolumeID())

	data, err := reopened.ReadFile("Mixed Case Document.txt")
	require.NoError(t, err)
	require.Equal(t, []byte("joliet content\n"), data)

	data, err = reopened.ReadFile("Ünïcödé Nämé.txt")
	require.NoError(t, err)
	require.Equal(t, []byte("unicode\n"), data)

	data, err = reopened.ReadFile("nested/Deep Dir Name/file.bin")
	require.NoError(t, err)
	require.Equal(t, []byte{1, 2, 3}, data)
}

func TestJolietLongNameTruncation(t *testing.T) {
	iso, err := Create("JLONG", option.WithJolietEnabled(true))
	require.NoError(t, err)

	// 80 characters: legal in Rock Ridge, truncated to 64 in Joliet.
	longName := strings.Repeat("abcdefgh", 10) + ".txt"
	require.NoError(t, iso.AddFile(longName, []byte("long")))
	path := saveToTempFile(t, iso)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	reopened, err := Open(f, option.WithPreferJoliet(true))
	require.NoError(t, err)

	files, err := reopened.ListFiles()
	require.NoError(t, err)
	require.Len(t, files, 1)
	// 64 UTF-16 units maximum (the version suffix rides on top).
	name := strings.TrimSuffix(files[0].Name, ";1")
	require.LessOrEqual(t, len([]rune(name)), 64)
	require.True(t, strings.HasPrefix(name, "abcdefgh"), "truncated name should preserve the prefix, got %q", name)

	// The Rock Ridge hierarchy still has the full name.
	rrOpen, err := os.Open(path)
	require.NoError(t, err)
	defer rrOpen.Close()
	viaRR, err := Open(rrOpen)
	require.NoError(t, err)
	data, err := viaRR.ReadFile(longName)
	require.NoError(t, err)
	require.Equal(t, []byte("long"), data)
}

func TestJolietPreservedOnModify(t *testing.T) {
	basePath := createJolietImage(t)

	base, err := os.Open(basePath)
	require.NoError(t, err)
	defer base.Close()

	opened, err := Open(base)
	require.NoError(t, err)
	require.NoError(t, opened.AddFile("Added Later.txt", []byte("later")))
	modPath := saveToTempFile(t, opened)

	mod, err := os.Open(modPath)
	require.NoError(t, err)
	defer mod.Close()

	// The rebuilt image keeps its Joliet hierarchy, including the new file.
	final, err := Open(mod, option.WithPreferJoliet(true))
	require.NoError(t, err)

	data, err := final.ReadFile("Added Later.txt")
	require.NoError(t, err)
	require.Equal(t, []byte("later"), data)

	data, err = final.ReadFile("Mixed Case Document.txt")
	require.NoError(t, err)
	require.Equal(t, []byte("joliet content\n"), data)
}

func TestNoJolietByDefault(t *testing.T) {
	iso, err := Create("NOJOLIET")
	require.NoError(t, err)
	require.NoError(t, iso.AddFile("plain.txt", []byte("x")))
	path := saveToTempFile(t, iso)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	reopened, err := Open(f)
	require.NoError(t, err)
	require.False(t, reopened.HasJoliet())
}

// TestJolietInteropXorriso verifies that xorriso sees the Joliet
// hierarchy in a created image.
func TestJolietInteropXorriso(t *testing.T) {
	xorriso, err := exec.LookPath("xorriso")
	if err != nil {
		t.Skip("xorriso not available")
	}

	path := createJolietImage(t)

	// Disable Rock Ridge reading so xorriso falls back to the Joliet
	// tree (Rock Ridge takes priority otherwise).
	out, err := exec.Command(xorriso, "-read_fs", "norock", "-indev", path,
		"-find", "/", "-exec", "lsdl").CombinedOutput()
	require.NoError(t, err, "xorriso failed: %s", out)
	text := string(out)

	require.Contains(t, text, "Mixed Case Document.txt")
	require.Contains(t, text, "Deep Dir Name")
	require.Contains(t, text, "Ünïcödé Nämé.txt")
}
