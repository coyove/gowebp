package arp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"hash"
	"io"
)

type hashreader struct {
	r io.Reader
	s hash.Hash
}

func (h *hashreader) Read(p []byte) (int, error) {
	n, err := h.r.Read(p)
	if err == nil {
		h.s.Write(p[:n])
	}
	return n, err
}

func (h *hashreader) Sum() []byte {
	return h.s.Sum(nil)
}

func HashCopyN(dst io.Writer, src io.Reader, n int64) (int64, []byte, error) {
	s := &hashreader{r: src, s: sha256.New()}
	n, err := io.CopyN(dst, s, n)
	return n, s.Sum(), err
}

func HashCopy(dst io.Writer, src io.Reader) (int64, []byte, error) {
	s := &hashreader{r: src, s: sha256.New()}
	n, err := io.Copy(dst, s)
	return n, s.Sum(), err
}

type IOWrapperAES struct {
	r io.Reader
	w io.Writer
	s cipher.Stream
}

func (cr *IOWrapperAES) Write(p []byte) (int, error) {
	if cr.s != nil {
		cr.s.XORKeyStream(p[:], p[:])
	}
	return cr.w.Write(p)
}

func (cr *IOWrapperAES) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	if err != nil {
		return n, err
	}
	if cr.s == nil {
		return n, err
	}
	cr.s.XORKeyStream(p[:n], p[:n])
	return n, err
}

func WrapReaderWriter(r io.Reader, w io.Writer, password string) *IOWrapperAES {
	cr := &IOWrapperAES{r: r, w: w}
	if password == "" {
		return cr
	}
	for len(password) < 16 {
		password += password
	}
	blk, _ := aes.NewCipher([]byte(password[:16]))
	cr.s = cipher.NewCTR(blk, []byte("                "))
	return cr
}
