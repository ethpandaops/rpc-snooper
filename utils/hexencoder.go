package utils

import (
	"encoding/hex"
	"io"
)

// bufferSize is the number of hexadecimal characters to buffer in encoder and decoder.
const bufferSize = 1024
const chunkSize = bufferSize / 2

type hexencoder struct {
	r   io.ReadCloser
	err error
	in  []byte // input buffer (raw form)
	out []byte // output buffer (encoded form)
}

// NewHexEncoder returns an io.Reader that encodes bytes from r into hexadecimal characters.
func NewHexEncoder(r io.ReadCloser) io.ReadCloser {
	return &hexencoder{
		r:   r,
		in:  make([]byte, chunkSize),
		out: make([]byte, bufferSize),
	}
}

func (e *hexencoder) Read(p []byte) (n int, err error) {
	pLen := len(p)
	inLen := pLen / 2

	if inLen > chunkSize {
		inLen = chunkSize
	}

	var readLen int

	readLen, e.err = e.r.Read(e.in[:inLen])
	if readLen > 0 && e.err == nil {
		readLen = hex.Encode(p, e.in[:inLen])
	}

	return readLen, e.err
}

func (e *hexencoder) Close() error {
	return e.r.Close()
}
