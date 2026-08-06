# iso-kit architecture

Internal documentation for contributors: how the library is structured, how
the read and write paths work, and the invariants that keep images valid.
User-facing documentation lives in [USAGE.md](USAGE.md).

- [Package map](#package-map)
- [The read path: Open](#the-read-path-open)
- [The mutable tree](#the-mutable-tree)
- [The write path: Pack and Save](#the-write-path-pack-and-save)
- [Rock Ridge internals](#rock-ridge-internals)
- [Joliet internals](#joliet-internals)
- [El Torito internals](#el-torito-internals)
- [Hybrid boot internals](#hybrid-boot-internals)
- [UDF read path](#udf-read-path)
- [Key invariants](#key-invariants)
- [Testing strategy](#testing-strategy)
- [Adding a feature](#adding-a-feature)

## Package map

```
iso.go                     Format detection, common ISO interface (iso9660 / udf dispatch)
pkg/
  iso9660/                 The ISO 9660 implementation (the bulk of the library)
    iso9660.go             Public API: Open, Create, mutation, Save, Extract
    pack.go                Layout engine: sector assignment, record planning
    names.go               Identifier mangling (ISO + Joliet), collision dedupe
    parser/                Sector-level parsing of an existing image
    descriptor/            Volume descriptors (PVD, SVD, boot record, partition, terminator)
    directory/             Directory records and file flags
    pathtable/             L/M path table records
    tree/                  Mutable in-memory directory tree (source of truth)
    extensions/            SUSP/RRIP (Rock Ridge) entry building and parsing
    boot/                  El Torito boot catalog
    systemarea/            System area, MBR, GPT
    encoding/              Both-byte-order integers, date/time formats, UCS-2
    validation/            Character sets and interchange-level rules
    xattr/                 Extended attribute records (parsing)
    info/                  ImageObject interface for layout inspection
  udf/                     Read-only UDF (ECMA-167) implementation
  filesystem/              FileSystemEntry: flat entry list shared by both formats
  option/                  Open/Create option structs
  consts/, helpers/, logging/, version/
```

Dependency direction: `iso9660.go` and `pack.go` orchestrate; the
subpackages (`descriptor`, `directory`, `tree`, ...) do not import each
other except through the small leaf packages (`encoding`, `consts`,
`info`). The `tree` package depends only on the standard library.

## The read path: Open

`iso9660.Open(reader)` walks an existing image:

```
sectors 0-15    system area           → kept verbatim (may hold MBR/GPT)
sector 16+      volume descriptors    → parser.GetPrimaryVolumeDescriptor etc.
                  boot record         → El Torito catalog (boot.UnmarshalBinary)
                  SVDs                → Joliet detection via escape sequences
                  terminator
path tables      → parsed for layout inspection (not used for traversal)
directory tree   → parser.WalkDirectoryRecords / BuildFileSystemEntries
```

Directory traversal reads each directory extent, decoding records
sector-by-sector (records never cross sector boundaries; a zero length
byte means "skip to the next sector"). For each record:

- **Rock Ridge**: the system use field is parsed into
  `extensions.RockRidgeExtensions`; SUSP `CE` entries chain to
  continuation areas, which the parser follows (bounded at 8 hops).
- **Multi-extent files**: consecutive records with the `MultiExtent` flag
  merge into one entry backed by `filesystem.MultiExtentReader`, which
  presents the concatenated extents as a single `io.ReaderAt`.
- The result is a flat `[]*filesystem.FileSystemEntry` (paths, sizes,
  modes, symlink targets) plus the mutable tree built from it.

Open chooses one hierarchy to traverse: primary (with Rock Ridge names) by
default, or Joliet with `WithPreferJoliet(true)`.

## The mutable tree

`tree.Node` is the source of truth between Open/Create and Save. Every
file node has exactly one content source:

- **Pending**: in-memory `[]byte` (added via `AddFile`), or
- **Reader-backed**: an `io.ReaderAt` plus sector location and size
  (parsed from an existing image).

This split is what makes open→modify→save memory-safe: existing content is
streamed from the source image to its new location at save time and never
fully materialized. `Node.SourceLocation()` exposes the original extent
sector, which the El Torito preservation logic uses to re-match boot images
after relocation.

Mutations (`AddFile`, `RemoveFile`, ...) set a dirty flag on the `ISO9660`
struct and invalidate the flat entry cache; `ListFiles` lazily rebuilds it
from the tree.

## The write path: Pack and Save

`Save` dispatches: a clean opened image re-serializes parsed structures at
their original offsets (passthrough); anything dirty or created runs the
full rebuild — `Pack()` then a sequential write.

`Pack()` assigns every structure a sector. The layout, in order:

```
0-15        system area (verbatim; hybrid MBR/GPT patched in later)
16          PVD
17          boot record            (when El Torito entries exist — spec requires sector 17)
next        SVD                    (when Joliet)
next        terminator
next        primary L path table, M path table
next        Joliet L/M path tables (when Joliet)
next        Rock Ridge continuation region (when RR: ER entry + SU overflow)
next        boot catalog sector    (when El Torito)
next        primary directory extents, breadth-first
next        Joliet directory extents
next        file extents           (shared by both hierarchies)
[+40 LBAs]  backup GPT             (hybrid GPT mode only, appended after the ISO)
```

The ordering trick: **sizes are computed before locations**. Directory
extent sizes depend only on record lengths (identifiers + system-use
sizes), never on the values inside the records, so Pack can size
everything, assign sectors in one pass, and then fill in cross-references
(PVD/SVD totals, path table locations, the root record, CE pointers,
catalog extents).

### Record plans

For every directory record to be written, `buildPlans` produces a
`recordPlan`: the on-disk identifier plus the SUSP entries destined for
its system use field, split between:

- **inline** — what fits in the record (max total record length is 255
  bytes, one length byte), and
- **continuation** — the overflow, placed in the continuation region and
  referenced by a 28-byte `CE` entry reserved in the inline budget.

The root `.` record always leads with `SP` and defers the 237-byte `ER`
entry to the continuation region. Continuation chunks never cross a sector
boundary (a SUSP requirement), which `assignContinuationOffsets` enforces
when laying chunks into the region.

### Writing

`saveRebuild` then writes each region in order. File extents stream through
`io.Copy` from each node's content reader and zero-pad to sector
boundaries, so every allocated sector is written and the image is exactly
`VolumeSpaceSize × 2048` bytes (plus the GPT tail in hybrid GPT mode).
Post-passes patch boot info tables into boot images and hybrid partition
structures into the system area — both need final locations, so they run
last.

## Rock Ridge internals

`pkg/iso9660/extensions` implements SUSP (IEEE P1281) and RRIP (P1282):

- Entry builders produce exact binary layouts: `PX` (36-byte form),
  `PN`, `SL` (component records with root/parent/current flags), `NM`
  (multi-entry continuation for long names), `TF` (7-byte recording
  format), `CL`/`PL`/`RE`, plus SUSP `SP`/`CE`/`ER`/`ST` and the legacy
  `RR` bitmap entry.
- The parser (`ParseInto`) is tolerant: unknown entries are skipped, `ST`
  terminates, both TF forms (7 and 17 byte) are handled, and NM/SL
  continuations accumulate across entries.
- On write, every record gets `RR`+`PX`+`TF`; children add `NM`;
  symlinks add `SL`. Identifier mangling happens in `names.go`
  (uppercase, d-characters, 31-char cap or 8.3 at level 1, deterministic
  `~N` dedupe) with the POSIX name preserved in `NM`.

Write policy: created images write RR by default
(`WithCreateRockRidgeEnabled(false)` opts out); opened images write RR only
if the source had it, keeping plain-ISO round-trips byte-exact.

## Joliet internals

Joliet is a second, parallel directory hierarchy under an SVD whose escape
sequences (`%/@`, `%/C`, `%/E`) mark UCS-2 identifier encoding:

- Identifiers are sanitized (Joliet forbids `*/:;?\` and control chars),
  truncated to 64 UTF-16 units, deduplicated, then **pre-encoded to UCS-2
  big-endian** in the record plans — the directory record marshal writes
  identifier bytes verbatim, so no encoding logic lives there.
- Joliet directory extents and path tables get their own sectors; file
  extents are shared with the primary hierarchy. An `extentRef` resolver
  passed to the record marshal picks the right directory extent per
  hierarchy.

## El Torito internals

The boot record descriptor (sector 17) points at a one-sector boot catalog:
validation entry, initial/default entry, then section headers (0x90/0x91)
grouping section entries by platform.

Two parsing subtleties worth knowing:

- Entry fields live at spec offsets (byte 1 media type, bytes 2-3 load
  segment, byte 4 system type); the platform comes from the validation
  entry or section header, *not* the entry itself.
- A 0x00 boot indicator means both "not bootable" and "end of catalog".
  Only the section header count disambiguates — entries promised by a
  header are consumed before the end-of-catalog check applies.

On write, entries reference tree files by path; Pack resolves them to
packed extents. When modifying an opened bootable image, parsed entries are
re-matched to tree files by source extent location, with a raw sector-copy
fallback for boot images that have no filesystem counterpart (hidden boot
images). The optional boot info table (56 bytes at image offset 8: PVD LBA,
image LBA, length, word-sum checksum from offset 64) is patched after the
image content is written.

## Hybrid boot internals

`systemarea` models the two partition schemes written into the 32 KB
system area:

- **MBR**: 440 bytes boot code, four-entry partition table, 0x55AA
  signature. CHS tuples are synthesized for the 64-head/32-sector geometry
  isohybrid assumes, saturating at the CHS limit.
- **GPT**: primary header (LBA 1) + 128-entry array (LBA 2-33) + backup
  at the end of the device, CRC32s over header and array. GUIDs are
  derived deterministically so image builds are reproducible.

Mode selection matters for interop: partition tools (libfdisk, parted)
ignore a GPT unless the MBR contains a protective 0xEE entry. So MBR-only
mode writes the classic isohybrid layout (bootable whole-image partition +
0xEF ESP entry, Debian-style) while GPT mode writes a protective MBR and
moves the ESP into the GPT (Fedora-style). The backup GPT lives in a
2048-aligned region appended after the ISO data.

## UDF read path

`pkg/udf` is an independent, read-only ECMA-167 implementation:

```
sector 16+   Volume Recognition Sequence  → BEA01 / NSR0x / TEA01 (detection)
sector 256   Anchor Volume Descriptor Pointer
             → main VDS (reserve VDS fallback)
VDS          PVD (identity), Partition Descriptor (block → sector mapping),
             Logical Volume Descriptor (block size, File Set location)
FSD          → root directory ICB
ICBs         File Entry / Extended File Entry; short_ad, long_ad, or
             inline allocation; File Identifier Descriptors per directory
```

Descriptor tags are checksum-verified; names decode from OSTA compressed
unicode (8-bit and UCS-2 forms); symlink ICBs decode 4/14.16 path component
sequences. File content reuses `filesystem.MultiExtentReader` over the
resolved extents. Directory walking is depth-bounded against reference
cycles. All mutation APIs return `ErrWriteUnsupported`.

## Key invariants

Violating any of these produces images other tools reject:

1. **Directory records never cross sector boundaries.** The extent writer
   zero-pads to the next sector when a record would not fit; readers treat
   a zero length byte as "next sector".
2. **Record length ≤ 255 bytes and even.** One length byte; the identifier
   pad byte plus a trailing SU pad byte maintain evenness.
3. **Numeric fields are both-byte-order** (little- then big-endian) unless
   the spec says otherwise (path table locations are single-endian per
   table flavor). `encoding.UnmarshalUint32LSBMSB` rejects mismatched
   halves as corruption.
4. **Path table records are breadth-first**, parents before children,
   numbering starting at 1 — directory numbers are array indices + 1.
5. **The boot record must be at sector 17** when El Torito is present.
6. **SUSP continuation areas fit within one logical block** (offset +
   length ≤ 2048).
7. **Every allocated sector is written.** The rebuild never leaves gaps, so
   image size always equals `VolumeSpaceSize × 2048` (+ GPT tail).
8. **`sizes before locations`** in Pack: nothing size-relevant may depend
   on an assigned location. If you add a structure whose size depends on
   where things land, you have a two-pass problem — look at how CE
   pointers handle it (fixed-size reservation, value filled later).

## Testing strategy

Three layers, all in the standard `go test` suite:

1. **Unit tests** pin binary layouts: marshal/unmarshal round-trips assert
   exact bytes and value recovery, corrupt input is rejected, and semantic
   contracts (CHS saturation, GPT CRCs, load-size saturation) hold.
2. **End-to-end round-trips**: Create→Save→Open→verify and
   Open→modify→Save→Open across every feature combination (Rock Ridge
   names, Joliet hierarchies, boot catalogs, hybrid layouts). For formats
   we cannot author (UDF, multi-extent), tests construct spec-valid images
   byte-by-byte and parse those.
3. **Interop verification**: generated images are checked with independent
   implementations — xorriso (Rock Ridge names/modes, Joliet trees,
   `-report_el_torito`), isoinfo, fdisk and parted (partition tables).
   These tests skip when tools are absent locally; CI installs them so
   they always run there.

The interop layer is the important one: internal round-trips can pass with
symmetrical bugs (the pre-rewrite El Torito offsets did exactly that), and
only a foreign reader catches them.

## Adding a feature

The typical path for a new on-disk structure:

1. Model it in the right subpackage with `Marshal`/`Unmarshal` and a unit
   test pinning the byte layout (write the test from the spec, not from
   the implementation).
2. Parse it in `parser/` (read side) and surface it on the entry/tree.
3. Extend `Pack()`: size it, place it in the layout order above, update
   cross-references. Respect invariant 8.
4. Write it in `saveRebuild` at its assigned location.
5. Add a round-trip test, and an interop assertion if any external tool
   can see the structure.

Deferred work and known gaps are tracked in [ROADMAP.md](ROADMAP.md).
