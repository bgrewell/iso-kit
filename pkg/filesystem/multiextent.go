package filesystem

import (
	"fmt"
	"io"
)

// ExtentSegment is one piece of a multi-extent file: an extent location in
// 2048-byte sectors and its length in bytes. ISO 9660 Level 3 files
// larger than 4 GiB are recorded as consecutive directory records whose
// extents concatenate to the full content.
type ExtentSegment struct {
	Location uint32
	Size     uint32
}

// NewMultiExtentReader assembles the segments of a multi-extent file into
// a single io.ReaderAt whose offset 0 is the start of the file.
func NewMultiExtentReader(source io.ReaderAt, sectorSize int64, segments []ExtentSegment) *MultiExtentReader {
	total := int64(0)
	for _, seg := range segments {
		total += int64(seg.Size)
	}
	return &MultiExtentReader{
		source:     source,
		sectorSize: sectorSize,
		segments:   segments,
		totalSize:  total,
	}
}

// MultiExtentReader presents the concatenated extents of a multi-extent
// file as one contiguous stream.
type MultiExtentReader struct {
	source     io.ReaderAt
	sectorSize int64
	segments   []ExtentSegment
	totalSize  int64
}

// TotalSize returns the assembled file size in bytes.
func (r *MultiExtentReader) TotalSize() int64 {
	return r.totalSize
}

// ReadAt reads from the assembled content, translating the relative
// offset into the underlying segment extents.
func (r *MultiExtentReader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("negative offset %d", off)
	}
	if off >= r.totalSize {
		return 0, io.EOF
	}

	read := 0
	segStart := int64(0)
	for _, seg := range r.segments {
		segSize := int64(seg.Size)
		if read == len(p) {
			break
		}
		segEnd := segStart + segSize
		if off >= segEnd {
			segStart = segEnd
			continue
		}

		// Offset within this segment where reading starts or continues.
		inSeg := off + int64(read) - segStart
		want := int64(len(p) - read)
		if avail := segSize - inSeg; want > avail {
			want = avail
		}
		srcOffset := int64(seg.Location)*r.sectorSize + inSeg
		n, err := r.source.ReadAt(p[read:read+int(want)], srcOffset)
		read += n
		if err != nil && err != io.EOF {
			return read, err
		}
		if int64(n) < want {
			return read, io.ErrUnexpectedEOF
		}
		segStart = segEnd
	}

	if off+int64(read) >= r.totalSize && read < len(p) {
		return read, io.EOF
	}
	return read, nil
}
