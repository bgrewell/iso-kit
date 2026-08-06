package validation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateDCharacters(t *testing.T) {
	require.NoError(t, ValidateDCharacters("ABC_123", false))
	require.Error(t, ValidateDCharacters("lower", false))
	require.Error(t, ValidateDCharacters("WITH SPACE", false))
	require.Error(t, ValidateDCharacters("DOT.TXT", false))
	require.NoError(t, ValidateDCharacters("DOT.TXT;1", true))
}

func TestValidateACharacters(t *testing.T) {
	require.NoError(t, ValidateACharacters("HELLO WORLD 123!", false))
	require.Error(t, ValidateACharacters("lower", false))
	require.Error(t, ValidateACharacters("emoji \U0001F600", false))
}

func TestValidateCCharacters(t *testing.T) {
	require.NoError(t, ValidateCCharacters("Mixed Case Name"))
	require.Error(t, ValidateCCharacters("with/slash"))
	require.Error(t, ValidateCCharacters("with*star"))
	require.Error(t, ValidateCCharacters("ctrl\x01char"))
	require.Error(t, ValidateCCharacters("outside \U0001F600 bmp"))
	require.NoError(t, ValidateA1Characters("Same as C"))
}

func TestValidateFileIdentifierLevels(t *testing.T) {
	// Relaxed level accepts anything.
	require.NoError(t, ValidateFileIdentifier("anything at all!", false, InterchangeLevelRelaxed))

	// Level 1: 8.3 for files, 8 for directories.
	require.NoError(t, ValidateFileIdentifier("FILENAME.EXT", false, InterchangeLevel1))
	require.NoError(t, ValidateFileIdentifier("F.E", false, InterchangeLevel1))
	require.Error(t, ValidateFileIdentifier("TOOLONGNAME.EXT", false, InterchangeLevel1))
	require.Error(t, ValidateFileIdentifier("FILE.LONG", false, InterchangeLevel1))
	require.Error(t, ValidateFileIdentifier("lower.txt", false, InterchangeLevel1))
	require.NoError(t, ValidateFileIdentifier("DIRNAME8", true, InterchangeLevel1))
	require.Error(t, ValidateFileIdentifier("DIRNAME93", true, InterchangeLevel1))

	// Level 2: 31-character budget.
	long28 := strings.Repeat("A", 28)
	require.NoError(t, ValidateFileIdentifier(long28+".EX", false, InterchangeLevel2))
	require.Error(t, ValidateFileIdentifier(long28+".EXTX", false, InterchangeLevel2))
	require.NoError(t, ValidateFileIdentifier(strings.Repeat("D", 31), true, InterchangeLevel2))
	require.Error(t, ValidateFileIdentifier(strings.Repeat("D", 32), true, InterchangeLevel2))

	// Directories may not contain separators at any level.
	require.Error(t, ValidateFileIdentifier("DIR.EXT", true, InterchangeLevel2))

	// Empty identifiers are invalid at strict levels.
	require.Error(t, ValidateFileIdentifier("", false, InterchangeLevel1))
}
