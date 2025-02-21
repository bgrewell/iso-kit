package option

import "github.com/bgrewell/iso-kit/pkg/logging"

// ISOType represents the type of ISO image
type ISOType int

const (
	ISO_TYPE_ISO9660 = iota
	ISO_TYPE_UDF
)

type CreateOptions struct {
	ISOType       ISOType
	Preparer      string
	RootDir       string
	JolietEnabled bool
	Logger        *logging.Logger
}

type CreateOption func(*CreateOptions)

func WithISOType(isoType ISOType) CreateOption {
	return func(o *CreateOptions) {
		o.ISOType = isoType
	}
}

func WithPreparerID(preparer string) CreateOption {
	return func(o *CreateOptions) {
		o.Preparer = preparer
	}
}

func WithRootDir(rootDir string) CreateOption {
	return func(o *CreateOptions) {
		o.RootDir = rootDir
	}
}

func WithJolietEnabled(jolietEnabled bool) CreateOption {
	return func(o *CreateOptions) {
		o.JolietEnabled = jolietEnabled
	}
}

// WithEnableLogging is a temp fix for the fact that we have separate options with helper functions in the same package
func WithEnableLogging(logger *logging.Logger) CreateOption {
	return func(o *CreateOptions) {
		o.Logger = logger
	}
}
