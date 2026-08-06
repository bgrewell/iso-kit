# iso-kit usage guide

The complete tour of the library and CLI tools. For a quick start, see the
[README](../README.md); for internals, see [ARCHITECTURE.md](ARCHITECTURE.md).

- [Opening images](#opening-images)
- [Inspecting an image](#inspecting-an-image)
- [Reading and extracting](#reading-and-extracting)
- [Creating images](#creating-images)
- [Adding and removing content](#adding-and-removing-content)
- [Modifying existing images](#modifying-existing-images)
- [Bootable images](#bootable-images)
- [USB-bootable (hybrid) images](#usb-bootable-hybrid-images)
- [Names: Rock Ridge, Joliet, and interchange levels](#names-rock-ridge-joliet-and-interchange-levels)
- [UDF images](#udf-images)
- [CLI reference](#cli-reference)
- [Limitations and gotchas](#limitations-and-gotchas)

## Opening images

The top-level `iso` package detects the format (ISO 9660 or UDF) and returns
a common interface:

```go
import "github.com/bgrewell/iso-kit"

img, err := iso.Open("image.iso")
defer img.Close()
```

When you know the format — or need the full ISO 9660 API (modification,
boot configuration) — use the `iso9660` package directly with any
`io.ReaderAt`:

```go
import (
    "github.com/bgrewell/iso-kit/pkg/iso9660"
    "github.com/bgrewell/iso-kit/pkg/option"
)

f, _ := os.Open("image.iso")
img, err := iso9660.Open(f,
    option.WithRockRidgeEnabled(true),   // default: POSIX names/permissions from Rock Ridge
    option.WithPreferJoliet(false),      // set true to walk the Joliet hierarchy instead
    option.WithElToritoEnabled(true),    // default: parse the boot catalog
)
```

Notes on open options:

- **`WithRockRidgeEnabled(true)`** (default) — names, permissions, uid/gid,
  timestamps, and symlinks come from the Rock Ridge extensions when present.
  Disable it to see raw ISO 9660 identifiers (`FILE.TXT;1`).
- **`WithPreferJoliet(true)`** — walk the Joliet (Windows Unicode) directory
  hierarchy instead of the primary one. Useful for images authored for
  Windows where the primary names are mangled.
- **`WithStripVersionInfo(true)`** (default) — hide the `;1` version suffix
  when neither Rock Ridge nor Joliet supplies a better name.
- **`WithExtractionProgress(callback)`** — receive per-file progress during
  `Extract`.

## Inspecting an image

```go
img.GetVolumeID()          // volume identifier
img.GetDataPreparerID()    // authoring tool
img.GetCreationDateTime()  // volume timestamps
img.GetVolumeSize()        // size in 2048-byte sectors

img.HasRockRidge()         // extensions present?
img.HasJoliet()
img.HasElTorito()

files, _ := img.ListFiles()        // flat list of all files
dirs, _ := img.ListDirectories()   // flat list of all directories
boots, _ := img.ListBootEntries()  // El Torito boot images

layout := img.GetLayout()          // every on-disk structure with offsets
```

Each entry from `ListFiles` carries `Name`, `FullPath`, `Size`, `Mode`,
`ModTime`, `UID`/`GID` (from Rock Ridge), and `SymlinkTarget` for symbolic
links. Entries can produce their content directly:

```go
data, _ := entry.GetBytes()
sum, _ := entry.GetSHA256()
```

## Reading and extracting

```go
// One file, by path. The ;1 version suffix is optional.
data, err := img.ReadFile("boot/grub/grub.cfg")

// The whole image to a directory. Rock Ridge symlinks become real
// symlinks; permissions and timestamps are applied.
err = img.Extract("./extracted")
```

If the image is bootable and El Torito parsing is enabled, `Extract` also
writes the boot images to the `[BOOT]` subdirectory (configurable via
`WithBootFileExtractLocation`).

## Creating images

```go
img, err := iso9660.Create("MYVOLUME",
    option.WithPreparerID("my-tool 1.0"),
    option.WithCreateRockRidgeEnabled(true),  // default
    option.WithJolietEnabled(true),           // opt-in
    option.WithInterchangeLevel(0),           // 0 = relaxed (default)
)
```

- **Rock Ridge** is written by default. Your file names, permissions,
  ownership, timestamps, and symlinks are preserved exactly; the underlying
  ISO 9660 identifiers are generated automatically (see
  [names](#names-rock-ridge-joliet-and-interchange-levels)).
- **Joliet** adds a second directory hierarchy with UCS-2 names for Windows.
  Both hierarchies share the same file data — the cost is a few sectors of
  metadata.
- `Save` writes the complete image to any `io.WriterAt`:

```go
out, _ := os.Create("new.iso")
defer out.Close()
err = img.Save(out)
```

## Adding and removing content

```go
// In-memory content. Parent directories are created automatically.
img.AddFile("docs/notes/today.txt", []byte("content"))

// Empty directory.
img.AddDirectory("var/empty")

// Symbolic link (requires Rock Ridge, the default).
img.AddSymlink("current", "releases/v2")

// Import a local directory tree, preserving permissions, modification
// times, and symlinks.
img.AddLocalDirectory("./payload", "/payload")

// Remove things. RemoveDirectory removes the whole subtree.
img.RemoveFile("obsolete.txt")
img.RemoveDirectory("old-stuff")
```

Attributes of an added file default to mode 0644 (0755 for directories),
root ownership, and the current time. `AddLocalDirectory` carries the source
attributes over instead.

## Modifying existing images

Open, mutate, save — the same APIs as creation:

```go
f, _ := os.Open("base.iso")
img, _ := iso9660.Open(f)

img.AddFile("extra/hello.txt", []byte("added\n"))
img.RemoveFile("unwanted.dat")

out, _ := os.Create("modified.iso")
img.Save(out)   // full rebuild: layout is recalculated
```

What happens on save:

- Content already in the source image is **streamed** to its new location —
  a 4 GB file is never loaded into memory.
- Rock Ridge and Joliet are preserved if the source image had them.
- The El Torito boot catalog is preserved: entries are re-pointed at the
  relocated boot files (or raw-copied when the boot image has no
  corresponding file in the directory tree).
- An **unmodified** opened image takes a fast path that re-serializes parsed
  structures without relayout.

Keep the source reader open until `Save` completes — the rebuild reads file
content from it.

## Bootable images

Boot images are ordinary files in the image that get registered in the
El Torito boot catalog:

```go
import "github.com/bgrewell/iso-kit/pkg/iso9660/boot"

img.AddFile("isolinux/isolinux.bin", isolinuxBin)
img.AddFile("EFI/BOOT/efiboot.img", espImage)

// BIOS entry: no emulation, load 4 virtual sectors, patch the boot
// info table (what isolinux expects).
img.AddBootImage(iso9660.BootImageConfig{
    Path:          "isolinux/isolinux.bin",
    Platform:      boot.BIOS,
    Emulation:     boot.NoEmulation,
    LoadSize:      4,
    BootInfoTable: true,
})

// EFI entry: the firmware reads the whole ESP image.
img.AddBootImage(iso9660.BootImageConfig{
    Path:      "EFI/BOOT/efiboot.img",
    Platform:  boot.EFI,
    Emulation: boot.NoEmulation,
})
```

The first image added becomes the initial/default entry; additional images
become section entries grouped by platform (the standard BIOS + UEFI
multi-boot layout).

`BootInfoTable: true` patches a 56-byte table into the image at offset 8
during save — isolinux requires this (`-boot-info-table` in mkisofs terms).

## USB-bootable (hybrid) images

Optical boot uses El Torito; booting from a USB stick requires partition
tables in the system area. `SetHybridBoot` writes them during save:

```go
// BIOS-only hybrid (classic isohybrid layout):
isohdpfx, _ := os.ReadFile("isohdpfx.bin")  // from the syslinux package
img.SetHybridBoot(iso9660.HybridBootConfig{
    MBRBootCode: isohdpfx,
})

// BIOS + UEFI hybrid with GPT:
img.SetHybridBoot(iso9660.HybridBootConfig{
    MBRBootCode:      isohdpfx,
    EFIBootImagePath: "EFI/BOOT/efiboot.img",
    AddGPT:           true,
})
```

Two layouts are written depending on `AddGPT`:

- **MBR-only**: a bootable whole-image partition (type `0xCD` by default)
  plus a type `0xEF` entry over the ESP image. This is the classic
  isohybrid layout (as used by Debian images).
- **GPT**: a protective MBR plus a GUID partition table carrying the EFI
  System Partition, with a backup GPT at the end of the image (as used by
  Fedora images). Partition tools require the protective MBR to honor a
  GPT, which is why the ESP moves into the GPT in this mode.

The result can be written to a USB drive with `dd` and booted on BIOS or
UEFI machines (given working boot loader images).

## Names: Rock Ridge, Joliet, and interchange levels

ISO 9660 identifiers are limited (uppercase A–Z, 0–9, `_`, one dot). The
extensions carry your real names:

- With **Rock Ridge** (default), the identifier is generated — uppercased,
  invalid characters replaced, truncated to 31 characters, deduplicated
  with a `~N` tail — and the real name travels in the NM entry.
  `lower case.txt` becomes `LOWER_CASE.TXT;1` on disk but reads back as
  `lower case.txt`.
- With **Joliet**, names are stored in UCS-2 with a 64-character limit,
  preserving case and most punctuation.
- With **neither**, names are written as-is. That maximizes byte-exact
  round-trips but can produce non-conforming images; enable an interchange
  level to enforce the rules instead.

`WithInterchangeLevel(n)` selects the strictness applied at save time:

| Level | File identifiers | Directory identifiers |
|---|---|---|
| 0 (default) | anything | anything |
| 1 | 8.3, d-characters | 8 characters |
| 2, 3 | 31 characters | 31 characters |

With Rock Ridge enabled the mangler simply produces conforming identifiers
(8.3 at level 1). Without it, a non-conforming name makes `Save` fail with
an explanatory error rather than writing an invalid image.

## UDF images

UDF (used by video discs, large-file images, and some OS installers) is
supported read-only:

```go
import "github.com/bgrewell/iso-kit/pkg/udf"

f, _ := os.Open("movie.iso")
if udf.IsUDF(f) {
    u, _ := udf.Open(f)
    files, _ := u.ListFiles()
    data, _ := u.ReadFile("VIDEO_TS/VTS_01_0.IFO")
    u.Extract("./out")
}
```

The top-level `iso.Open` dispatches automatically: bridge images carrying
both ISO 9660 and UDF open through the richer ISO 9660 path.

## CLI reference

### isoextract

```
isoextract [options] <iso-path>
  -o,  --output     Output directory (default ./extracted)
  -b,  --boot       Also extract El Torito boot images
  -bd, --bootdir    Directory for boot images (default [BOOT])
  -rr, --rockridge  Use Rock Ridge names/permissions (default true)
  -s,  --strip      Strip ;1 version suffixes (default true)
```

### isocreate

```
isocreate [options] <source-dir>
  -o, --output         Output ISO path (required)
  -V, --volid          Volume identifier (default ISOIMAGE)
  -p, --preparer       Data preparer identifier
  -rr, --rockridge     Write Rock Ridge (default true)
  -J, --joliet         Write a Joliet hierarchy
  -l, --level          Interchange level to enforce (1/2/3, default 0)
  -b, --bios-boot      In-image path of the BIOS boot image
  -e, --efi-boot       In-image path of the EFI boot image
  -H, --isohybrid      Write hybrid MBR partition structures
  -m, --isohybrid-mbr  File with MBR boot code (e.g. isohdpfx.bin)
  -g, --gpt            Add a GPT with an ESP entry (requires --efi-boot)
```

### isoview

```
isoview <iso-path>    Inspect image structure and layout
```

## Limitations and gotchas

- **Files over 4 GiB** read fine (multi-extent assembly) but cannot yet be
  written — `Save` fails with an explicit error rather than truncating.
- **UDF is read-only**; mutation APIs return `ErrWriteUnsupported`.
- **Symlinks require Rock Ridge.** In an image saved without it, symlinks
  are silently absent (there is nowhere to record them).
- **Modifying drops nothing silently**: Joliet, Rock Ridge, and El Torito
  all survive an open→modify→save cycle. Multi-session images and
  ISO 9660:1999 (Enhanced Volume Descriptors) are not supported.
- **Keep the source open.** After `iso9660.Open(f)`, the returned image
  reads file content from `f` lazily — closing it before `Save` or
  `ReadFile` breaks those calls.
- **Booting is bring-your-own-bootloader.** iso-kit writes the catalog and
  partition structures; the boot images themselves (isolinux, GRUB, an ESP
  FAT image) come from your toolchain.
