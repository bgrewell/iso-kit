package helpers

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPadString(t *testing.T) {
	// Shorter strings pad with the ISO 9660 filler (space).
	padded := PadString("ABC", 8)
	require.Equal(t, []byte("ABC     "), padded)

	// Exact fit passes through.
	require.Equal(t, []byte("ABCD"), PadString("ABCD", 4))

	// Longer strings truncate to the field.
	require.Equal(t, []byte("AB"), PadString("ABCD", 2))

	// Zero-length fields are empty.
	require.Empty(t, PadString("ABC", 0))
}
