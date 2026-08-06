# iso-kit Roadmap

Status as of 2026-08-05, based on a full review of the codebase.

## Current State

The **read path is functional**: `Open()` correctly parses system areas, all volume descriptor
types, El Torito boot catalogs, path tables (LE/BE), and directory trees with Rock Ridge and
Joliet support. `Extract()` works. Marshal/Unmarshal round-trips exist for PVDs, SVDs, boot
records, terminators, directory records, and path table records. ~9,600 lines across 43 files.

The **write path is almost entirely non-functional**. `Create()` is commented out (returns a
zero-value struct that nil-panics). `ReadFile()`, `AddFile()`, `RemoveFile()` all panic.
`Save()` only re-serializes unmodified parsed ISOs at original byte offsets. No `Pack()` /
layout engine, no mutable directory tree, no path table builder, no directory extent writer.

Rock Ridge marshal is self-acknowledged as broken. El Torito marshal has incorrect field
offsets. UDF is 37 panic stubs. 68 methods panic with "implement me". 144 TODO/FIXME markers.
Test suite fails on main.

## PR #4 (Copilot Agent) — Close Without Merging

Cherry-pick only the option struct additions (`WithCreateRockRidgeEnabled`,
`WithCreateElToritoEnabled`) and the go.mod change. Adopt the `pendingFiles` map concept
and API shapes, but rewrite all implementations from scratch. See `docs/pr4-analysis.md`
for the full breakdown.

---

## P0: Foundation — Bug Fixes & Read-Path Stability

Goal: green test suite, read path that handles real-world ISOs without panicking.
Every subsequent phase depends on these fixes.

- [ ] Fix `DateTime` unmarshal for zero-filled (0x00) fields → return `time.Time{}`
  - `pkg/iso9660/encoding/encoding.go` — `UnmarshalDateTime` only recognizes ASCII '0' (0x30)
  - Unblocks the failing `TestPrimaryVolumeDescriptorBody_PreservesTrailingSpaces`
- [ ] Fix SVD `LocationOfPathTableL()` — returns M-table location instead of L-table
  - `pkg/iso9660/descriptor/supplementary.go:29` — one-line fix
- [ ] Fix SVD `HasJoliet()` — unconditionally returns true
  - `pkg/iso9660/descriptor/supplementary.go:94` — delegate to `IsJoliet()`
- [ ] Fix terminator `Unmarshal` offset — never increments past reserved bytes
  - `pkg/iso9660/descriptor/terminator.go:78` — add `offset += TERMINATOR_RESERVED_SIZE`
- [ ] Fix `ListBootEntries()` nil dereference on non-bootable ISOs
  - `pkg/iso9660/iso9660.go:430` — add `elTorito` nil check
- [ ] Add nil-safety to all getter methods for `Create()` path
  - `pkg/iso9660/iso9660.go:292-398` — 15+ methods deref `openOptions` without nil check
- [ ] Fix `Extract()` file handle leak — `defer Close()` inside for loop
  - `pkg/iso9660/iso9660.go:536` and `pkg/iso9660/boot/eltorito.go:464`
- [ ] Fix `StripVersionInfo` — `TrimRight(";1")` → `TrimSuffix(";1")`
  - `pkg/iso9660/iso9660.go:528`
- [ ] Fix `Save()` sort overflow on large offsets — `int()` cast → `cmp.Compare`
  - `pkg/iso9660/iso9660.go:661`
- [ ] Replace panic stubs in `boot.go` and `partition.go` with zero-value returns
  - `pkg/iso9660/descriptor/boot.go`, `partition.go` — 30+ panic stubs
- [ ] Relax `UnmarshalFileFlags` reserved bit validation — warn instead of error
  - `pkg/iso9660/directory/flags.go`
- [ ] Relax `DirectoryRecord` padding byte validation — warn instead of error
  - `pkg/iso9660/directory/record.go`
- [ ] Fix `ReadDirectoryRecords` sectorBoundary tracking after zero-padding jump
  - `pkg/iso9660/parser/parser.go:498-504`
