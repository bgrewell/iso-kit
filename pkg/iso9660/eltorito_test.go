package iso9660

import (
	"encoding/binary"
	"os"
	"os/exec"
	"testing"

	"github.com/bgrewell/iso-kit/pkg/iso9660/boot"
	"github.com/stretchr/testify/require"
)

// fakeBootImage returns content resembling a boot loader image: non-zero
// bytes with recognizable structure, larger than one virtual sector.
func fakeBootImage(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i * 7)
	}
	return data
}

func createBootableImage(t *testing.T) string {
	t.Helper()
	iso, err := Create("BOOTVOL")
	require.NoError(t, err)
	require.NoError(t, iso.AddFile("isolinux/isolinux.bin", fakeBootImage(24*1024)))
	require.NoError(t, iso.AddFile("EFI/BOOT/efiboot.img", fakeBootImage(1440*1024)))
	require.NoError(t, iso.AddBootImage(BootImageConfig{
		Path:      "isolinux/isolinux.bin",
		Platform:  boot.BIOS,
		Emulation: boot.NoEmulation,
		LoadSize:  4,
	}))
	require.NoError(t, iso.AddBootImage(BootImageConfig{
		Path:      "EFI/BOOT/efiboot.img",
		Platform:  boot.EFI,
		Emulation: boot.NoEmulation,
	}))
	return saveToTempFile(t, iso)
}

func TestElToritoCreateAndReopen(t *testing.T) {
	path := createBootableImage(t)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	reopened, err := Open(f)
	require.NoError(t, err)
	require.True(t, reopened.HasElTorito(), "reopened image should report El Torito")

	bootEntries, err := reopened.ListBootEntries()
	require.NoError(t, err)
	require.Len(t, bootEntries, 2)

	// The BIOS entry loads 4 virtual sectors; the EFI entry the full image.
	require.Equal(t, uint64(4*512), bootEntries[0].Size)
	require.Equal(t, uint64(((1440*1024)+511)/512*512), bootEntries[1].Size)

	// The boot file content is still readable through the filesystem and
	// matches what went in (the catalog references the same extent).
	data, err := reopened.ReadFile("isolinux/isolinux.bin")
	require.NoError(t, err)
	require.Equal(t, fakeBootImage(24*1024), data)
}

func TestElToritoCatalogRoundTrip(t *testing.T) {
	path := createBootableImage(t)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	reopened, err := Open(f)
	require.NoError(t, err)
	require.NotNil(t, reopened.elTorito)
	require.Len(t, reopened.elTorito.Entries, 2)

	first, second := reopened.elTorito.Entries[0], reopened.elTorito.Entries[1]
	require.Equal(t, boot.BIOS, first.Platform)
	require.Equal(t, boot.NoEmulation, first.Emulation)
	require.True(t, first.Bootable)
	require.Equal(t, uint16(4), first.SectorCount())

	require.Equal(t, boot.EFI, second.Platform)
	require.True(t, second.Bootable)
}

func TestElToritoPreservedOnModify(t *testing.T) {
	basePath := createBootableImage(t)

	base, err := os.Open(basePath)
	require.NoError(t, err)
	defer base.Close()

	opened, err := Open(base)
	require.NoError(t, err)
	require.NoError(t, opened.AddFile("extra.txt", []byte("added after boot setup")))
	modPath := saveToTempFile(t, opened)

	mod, err := os.Open(modPath)
	require.NoError(t, err)
	defer mod.Close()

	final, err := Open(mod)
	require.NoError(t, err)
	require.True(t, final.HasElTorito(), "boot catalog should survive modification")

	entries, err := final.ListBootEntries()
	require.NoError(t, err)
	require.Len(t, entries, 2)

	// The boot image extent was relocated with its file; verify the
	// catalog points at content identical to the original image.
	require.NotNil(t, final.elTorito)
	loc := final.elTorito.Entries[0].Location()
	require.NotZero(t, loc)
	imgData := make([]byte, 4*512)
	_, err = mod.ReadAt(imgData, int64(loc)*2048)
	require.NoError(t, err)
	require.Equal(t, fakeBootImage(24 * 1024)[:4*512], imgData)
}

func TestElToritoBootInfoTable(t *testing.T) {
	iso, err := Create("BINFOTBL")
	require.NoError(t, err)
	img := fakeBootImage(24 * 1024)
	require.NoError(t, iso.AddFile("isolinux.bin", img))
	require.NoError(t, iso.AddBootImage(BootImageConfig{
		Path:          "isolinux.bin",
		Platform:      boot.BIOS,
		Emulation:     boot.NoEmulation,
		LoadSize:      4,
		BootInfoTable: true,
	}))
	path := saveToTempFile(t, iso)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	reopened, err := Open(f)
	require.NoError(t, err)
	require.NotNil(t, reopened.elTorito)
	loc := reopened.elTorito.Entries[0].Location()

	written := make([]byte, len(img))
	_, err = f.ReadAt(written, int64(loc)*2048)
	require.NoError(t, err)

	// Table at offset 8: PVD LBA, image LBA, image length, checksum.
	require.Equal(t, uint32(16), binary.LittleEndian.Uint32(written[8:12]))
	require.Equal(t, loc, binary.LittleEndian.Uint32(written[12:16]))
	require.Equal(t, uint32(len(img)), binary.LittleEndian.Uint32(written[16:20]))

	var wantSum uint32
	for i := 64; i < len(written); i += 4 {
		wantSum += binary.LittleEndian.Uint32(written[i : i+4])
	}
	require.Equal(t, wantSum, binary.LittleEndian.Uint32(written[20:24]))

	// Bytes outside the table are untouched.
	require.Equal(t, img[:8], written[:8])
	require.Equal(t, img[64:], written[64:])
}

// TestElToritoInteropXorriso verifies that xorriso recognizes the boot
// catalog, platforms, and load sizes of a created image.
func TestElToritoInteropXorriso(t *testing.T) {
	xorriso, err := exec.LookPath("xorriso")
	if err != nil {
		t.Skip("xorriso not available")
	}

	path := createBootableImage(t)

	out, err := exec.Command(xorriso, "-indev", path, "-report_el_torito", "plain").CombinedOutput()
	require.NoError(t, err, "xorriso failed: %s", out)
	text := string(out)

	require.Contains(t, text, "El Torito catalog")
	require.Contains(t, text, "BIOS")
	require.Contains(t, text, "UEFI")
}
