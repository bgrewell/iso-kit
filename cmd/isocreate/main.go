package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/bgrewell/iso-kit/pkg/iso9660"
	"github.com/bgrewell/iso-kit/pkg/iso9660/boot"
	"github.com/bgrewell/iso-kit/pkg/option"
	"github.com/bgrewell/iso-kit/pkg/version"
	"github.com/bgrewell/usage"
)

func fail(u *usage.Usage, err error) {
	u.PrintError(err)
	os.Exit(1)
}

func main() {
	u := usage.NewUsage(
		usage.WithApplicationVersion(version.Version()),
		usage.WithApplicationBranch(version.Branch()),
		usage.WithApplicationBuildDate(version.Date()),
		usage.WithApplicationCommitHash(version.Revision()),
		usage.WithApplicationName("isocreate"),
		usage.WithApplicationDescription("isocreate builds ISO9660 images from a directory tree, with support for Rock Ridge, Joliet, El Torito boot entries, and hybrid (USB-bootable) layouts."),
	)

	help := u.AddBooleanOption("h", "help", false, "Show this help message", "optional", nil)

	volumeID := u.AddStringOption("V", "volid", "ISOIMAGE", "Volume identifier", "", nil)
	preparer := u.AddStringOption("p", "preparer", "", "Data preparer identifier", "", nil)
	output := u.AddStringOption("o", "output", "", "Output ISO file path (required)", "", nil)

	rockRidge := u.AddBooleanOption("rr", "rockridge", true, "Write Rock Ridge (POSIX metadata) extensions", "", nil)
	joliet := u.AddBooleanOption("J", "joliet", false, "Write a Joliet (Windows Unicode names) hierarchy", "", nil)
	level := u.AddIntegerOption("l", "level", 0, "ISO9660 interchange level to enforce (1, 2, or 3; 0 = relaxed)", "", nil)

	biosBoot := u.AddStringOption("b", "bios-boot", "", "Path (inside the image) of the BIOS boot image (El Torito, no emulation, 4-sector load, boot info table)", "", nil)
	efiBoot := u.AddStringOption("e", "efi-boot", "", "Path (inside the image) of the EFI boot image (El Torito EFI platform entry)", "", nil)
	hybrid := u.AddBooleanOption("H", "isohybrid", false, "Write hybrid MBR partition structures for USB boot", "", nil)
	hybridMBR := u.AddStringOption("m", "isohybrid-mbr", "", "File containing MBR boot code for the hybrid layout (e.g. isohdpfx.bin)", "", nil)
	gpt := u.AddBooleanOption("g", "gpt", false, "Add a GPT with an EFI System Partition entry (requires --efi-boot)", "", nil)

	sourceDir := u.AddArgument(1, "source-dir", "Directory tree to build the image from", "")

	if !u.Parse() {
		fail(u, fmt.Errorf("failed to parse arguments"))
	}
	if *help {
		u.PrintUsage()
		os.Exit(0)
	}
	if *sourceDir == "" {
		fail(u, fmt.Errorf("source directory is required"))
	}
	if *output == "" {
		fail(u, fmt.Errorf("output path is required (-o)"))
	}
	if *gpt && *efiBoot == "" {
		fail(u, fmt.Errorf("--gpt requires --efi-boot"))
	}

	opts := []option.CreateOption{
		option.WithCreateRockRidgeEnabled(*rockRidge),
		option.WithJolietEnabled(*joliet),
		option.WithInterchangeLevel(*level),
	}
	if *preparer != "" {
		opts = append(opts, option.WithPreparerID(*preparer))
	}

	iso, err := iso9660.Create(*volumeID, opts...)
	if err != nil {
		fail(u, fmt.Errorf("failed to create image: %w", err))
	}

	if err := iso.AddLocalDirectory(*sourceDir, "/"); err != nil {
		fail(u, fmt.Errorf("failed to import %s: %w", *sourceDir, err))
	}

	if *biosBoot != "" {
		err := iso.AddBootImage(iso9660.BootImageConfig{
			Path:          strings.TrimPrefix(*biosBoot, "/"),
			Platform:      boot.BIOS,
			Emulation:     boot.NoEmulation,
			LoadSize:      4,
			BootInfoTable: true,
		})
		if err != nil {
			fail(u, fmt.Errorf("failed to add BIOS boot image: %w", err))
		}
	}
	if *efiBoot != "" {
		err := iso.AddBootImage(iso9660.BootImageConfig{
			Path:      strings.TrimPrefix(*efiBoot, "/"),
			Platform:  boot.EFI,
			Emulation: boot.NoEmulation,
		})
		if err != nil {
			fail(u, fmt.Errorf("failed to add EFI boot image: %w", err))
		}
	}

	if *hybrid || *gpt || *hybridMBR != "" {
		cfg := iso9660.HybridBootConfig{
			EFIBootImagePath: strings.TrimPrefix(*efiBoot, "/"),
			AddGPT:           *gpt,
		}
		if *hybridMBR != "" {
			code, err := os.ReadFile(*hybridMBR)
			if err != nil {
				fail(u, fmt.Errorf("failed to read MBR boot code: %w", err))
			}
			cfg.MBRBootCode = code
		}
		if err := iso.SetHybridBoot(cfg); err != nil {
			fail(u, fmt.Errorf("failed to configure hybrid boot: %w", err))
		}
	}

	out, err := os.Create(*output)
	if err != nil {
		fail(u, fmt.Errorf("failed to create output file: %w", err))
	}
	defer out.Close()

	if err := iso.Save(out); err != nil {
		fail(u, fmt.Errorf("failed to write image: %w", err))
	}

	fmt.Printf("Wrote %s (volume %q, %d sectors)\n", *output, *volumeID, iso.GetVolumeSize())
}
