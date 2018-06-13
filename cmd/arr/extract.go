package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unsafe"
)

// func makeDirForFile(filename string) {
// 	dir := filepath.Dir(filename)
// 	os.MkdirAll(dir, )
// }

// Extract extracts the archive to the given path
// if the path doesn't exist, it will be created first
func Extract(arpath, destpath string) (int, error) {

	// dirsSort := make([]*EntryInfo, 0, len(a.pathhash)/2)
	// for _, fi := range a.pathhash {
	// 	if !fi.IsDir {
	// 		continue
	// 	}
	// 	dirsSort = append(dirsSort, fi)
	// 	fi.score = 0
	// 	for _, ch := range fi.Path {
	// 		if ch == '/' {
	// 			fi.score++
	// 		}
	// 	}
	// }

	// sort.Slice(dirsSort, func(i, j int) bool { return dirsSort[i].score < dirsSort[j].score })

	// for _, dir := range dirsSort {
	// 	if options.OnBeforeExtractingEntry != nil {
	// 		options.OnBeforeExtractingEntry(dir)
	// 	}
	// 	p := filepath.Join(path, dir.Path)
	// 	if err := os.MkdirAll(p, os.FileMode(dir.Mode)); err != nil {
	// 		return 0, err
	// 	}
	// }

	// for _, fi := range a.pathhash {
	// 	if fi.IsDir {
	// 		continue
	// 	}
	// 	if options.OnBeforeExtractingEntry != nil {
	// 		options.OnBeforeExtractingEntry(fi)
	// 	}
	// 	p := filepath.Join(path, fi.Path)
	// 	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE, os.FileMode(fi.Mode))
	// 	if err != nil {
	// 		return 0, err
	// 	}
	// 	if _, err := a.Stream(f, fi.Path); err != nil {
	// 		return 0, err
	// 	}
	// 	f.Close()
	// }

	// return 0, nil
	ar, err := os.Open(arpath)
	if err != nil {
		return 0, err
	}
	defer ar.Close()

	st, err := ar.Stat()
	if err != nil {
		return 0, err
	}

	fmtPrintln("Extract:", arpath, ", Size:", humansize(st.Size()))
	fmtPrintln("Output :", destpath)

	cursor := &uint64map{}

	p := [metasize]byte{}
	if _, err := ar.Read(p[:]); err != nil {
		return 0, err
	}
	if string(p[:4]) != "zzz0" {
		return 0, fmt.Errorf("invalid header")
	}

	count := binary.BigEndian.Uint32(p[4:8])
	// x.Created = time.Unix(int64(binary.BigEndian.Uint32(p[8:12])), 0)
	if p[12] != *(*byte)(unsafe.Pointer(&one)) {
		return 0, fmt.Errorf("unmatched endianness")
	}

	cursor.data = make([][3]uint64, count)
	if _, err := ar.Read(cursor.bytes()); err != nil {
		return 0, err
	}

	pathbuf := make([]byte, 256)
	for i := uint32(0); i < count; i++ {
		if _, err := ar.Read(p[:2]); err != nil {
			return 0, err
		}

		pathlen := int(binary.BigEndian.Uint16(p[:2]))
		if pathlen > len(pathbuf) {
			pathbuf = make([]byte, pathlen)
		}

		if _, err := ar.Read(pathbuf[:4]); err != nil {
			return 0, err
		}
		mode := os.FileMode(binary.BigEndian.Uint32(pathbuf))

		if _, err := ar.Read(pathbuf[:4]); err != nil {
			return 0, err
		}

		if _, err := ar.Read(pathbuf[:sha256.Size]); err != nil {
			return 0, err
		}

		hash := [sha256.Size]byte{}
		copy(hash[:], pathbuf[:sha256.Size])
		isDir := bytes.Equal(pathbuf[:sha256.Size], []byte(dirguid))

		if _, err := ar.Read(pathbuf[:pathlen]); err != nil {
			return 0, err
		}

		path := string(pathbuf[:pathlen])
		finalpath := filepath.Join(destpath, path)

		if isDir {
			if _, err := os.Stat(finalpath); err == nil {
				if err := os.Chmod(finalpath, mode); err != nil {
					return 0, err
				}
			} else {
				if err := os.MkdirAll(finalpath, mode); err != nil {
					return 0, err
				}
			}
			continue
		}

		w, err := os.OpenFile(finalpath, os.O_CREATE|os.O_WRONLY, mode)
		if err != nil {
			return 0, err
		}

		old, _ := ar.Seek(0, 1)
		start, length, _ := cursor.get(path)
		if _, err := ar.Seek(int64(start), 0); err != nil {
			w.Close()
			return 0, err
		}

		wr, h, err := hashcopyN(w, ar, int64(length))
		if !bytes.Equal(h, hash[:]) {
			w.Close()
			return 0, ErrCorruptedHash
		}
		if err != nil {
			w.Close()
			return 0, err
		}
		if wr != int64(length) {
			w.Close()
			return 0, io.ErrShortWrite
		}
		w.Close()
		ar.Seek(old, 0)
	}

	return int(count), nil
}
