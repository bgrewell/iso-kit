package extensions

import (
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

func TestRockRidgeMarshalUnmarshalRoundTrip(t *testing.T) {
	perms := fs.FileMode(0o754)
	modTime := time.Date(2021, 3, 14, 15, 9, 26, 0, time.UTC)
	rr := &RockRidgeExtensions{
		UID:              ptr(uint32(1000)),
		GID:              ptr(uint32(2000)),
		Permissions:      &perms,
		Major:            ptr(uint32(8)),
		Minor:            ptr(uint32(1)),
		SymlinkTarget:    ptr("../lib/target"),
		AlternateName:    ptr("Long Mixed Case Name.txt"),
		ChildLinkLBA:     ptr(uint32(1234)),
		ParentLinkLBA:    ptr(uint32(5678)),
		IsRelocated:      ptr(true),
		ModificationTime: &modTime,
	}

	data, err := MarshalRockRidge(rr)
	require.NoError(t, err)

	decoded, err := UnmarshalRockRidge(data)
	require.NoError(t, err)

	require.Equal(t, uint32(1000), *decoded.UID)
	require.Equal(t, uint32(2000), *decoded.GID)
	require.Equal(t, perms.Perm(), decoded.Permissions.Perm())
	require.Equal(t, uint32(8), *decoded.Major)
	require.Equal(t, uint32(1), *decoded.Minor)
	require.Equal(t, "../lib/target", *decoded.SymlinkTarget)
	require.Equal(t, "Long Mixed Case Name.txt", *decoded.AlternateName)
	require.Equal(t, uint32(1234), *decoded.ChildLinkLBA)
	require.Equal(t, uint32(5678), *decoded.ParentLinkLBA)
	require.True(t, *decoded.IsRelocated)
	require.True(t, decoded.ModificationTime.Equal(modTime), "mod time: got %v", decoded.ModificationTime)
}

func TestSymlinkTargetRoundTrips(t *testing.T) {
	for _, target := range []string{
		"simple",
		"dir/nested/leaf",
		"/absolute/path",
		"../parent/relative",
		"./current/style",
		"/",
		"../../up/twice",
	} {
		rr := &RockRidgeExtensions{SymlinkTarget: &target}
		data, err := MarshalRockRidge(rr)
		require.NoError(t, err)
		decoded, err := UnmarshalRockRidge(data)
		require.NoError(t, err)
		require.NotNil(t, decoded.SymlinkTarget, "target %q lost", target)
		require.Equal(t, target, *decoded.SymlinkTarget)
	}
}

func TestLongNameSplitsAcrossNMEntries(t *testing.T) {
	// Longer than one NM entry's payload capacity: must split with the
	// continue flag and reassemble on parse.
	longName := strings.Repeat("abcdefghij", 30) // 300 bytes
	nm := BuildNM(longName)
	require.Greater(t, len(nm), 1, "expected multiple NM entries")
	require.NotZero(t, nm[0][4]&NM_CONTINUE, "first entry should carry the continue flag")
	require.Zero(t, nm[len(nm)-1][4]&NM_CONTINUE, "final entry should not continue")

	var data []byte
	for _, e := range nm {
		data = append(data, e...)
	}
	decoded, err := UnmarshalRockRidge(data)
	require.NoError(t, err)
	require.Equal(t, longName, *decoded.AlternateName)
}

func TestTFLongFormParsing(t *testing.T) {
	// Long-form TF uses 17-byte timestamps (flag bit 7).
	modTime := time.Date(2019, 7, 20, 20, 17, 40, 0, time.UTC)
	stamp := [17]byte{}
	copy(stamp[:], "2019072020174000")
	// Digits: YYYYMMDDhhmmsscc then offset 0.
	copy(stamp[:], []byte("2019072020174000"))
	stamp[16] = 0

	entry := []byte{'T', 'F', byte(4 + 1 + 17), 1, TF_MODIFY | TF_LONG_FORM}
	entry = append(entry, stamp[:]...)

	decoded, err := UnmarshalRockRidge(entry)
	require.NoError(t, err)
	require.NotNil(t, decoded.ModificationTime)
	require.True(t, decoded.ModificationTime.Equal(modTime), "got %v want %v", decoded.ModificationTime, modTime)
}

func TestPosixModeParseFileModeInverse(t *testing.T) {
	cases := []fs.FileMode{
		0o644,
		0o755,
		0o400,
		fs.ModeDir | 0o755,
		fs.ModeSymlink | 0o777,
		fs.ModeSetuid | 0o755,
		fs.ModeSetgid | 0o750,
		fs.ModeSticky | 0o777,
	}
	for _, mode := range cases {
		bits := PosixMode(mode)
		back := parseFileMode(bits)
		require.Equal(t, mode.Perm(), back.Perm(), "perm mismatch for %v", mode)
		require.Equal(t, mode&fs.ModeDir, back&fs.ModeDir, "dir bit for %v", mode)
		require.Equal(t, mode&fs.ModeSymlink, back&fs.ModeSymlink, "symlink bit for %v", mode)
		require.Equal(t, mode&fs.ModeSetuid, back&fs.ModeSetuid, "setuid for %v", mode)
		require.Equal(t, mode&fs.ModeSetgid, back&fs.ModeSetgid, "setgid for %v", mode)
		require.Equal(t, mode&fs.ModeSticky, back&fs.ModeSticky, "sticky for %v", mode)
	}
}

func TestSUSPEntryShapes(t *testing.T) {
	sp := BuildSP(0)
	require.Len(t, sp, 7)
	require.Equal(t, "SP", string(sp[:2]))
	require.Equal(t, byte(0xBE), sp[4])
	require.Equal(t, byte(0xEF), sp[5])

	ce := BuildCE(100, 256, 237)
	require.Len(t, ce, 28)
	require.Equal(t, "CE", string(ce[:2]))
	require.Equal(t, byte(28), ce[2])

	er := BuildER()
	require.Equal(t, "ER", string(er[:2]))
	require.Equal(t, len(er), int(er[2]))
	require.Contains(t, string(er), RRIP_ER_ID)

	st := BuildST()
	require.Equal(t, []byte{'S', 'T', 4, 1}, st)

	rr := BuildRR(0x89)
	require.Equal(t, []byte{'R', 'R', 5, 1, 0x89}, rr)

	px := BuildPX(0o644, 1, 0, 0)
	require.Len(t, px, 36)
	require.Equal(t, int(px[2]), len(px))
}

func TestUnmarshalIgnoresUnknownAndStopsAtST(t *testing.T) {
	name := "x"
	nm := BuildNM(name)[0]

	// Unknown entry, then NM, then ST, then garbage that must not be read.
	data := []byte{'Z', 'Z', 6, 1, 0, 0}
	data = append(data, nm...)
	data = append(data, BuildST()...)
	data = append(data, 'N', 'M', 200, 1) // truncated garbage after ST

	decoded, err := UnmarshalRockRidge(data)
	require.NoError(t, err)
	require.Equal(t, name, *decoded.AlternateName)
}

func TestUnmarshalTooShort(t *testing.T) {
	_, err := UnmarshalRockRidge([]byte{1, 2})
	require.Error(t, err)
}

func TestCEParsing(t *testing.T) {
	decoded, err := UnmarshalRockRidge(BuildCE(50, 512, 300))
	require.NoError(t, err)
	require.NotNil(t, decoded.Continuation)
	require.Equal(t, uint32(50), decoded.Continuation.Block)
	require.Equal(t, uint32(512), decoded.Continuation.Offset)
	require.Equal(t, uint32(300), decoded.Continuation.Length)
}

func TestHasRockRidge(t *testing.T) {
	require.False(t, (&RockRidgeExtensions{}).HasRockRidge())
	require.True(t, (&RockRidgeExtensions{UID: ptr(uint32(0))}).HasRockRidge())
	require.True(t, (&RockRidgeExtensions{SymlinkTarget: ptr("x")}).HasRockRidge())
}
