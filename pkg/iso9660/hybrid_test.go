package iso9660

import (
	"os"
	"os/exec"
	"testing"

	"github.com/bgrewell/iso-kit/pkg/iso9660/boot"
	"github.com/bgrewell/iso-kit/pkg/iso9660/systemarea"
	"github.com/stretchr/testify/require"
)

func createHybridImage(t *testing.T, addGPT bool) string {
	t.Helper()
	iso, err := Create("HYBRID")
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

	bootCode := make([]byte, 432)
	copy(bootCode, "FAKE-ISOHDPFX-BOOTCODE")
	require.NoError(t, iso.SetHybridBoot(HybridBootConfig{
		MBRBootCode:      bootCode,
		EFIBootImagePath: "EFI/BOOT/efiboot.img",
		AddGPT:           addGPT,
	}))
	return saveToTempFile(t, iso)
}

func TestHybridBootMBR(t *testing.T) {
	path := createHybridImage(t, false)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	mbr := systemarea.ParseMBR(data)
	require.NotNil(t, mbr, "image should carry an MBR boot signature")

	// Boot code survived.
	require.Equal(t, []byte("FAKE-ISOHDPFX-BOOTCODE"), mbr.BootCode[:22])

	// Partition 1: bootable, covers the whole image.
	p1 := mbr.Partitions[0]
	require.Equal(t, byte(0x80), p1.Status)
	require.Equal(t, byte(systemarea.MBR_TYPE_ISO9660), p1.Type)
	require.Equal(t, uint32(0), p1.StartLBA)
	require.Equal(t, uint32(len(data)/512), p1.SizeLBA)

	// Partition 2: EFI system partition over the ESP image extent.
	p2 := mbr.Partitions[1]
	require.Equal(t, byte(systemarea.MBR_TYPE_EFI_SYSTEM), p2.Type)
	require.NotZero(t, p2.StartLBA)
	require.Equal(t, uint32((1440*1024)/512), p2.SizeLBA)

	// The partition points at the actual ESP content.
	espStart := int64(p2.StartLBA) * 512
	require.Equal(t, fakeBootImage(1440 * 1024)[:64], data[espStart:espStart+64])

	// The ISO filesystem is still intact.
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	reopened, err := Open(f)
	require.NoError(t, err)
	require.True(t, reopened.HasElTorito())
}

func TestHybridBootGPT(t *testing.T) {
	path := createHybridImage(t, true)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Zero(t, len(data)%512, "image should be LBA aligned")

	// Primary GPT header at LBA 1.
	require.Equal(t, []byte("EFI PART"), data[512:520])

	// Backup GPT header occupies the final LBA.
	backup := data[len(data)-512:]
	require.Equal(t, []byte("EFI PART"), backup[:8])

	// The image still opens as an ISO 9660 filesystem.
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	reopened, err := Open(f)
	require.NoError(t, err)
	data2, err := reopened.ReadFile("isolinux/isolinux.bin")
	require.NoError(t, err)
	require.Equal(t, fakeBootImage(24*1024), data2)
}

func TestHybridBootValidation(t *testing.T) {
	iso, err := Create("HVAL")
	require.NoError(t, err)
	require.NoError(t, iso.AddFile("esp.img", []byte("x")))

	// Boot code too large.
	err = iso.SetHybridBoot(HybridBootConfig{MBRBootCode: make([]byte, 441)})
	require.Error(t, err)

	// GPT without an ESP image.
	err = iso.SetHybridBoot(HybridBootConfig{AddGPT: true})
	require.Error(t, err)

	// ESP path that does not exist.
	err = iso.SetHybridBoot(HybridBootConfig{EFIBootImagePath: "missing.img"})
	require.Error(t, err)
}

// TestHybridInteropFdisk verifies that standard partition tools read the
// hybrid partition tables.
func TestHybridInteropFdisk(t *testing.T) {
	fdisk, err := exec.LookPath("fdisk")
	if err != nil {
		t.Skip("fdisk not available")
	}

	// MBR-only image.
	path := createHybridImage(t, false)
	out, err := exec.Command(fdisk, "-l", path).CombinedOutput()
	require.NoError(t, err, "fdisk failed: %s", out)
	text := string(out)
	require.Contains(t, text, "EFI (FAT-12/16/32)", "fdisk should identify the 0xEF partition")

	// GPT image: fdisk should recognize the GPT and the ESP entry.
	gptPath := createHybridImage(t, true)
	out, err = exec.Command(fdisk, "-l", gptPath).CombinedOutput()
	require.NoError(t, err, "fdisk failed: %s", out)
	text = string(out)
	require.Contains(t, text, "gpt", "fdisk should detect the GPT disklabel")
	require.Contains(t, text, "EFI System", "fdisk should list the EFI System Partition")
}