- [ ] Fix `SystemArea.ObjectSize` not set during `Open()`
  - `pkg/iso9660/iso9660.go:49-55`
- [ ] Remove unreachable code in `Save()` flagged by `go vet`
  - `pkg/iso9660/iso9660.go:812`
- [ ] Add Open→Save round-trip integration test
  - Regression gate for all write-path work

## P1: Core Write Path — Mutable Tree, Layout Engine, ISO Creation

Goal: create and modify ISOs. Largest and most architecturally significant phase.

- [x] Design and implement mutable in-memory directory tree (`pkg/iso9660/tree`)
  - Node with parent-child relationships, children map, pending data
  - AddFile, AddDirectory, AddExistingFile, Remove, Lookup, Walk, Directories
  - Built from parsed entries during Open; single source of truth for Save
- [x] Implement `ReadFile(path)`
  - Tree lookup; pending data from memory, existing content streamed from the
    backing image
- [x] Implement `AddFile(path, data)` and `RemoveFile(path)`
  - Tree-backed, create parent dirs as needed; ";1" version suffix appended at
    marshal time; flat entry list rebuilt lazily from the tree after mutation
  - Also: `AddDirectory(path)`, `RemoveDirectory(path)`
- [x] Implement `AddLocalDirectory(sourcePath, targetPath)`
  - `filepath.Walk` import of a local directory tree, preserving mode/mtime
- [x] Implement directory extent writer (`pack.go: marshalDirectoryExtent`)
  - Marshals children into 2048-byte sectors with . and .. entries
  - Zero-pads at sector boundaries (records never cross boundaries)
- [x] Implement path table builder from directory tree (`pack.go: buildPathTables`)
  - Breadth-first walk, sequential parent numbers, L-type and M-type output
- [x] Promote SVD numeric fields from raw byte arrays to typed values
  - VolumeSpaceSize (uint32), VolumeSetSize/VolumeSequenceNumber/
    LogicalBlockSize (uint16), both-byte-order encoding matching the PVD
- [x] Implement `Pack()` — the sector layout engine
  - Assigns sector locations: PVD, terminator, path tables, directory extents
    (breadth-first), file data
  - Updates cross-references: VolumeSpaceSize, PathTableSize, LocationOfPathTable
    L/M, root record location/length, each extent's LocationOfExtent/DataLength
  - Joliet SVD layout deferred to P2 (SVDs are dropped from rebuilt output with a
    logged warning)
- [x] Implement `Create()` with proper initialization
  - SystemArea, PVD with root DirectoryRecord, empty tree, terminator
  - Joliet SVD deferred to P2 (warned when requested)
- [x] Rewrite `Save()` for complete ISO output
  - Pristine passthrough for unmodified opened images; Pack → full rebuild for
    created/modified images
  - Pending files written from memory, existing files streamed from isoReader
    (relocation-safe)
- [x] Fix Joliet directory record marshal (UCS-2 re-encoding)
  - Identifiers are pre-encoded to UCS-2 in the layout engine's Joliet
    record plans; the record marshal writes them byte-exact
- [ ] Add `VolumeDescriptorSet` methods (WriteTo, Validate)
- [ ] Implement `VolumePartitionDescriptor` Marshal/Unmarshal
- [x] End-to-end Create→AddFile→Save→Open verification test
  - Round-trip tests plus Open→modify→Save→Open, multi-sector directories,
    empty images, and external-tool interop (isoinfo/xorriso)

## P2: Extensions — Rock Ridge, Joliet, El Torito Write Support

Goal: proper extension support with spec-compliant serialization.

- [x] Implement SUSP framework (`pkg/iso9660/extensions/susp.go`)
  - SP, CE, ER, ST, RR entry builders; continuation areas for RR data
    exceeding System Use space (per-record chunks, never crossing a sector)
- [x] Rewrite `MarshalRockRidge` from scratch
  - PX (36 bytes, both-endian), NM/SL (with flags and multi-entry
    continuation), TF (7-byte recording format), CL/PL (both-endian), RR
    signature entry with correct flag bits
