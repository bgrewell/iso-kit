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

- [ ] Design and implement mutable in-memory directory tree
  - TreeNode with parent-child relationships, children map, pending data
  - Insert, Remove, Lookup, Walk, BuildFromParsed methods
  - Becomes single source of truth for filesystem state
- [ ] Implement `ReadFile(path)`
  - Search tree (or filesystemEntries), delegate to `GetBytes()` for existing files
  - Return pendingFiles data for in-memory files
- [ ] Implement `AddFile(path, data)` and `RemoveFile(path)`
  - Tree-backed operations, create parent dirs as needed, store in pendingFiles
  - Append ISO 9660 ";1" version suffix, maintain tree/flat-list consistency
- [ ] Implement `AddDirectory(sourcePath, targetPath)`
  - `filepath.Walk` with explicit directory records (. and ..) for subdirectories
- [ ] Implement directory extent writer
  - Marshal children into 2048-byte sectors, prepend . and .. entries
  - Zero-pad at sector boundaries (records must not cross boundaries)
- [ ] Implement path table builder from directory tree
  - Breadth-first walk, sequential parent numbers, L-type and M-type output
- [ ] Promote SVD numeric fields from raw byte arrays to typed values
  - VolumeSpaceSize, VolumeSetSize, etc. — match PVD pattern
  - Required for Pack() to update SVD programmatically
- [ ] Implement `Pack()` — the sector layout engine
  - Calculate descriptor area size (system area + PVD + optional boot/SVDs + terminator)
  - Assign sector locations: path tables, directory extents (bottom-up), file data
  - Update cross-references: VolumeSpaceSize, PathTableSize, LocationOfPathTable L/M,
    each LocationOfExtent and DataLength
  - Handle Joliet SVD layout separately
- [ ] Implement `Create()` with proper initialization
  - SystemArea, PVD with root DirectoryRecord, empty tree, terminator
  - Optional Joliet SVD, fix getter nil-safety
- [ ] Rewrite `Save()` for complete ISO output
  - Pristine re-save (with gap padding) and modified/new ISO (Pack → full write)
  - Pending files from memory, existing files from isoReader
- [ ] Fix Joliet directory record marshal (UCS-2 re-encoding)
  - When `dr.Joliet` is true, encode FileIdentifier as UCS-2
- [ ] Add `VolumeDescriptorSet` methods (WriteTo, Validate)
- [ ] Implement `VolumePartitionDescriptor` Marshal/Unmarshal
- [ ] End-to-end Create→AddFile→Save→Open verification test

## P2: Extensions — Rock Ridge, Joliet, El Torito Write Support

Goal: proper extension support with spec-compliant serialization.

- [ ] Implement SUSP framework (SP, CE, ER, ST, ES)
  - Continuation areas for RR data exceeding System Use space
- [ ] Rewrite `MarshalRockRidge` from scratch
  - PX (36 bytes, both-endian), NM/SL (with flags), TF (7/17-byte ISO format),
    CL/PL (both-endian), RR signature entry
- [ ] Fix Rock Ridge unmarshal: add PN, CL, PL, RE, SF + fix TF/SL/NM parsing
- [ ] Integrate Rock Ridge into write pipeline
  - SP+ER in root, PX/NM/TF in every record, CE for overflow
- [ ] Complete Joliet write support
  - Separate UCS-2 directory tree, separate path tables, 64-char filename validation
- [ ] Fix El Torito entry field offsets to match specification
  - Byte 0 = Boot Indicator, byte 1 = Boot Media Type, bytes 2-3 = Load Segment
- [ ] Fix El Torito marshal for multi-boot catalogs with section headers
- [ ] Implement Boot Information Table support (56-byte table at offset 8)
- [ ] Integrate El Torito into Create/Save pipeline
- [ ] Wire character validation into descriptor write path
- [ ] Implement ISO 9660 filename validation (Level 1/2/3)
- [ ] Add multi-extent file assembly for reading (Level 3 / >4GB files)

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
