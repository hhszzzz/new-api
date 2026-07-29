package helper

import (
	"errors"
	"io"
)

// BufferedStreamMaxBytes bounds how much upstream SSE a buffered stream
// handler may aggregate for a non-streaming client. Real completions are far
// smaller; the cap only stops a runaway upstream from growing gateway memory
// without limit.
const BufferedStreamMaxBytes = 64 << 20

var ErrBufferedStreamTooLarge = errors.New("upstream stream exceeded the buffered response size limit")

type boundedStreamReader struct {
	reader    io.Reader
	remaining int64
}

// BoundedStreamReader passes through up to BufferedStreamMaxBytes and then
// fails with ErrBufferedStreamTooLarge instead of truncating silently. A
// stream that ends exactly at the limit is still delivered in full.
func BoundedStreamReader(reader io.Reader) io.Reader {
	return &boundedStreamReader{reader: reader, remaining: BufferedStreamMaxBytes + 1}
}

func (b *boundedStreamReader) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, ErrBufferedStreamTooLarge
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.reader.Read(p)
	b.remaining -= int64(n)
	return n, err
}
