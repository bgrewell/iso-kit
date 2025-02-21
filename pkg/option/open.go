package option

import (
	"github.com/bgrewell/iso-kit/pkg/logging"
)

type ExtractionProgressCallback func(
	currentFilename string,
	bytesTransferred int64,
	totalBytes int64,
	currentFileNumber int,
	totalFileCount int,
)

type OpenOptions struct {
	ParseOnOpen                bool
	ReadOnly                   bool
	PreloadDir                 bool
	PreferJoliet               bool
	StripVersionInfo           bool
	RockRidgeEnabled           bool
	ElToritoEnabled            bool
	BootFileExtractLocation    string
	ExtractionProgressCallback ExtractionProgressCallback
	Logger                     *logging.Logger
}

type OpenOption func(*OpenOptions)

// WithExtractionProgress sets a progress callback function that will be called with progress updates.
// Parameters:
// - currentFilename: The name of the file currently being processed.
// - bytesTransferred: The number of bytes transferred so far for the current file.
// - totalBytes: The total number of bytes to be transferred for the current file.
// - currentFileNumber: The index of the current file being processed.
// - totalFileCount: The total number of files to be processed.
func WithExtractionProgress(callback ExtractionProgressCallback) OpenOption {
	return func(o *OpenOptions) {
		o.ExtractionProgressCallback = callback
	}
}

func WithBootFileExtractLocation(location string) OpenOption {
	return func(o *OpenOptions) {
		o.BootFileExtractLocation = location
	}
}

func WithLogger(logger *logging.Logger) OpenOption {
	return func(o *OpenOptions) {
		o.Logger = logger
	}
}

func WithParseOnOpen(parseOnOpen bool) OpenOption {
	return func(o *OpenOptions) {
		o.ParseOnOpen = parseOnOpen
	}
}

func WithReadOnly(readOnly bool) OpenOption {
	return func(o *OpenOptions) {
		o.ReadOnly = readOnly
	}
}

func WithPreloadDir(preloadDir bool) OpenOption {
	return func(o *OpenOptions) {
		o.PreloadDir = preloadDir
	}
}

func WithStripVersionInfo(stripVersionInfo bool) OpenOption {
	return func(o *OpenOptions) {
		o.StripVersionInfo = stripVersionInfo
	}
}

func WithPreferJoliet(preferJoliet bool) OpenOption {
	return func(o *OpenOptions) {
		o.PreferJoliet = preferJoliet
	}
}

func WithRockRidgeEnabled(rockRidgeEnabled bool) OpenOption {
	return func(o *OpenOptions) {
		o.RockRidgeEnabled = rockRidgeEnabled
	}
}

func WithElToritoEnabled(elToritoEnabled bool) OpenOption {
	return func(o *OpenOptions) {
		o.ElToritoEnabled = elToritoEnabled
	}
}