- [x] Fix Rock Ridge unmarshal: add PN, CL, PL, RE, SF, CE + fix TF/SL/NM parsing
  - TF honors flag-bit ordering and long/short form; SL decodes component
    records (root/current/parent); NM continuation appends; parser follows
    CE continuation chains (bounded against cycles)
- [x] Integrate Rock Ridge into write pipeline
  - SP in root ".", ER in continuation area, RR/PX/TF on every record
    (including "." and ".."), NM per child, SL for symlinks, CE for overflow
  - ISO identifiers mangled (uppercase, d-chars, 31-char cap, deterministic
    ~N dedupe) with POSIX names preserved via NM
  - Tree carries uid/gid and symlink nodes; AddLocalDirectory imports
    symlinks; created images write RR by default
    (`WithCreateRockRidgeEnabled(false)` to disable); opened images keep RR
    iff the source had it
  - Verified with xorriso: POSIX names, PX modes, and symlinks recognized
- [x] Complete Joliet write support
  - SVD written at sector 17; separate UCS-2 directory extents and path
    tables sharing file data with the primary hierarchy; 64-unit name
    sanitization/truncation with ~N dedupe
  - Created images opt in with `WithJolietEnabled(true)`; opened images
    preserve Joliet when the source had it; verified with xorriso
    (`-read_fs norock` shows the Joliet tree with Unicode names)
- [ ] Fix El Torito entry field offsets to match specification
  - Byte 0 = Boot Indicator, byte 1 = Boot Media Type, bytes 2-3 = Load Segment
- [ ] Fix El Torito marshal for multi-boot catalogs with section headers
- [ ] Implement Boot Information Table support (56-byte table at offset 8)
- [ ] Integrate El Torito into Create/Save pipeline
- [ ] Wire character validation into descriptor write path
- [ ] Implement ISO 9660 filename validation (Level 1/2/3)
  - Partially covered by the Rock Ridge identifier mangler; strict
    level-selectable validation still open
- [ ] Add multi-extent file assembly for reading (Level 3 / >4GB files)
- [ ] Extract() should materialize Rock Ridge symlinks as symlinks
  (currently written as empty files on extraction)

## P3: Advanced — UDF, Hybrid ISO, Production Readiness

Goal: UDF support, USB-bootable hybrid ISOs, production CLI, comprehensive testing.

- [ ] Fix UDF detection (scan Volume Recognition Sequence at sectors 16+)
- [ ] UDF Volume Recognition Sequence and AVDP parsing
- [ ] UDF volume descriptor and partition parsing (ECMA-167)
- [ ] UDF file system traversal (FSD → ICB → File Entry → FID)
- [ ] System area MBR partition table parsing and generation
- [ ] GPT support for UEFI hybrid ISOs
- [ ] Isohybrid post-processing (MBR + GPT after ISO build)
- [ ] Rebuild `isocreate` CLI with proper argument parsing
- [ ] Comprehensive unit tests (directory, parser, pathtable, extensions, eltorito)
- [ ] CI pipeline (GitHub Actions) + README update
- [ ] Split `VolumeDescriptor` interface into base + filesystem-aware sub-interface

## Architectural Concerns

These should be addressed early — they affect multiple work items:

1. **No mutable directory tree** — flat `filesystemEntries` slice has no parent-child
   relationships. Single biggest structural gap. Must be addressed before write-path work.
2. **No parsed vs modified state separation** — need dirty-tracking. `Save()` must handle
   both files backed by `io.ReaderAt` and files added in-memory.
3. **VolumeDescriptor interface too broad** — forces boot records and terminators to
   implement 20+ meaningless methods. Split into base + filesystem-aware sub-interface.
4. **Separate OpenOptions / CreateOptions** — all getters only check `openOptions`.
   Create()'d ISOs nil-panic on every getter. Unify or add fallback logic.
5. **El Torito field offsets wrong** — parse and marshal share same incorrect layout.
   Round-trips accidentally work but interop with other tools will corrupt boot catalogs.
6. **Rock Ridge marshal acknowledged broken** — every entry type has encoding errors.
   Complete rewrite needed.
7. **SVD raw byte arrays** — VolumeSpaceSize etc. not typed. Pack() can't update them.
