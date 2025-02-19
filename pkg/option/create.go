package option

// ISOType represents the type of ISO image
type ISOType int

const (
	ISO_TYPE_ISO9660 = iota
	ISO_TYPE_UDF
)

type CreateOptions struct {
	ISOType  ISOType
	Preparer string
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
