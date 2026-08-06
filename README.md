# iso-kit

[![CI](https://github.com/bgrewell/iso-kit/actions/workflows/ci.yml/badge.svg)](https://github.com/bgrewell/iso-kit/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/bgrewell/iso-kit/graph/badge.svg?token=D15C46IECF)](https://codecov.io/gh/bgrewell/iso-kit)

**iso-kit** is a Go library for working with ISO 9660 disk images: open existing
images, modify them, or build new ones from scratch — with Rock Ridge, Joliet,
El Torito boot, hybrid (USB-bootable) layouts, and read-only UDF support.

> **Notice:** The API is pre-1.0 and may still change between releases.

## Features

- **Read**: parse ISO 9660 images including Rock Ridge (POSIX metadata,
  symlinks), Joliet (Unicode names), El Torito boot catalogs, multi-extent
  (>4 GiB) files, and path tables. Extract full trees to disk, symlinks
  included.
- **Create**: build images from scratch or from a local directory tree.
  Rock Ridge is written by default; Joliet is opt-in; identifiers can be
  enforced at ISO 9660 interchange levels 1–3.
- **Modify**: open an image, add/remove files and directories, and save —
  existing file content is streamed and relocated, never fully loaded into
  memory.
- **Boot**: register BIOS and EFI El Torito boot entries (with isolinux
  boot-info-table patching), and write hybrid MBR/GPT partition structures so
  images boot from USB media.
- **UDF**: read-only support for ECMA-167 / UDF images (listing, reading,
  extraction).

## Library usage

```go
import (
    "os"

    "github.com/bgrewell/iso-kit/pkg/iso9660"
    "github.com/bgrewell/iso-kit/pkg/option"
)

// Create an image from scratch.
img, _ := iso9660.Create("MYVOLUME", option.WithJolietEnabled(true))
img.AddFile("docs/readme.txt", []byte("hello\n"))
img.AddLocalDirectory("./payload", "/payload")
out, _ := os.Create("out.iso")
img.Save(out)

// Open, modify, save.
f, _ := os.Open("existing.iso")
img2, _ := iso9660.Open(f)
data, _ := img2.ReadFile("some/file.txt")
img2.AddFile("added.txt", data)
img2.RemoveFile("obsolete.txt")
out2, _ := os.Create("modified.iso")
img2.Save(out2)
```

Bootable, USB-writable images:

```go
img.AddBootImage(iso9660.BootImageConfig{
    Path: "isolinux/isolinux.bin", Platform: boot.BIOS,
    Emulation: boot.NoEmulation, LoadSize: 4, BootInfoTable: true,
})
img.AddBootImage(iso9660.BootImageConfig{
    Path: "EFI/BOOT/efiboot.img", Platform: boot.EFI, Emulation: boot.NoEmulation,
})
img.SetHybridBoot(iso9660.HybridBootConfig{
    MBRBootCode:      isohdpfx, // e.g. syslinux isohdpfx.bin
    EFIBootImagePath: "EFI/BOOT/efiboot.img",
    AddGPT:           true,
})
```

## Command line tools

```bash
go install github.com/bgrewell/iso-kit/cmd/isoextract@latest
go install github.com/bgrewell/iso-kit/cmd/isocreate@latest
go install github.com/bgrewell/iso-kit/cmd/isoview@latest
```

- **isoextract** — extract files and boot images from an ISO
- **isocreate** — build an ISO from a directory tree
  (`isocreate -V MYVOL -o out.iso ./srcdir`, plus `--bios-boot`,
  `--efi-boot`, `--isohybrid`, `--gpt`, `--joliet`, `--level`)
- **isoview** — inspect image structure and layout

*Note: ensure `$GOBIN` is in your `$PATH`
(`export PATH=$PATH:$(go env GOPATH)/bin`).*

## Format support

| Capability | Read | Write |
|---|---|---|
| ISO 9660 | ✅ | ✅ |
| Rock Ridge (SUSP/RRIP: SP, CE, ER, PX, NM, SL, TF, PN, CL, PL, RE) | ✅ | ✅ |
| Joliet (UCS-2 hierarchy) | ✅ | ✅ |
| El Torito (multi-boot, BIOS + EFI sections, boot info table) | ✅ | ✅ |
| Hybrid MBR / GPT (USB boot) | ✅ | ✅ |
| Multi-extent files (>4 GiB) | ✅ | ❌ |
| UDF (ECMA-167) | ✅ | ❌ |

Interoperability is verified in CI against xorriso (Rock Ridge, Joliet,
El Torito reporting) and util-linux fdisk / parted (hybrid partition tables).

## Roadmap

See [docs/ROADMAP.md](docs/ROADMAP.md) for the detailed phase plan and
remaining work (UDF write support, multi-extent write, and more).
