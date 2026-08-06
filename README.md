# iso-kit

[![CI](https://github.com/bgrewell/iso-kit/actions/workflows/ci.yml/badge.svg)](https://github.com/bgrewell/iso-kit/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/bgrewell/iso-kit/graph/badge.svg?token=D15C46IECF)](https://codecov.io/gh/bgrewell/iso-kit)
[![Go Reference](https://pkg.go.dev/badge/github.com/bgrewell/iso-kit.svg)](https://pkg.go.dev/github.com/bgrewell/iso-kit)

Work with ISO disk images in Go — or straight from the command line.

Open an ISO and pull files out of it. Change what's inside and save it back.
Build a brand-new image from a folder, including ones that boot on real
hardware from a USB stick.

## Command line tools

```bash
go install github.com/bgrewell/iso-kit/cmd/isoextract@latest
go install github.com/bgrewell/iso-kit/cmd/isocreate@latest
go install github.com/bgrewell/iso-kit/cmd/isoview@latest
```

**Extract an ISO:**

```bash
isoextract -o ./extracted ubuntu-24.04.iso
```

**Build an ISO from a folder:**

```bash
isocreate -V "MY_BACKUP" -o backup.iso ./my-files
```

**Build a bootable, USB-writable ISO:**

```bash
isocreate -V "MY_LINUX" -o my-linux.iso \
  --bios-boot isolinux/isolinux.bin \
  --efi-boot EFI/BOOT/efiboot.img \
  --isohybrid-mbr isohdpfx.bin --gpt \
  ./my-linux-root
```

**Look inside an ISO:**

```bash
isoview ubuntu-24.04.iso
```

## Using the library

```bash
go get github.com/bgrewell/iso-kit
```

```go
import "github.com/bgrewell/iso-kit/pkg/iso9660"

// Open an ISO and read a file out of it.
f, _ := os.Open("image.iso")
img, _ := iso9660.Open(f)
data, _ := img.ReadFile("docs/readme.txt")

// Change it and save a new copy.
img.AddFile("extras/new-file.txt", []byte("added!\n"))
img.RemoveFile("obsolete.txt")
out, _ := os.Create("modified.iso")
img.Save(out)
```

```go
// Or build one from scratch.
img, _ := iso9660.Create("MYVOLUME")
img.AddLocalDirectory("./payload", "/")
out, _ := os.Create("new.iso")
img.Save(out)
```

That's the whole core loop: `Open` or `Create`, change things, `Save`.
Long filenames, mixed case, permissions, and symlinks are preserved
automatically (Rock Ridge is on by default). See the
**[usage guide](docs/USAGE.md)** for bootable images, Windows-friendly
naming (Joliet), and everything else.

## What it supports

| | Read | Write |
|---|:---:|:---:|
| ISO 9660 | ✅ | ✅ |
| Rock Ridge — POSIX names, permissions, symlinks | ✅ | ✅ |
| Joliet — Windows Unicode names | ✅ | ✅ |
| El Torito — BIOS + EFI boot | ✅ | ✅ |
| Hybrid MBR/GPT — boots from USB | ✅ | ✅ |
| Files over 4 GiB (multi-extent) | ✅ | — |
| UDF | ✅ | — |

Output is verified against independent tools (xorriso, fdisk, parted) in CI.

## Documentation

- **[Usage guide](docs/USAGE.md)** — the full tour: options, bootable
  images, modifying existing ISOs, CLI reference, limitations
- **[Architecture](docs/ARCHITECTURE.md)** — how the library works inside,
  for contributors
- **[Roadmap](docs/ROADMAP.md)** — what's done and what's planned

> The API is pre-1.0 and may still change between releases.
