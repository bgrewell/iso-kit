package iso

import (
	"errors"
	"github.com/bgrewell/iso-kit/pkg/consts"
	"github.com/bgrewell/iso-kit/pkg/filesystem"
	"github.com/bgrewell/iso-kit/pkg/iso9660"
	"github.com/bgrewell/iso-kit/pkg/iso9660/info"
	"github.com/bgrewell/iso-kit/pkg/logging"
	"github.com/bgrewell/iso-kit/pkg/option"
	"github.com/bgrewell/iso-kit/pkg/udf"
	"io"
	"os"
	"time"
)

// ISO represents a generic ISO filesystem with read/write capabilities.
type ISO interface {
	GetVolumeID() string
	GetSystemID() string
	GetVolumeSetID() string
	GetPublisherID() string
	GetDataPreparerID() string
	GetApplicationID() string
	GetCopyrightID() string
	GetAbstractID() string
	GetBibliographicID() string
	GetCreationDateTime() time.Time
	GetModificationDateTime() time.Time
	GetExpirationDateTime() time.Time
	GetEffectiveDateTime() time.Time

	GetVolumeSize() uint32
	RootDirectoryLocation() uint32

	ListBootEntries() ([]*filesystem.FileSystemEntry, error)
	ListFiles() ([]*filesystem.FileSystemEntry, error)
	ListDirectories() ([]*filesystem.FileSystemEntry, error)
	ReadFile(path string) ([]byte, error)
	AddFile(path string, data []byte) error
	RemoveFile(path string) error
	CreateDirectories(path string) error
	Extract(path string) error

	HasJoliet() bool
	HasRockRidge() bool
	HasElTorito() bool

	SetLogger(*logging.Logger)
	GetLogger() *logging.Logger

	GetLayout() *info.ISOLayout

	Save(writer io.WriterAt) error
	Close() error
}

func Open(filename string, opts ...option.OpenOption) (ISO, error) {

	// Get file info
	fileInfo, err := os.Stat(filename)
	if err != nil {
		return nil, err
	}

	// Open file
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	// Check if file is large enough to be a valid ISO
	if fileInfo.Size() < 16*consts.ISO9660_SECTOR_SIZE {
		f.Close()
		return nil, errors.New("file is too small to be a valid ISO9660 ISO")
	}

	// Read PVD header at sector 16 (offset 32768)
	var header [6]byte
	if _, err = f.ReadAt(header[:], 16*consts.ISO9660_SECTOR_SIZE); err != nil {
		f.Close()
		return nil, err
	}

	// Detect ISO9660
	if string(header[1:6]) == consts.ISO9660_STD_IDENTIFIER {
		return iso9660.Open(f, opts...)
	}

	// Check if file is large enough to be a valid UDF ISO
	if fileInfo.Size() < 256*consts.UDF_SECTOR_SIZE {
		f.Close()
		return nil, errors.New("file is too small to be a valid ISO9660 or UDF ISO")
	}

	// Read UDF anchor volume descriptor at sector 256 (offset 524288)
	if _, err = f.ReadAt(header[:], 256*consts.UDF_SECTOR_SIZE); err == nil {
		if string(header[1:5]) == consts.UDF_STD_IDENTIFIER {
			return udf.Open(f, opts...)
		}
	}

	return nil, errors.New("unsupported ISO format")
}

func Create(name string, opts ...option.CreateOption) (ISO, error) {
	// Set default option(s)
	options := option.CreateOptions{
		ISOType: option.ISO_TYPE_ISO9660,
	}

	// Apply options
	for _, opt := range opts {
		opt(&options)
	}

	switch options.ISOType {
	case option.ISO_TYPE_ISO9660:
		return iso9660.Create(name, opts...)
	case option.ISO_TYPE_UDF:
		return udf.Create(name, opts...)
	default:
		return nil, errors.New("unsupported ISO type")
	}

}
