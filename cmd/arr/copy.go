package main

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
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

func iteratePaths(paths []string, pathslist *os.File, callback func(i int, path string)) {
	if pathslist != nil {
		pathslist.Seek(0, 0)
		i, r := 0, bufio.NewReader(pathslist)
		for {
			path, err := r.ReadString('\n')
			path = strings.Replace(path, "\n", "", 1)
			if err != nil {
				if path != "" {
					callback(i, path)
				}
				return
			}
			callback(i, path)
			i++
		}
	}
	for i, path := range paths {
		callback(i, path)
	}
}

func rel(basepath, path string) string {
	p, err := filepath.Rel(basepath, path)
	if err != nil {
		panic(err)
	}
	return p
}

func isUnder(dir, path string) bool {
	if dir == "." || dir == "" {
		p := strings.Split(path, "/")
		return len(p) == 1
	}

	dir += "/"
	if !strings.HasPrefix(path, dir) {
		return false
	}
	p := strings.Split(path[len(dir):], "/")
	return len(p) == 1
}

type oneliner struct {
	start time.Time
	lastp string
}

func newoneliner() oneliner {
	return oneliner{start: time.Now()}
}

func (o *oneliner) fill(p string) string {
	if len(p) > 80 {
		p = p[:37] + "..." + p[len(p)-40:]

	}

	if o.lastp == "" || len(p) >= len(o.lastp) {
		o.lastp = p
		return p
	}
	n := len(o.lastp) - len(p)
	o.lastp = p
	return p + strings.Repeat(" ", n)
}

func (o *oneliner) elapsed() string {
	secs := int64(time.Now().Sub(o.start).Seconds())
	hrs := secs / 3600
	mins := (secs - hrs*3600) / 60
	secs = secs - hrs*3600 - mins*60
	return fmt.Sprintf("%02d:%02d:%02d", hrs, mins, secs)
}
