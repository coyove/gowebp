package main

import (
	"bufio"
	"crypto/sha256"
	"hash"
	"io"
	"os"
	"strings"
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

func hashcopyN(dst io.Writer, src io.Reader, n int64) (int64, []byte, error) {
	s := &hashreader{r: src, s: sha256.New()}
	n, err := io.CopyN(dst, s, n)
	return n, s.Sum(), err
}

func hashcopy(dst io.Writer, src io.Reader) (int64, []byte, error) {
	s := &hashreader{r: src, s: sha256.New()}
	n, err := io.Copy(dst, s)
	return n, s.Sum(), err
}

func iteratePaths(paths []string, pathslist *os.File, callback func(i int, path string) error) error {
	if pathslist != nil {
		pathslist.Seek(0, 0)
		i, r := 0, bufio.NewReader(pathslist)
		for {
			path, err := r.ReadString('\n')
			path = strings.Replace(path, "\n", "", 1)
			if err != nil {
				if path != "" {
					if err := callback(i, path); err != nil {
						return err
					}
				}
				return nil
			}
			if err := callback(i, path); err != nil {
				return err
			}
			i++
		}
	}
	for i, path := range paths {
		if err := callback(i, path); err != nil {
			return err
		}
	}
	return nil
}
