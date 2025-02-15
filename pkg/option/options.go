package option

import "github.com/go-logr/logr"

// ISOType represents the type of ISO image
type ISOType int

const (
	ISO_TYPE_ISO9660 = iota
	ISO_TYPE_UDF
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
	PreferEnhancedVolumes      bool
	StripVersionInfo           bool
	BootFileExtractLocation    string
	ExtractionProgressCallback ExtractionProgressCallback
	Logger                     logr.Logger
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

func WithLogger(logger logr.Logger) OpenOption {
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

func WithPreferEnhancedVolumes(preferEnhancedVolumes bool) OpenOption {
	return func(o *OpenOptions) {
		o.PreferEnhancedVolumes = preferEnhancedVolumes
	}
}

type CreateOptions struct {
	ISOType ISOType
}

type CreateOption func(*CreateOptions)

func WithISOType(isoType ISOType) CreateOption {
	return func(o *CreateOptions) {
		o.ISOType = isoType
	}
}
