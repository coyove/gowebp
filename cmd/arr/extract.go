package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"
)

// Extract extracts the archive to the given path
// if the path doesn't exist, it will be created first
func Extract(arpath, destpath string) {
	const tf = "2006-01-02 15:04:05"
	var badFiles = 0

	a, err := OpenArchive(arpath, true)
	fmtFatalErr(err)
	defer a.Close()

	fmtPrintln("Source:", arpath, ", size:", humansize(a.Info.Size()))
	if flags.action == 'l' { 
		if flags.checksum {
			fmtPrintf("\nMode       Modtime                 Offset       Size  H\n\n")
		} else {
			fmtPrintf("\nMode       Modtime                 Offset       Size\n\n")
		}
	} else {
		fmtPrintln("Output :", destpath)
	}

	pathbuf := make([]byte, 256)
	count := uint32(len(a.cursor.data))

	p := [metasize]byte{}
	for i := uint32(0); i < count; i++ {
		_, err = a.fd.Read(p[:2])
		fmtFatalErr(err)

		pathlen := int(binary.BigEndian.Uint16(p[:2]))
		if pathlen > len(pathbuf) {
			pathbuf = make([]byte, pathlen)
		}

		_, err = a.fd.Read(pathbuf[:4])
		fmtFatalErr(err)
		mode := os.FileMode(binary.BigEndian.Uint32(pathbuf))

		_, err = a.fd.Read(pathbuf[:4])
		modtime := time.Unix(int64(binary.BigEndian.Uint32(pathbuf)), 0)
		fmtFatalErr(err)

		_, err = a.fd.Read(pathbuf[:sha256.Size])
		fmtFatalErr(err)

		hash := [sha256.Size]byte{}
		copy(hash[:], pathbuf[:sha256.Size])
		isDir := bytes.Equal(pathbuf[:sha256.Size], []byte(dirguid))

		_, err = a.fd.Read(pathbuf[:pathlen])
		fmtFatalErr(err)

		path := string(pathbuf[:pathlen])
		finalpath := filepath.Join(destpath, path)

		// list the content and continue reading
		if flags.action == 'l' {
			start, length, _ := a.cursor.get(path)
			flag, dirstr := " · ", "-"
			if isDir {
				dirstr = "d"
				start, length = 0, 0
			}

			if flags.checksum {
				if !isDir {
					old, err := a.fd.Seek(0, 1)
					fmtFatalErr(err)
					if _, err = a.Stream(ioutil.Discard, path); err == ErrCorruptedHash {
						badFiles++
						flag = " X "  
					}
					_, err = a.fd.Seek(old,0)
					fmtFatalErr(err)
				}

				fmtPrintf("%s%s %s %10x %10d %s %s\n", dirstr, uint16mod(uint16(mode)), modtime.Format(tf), start, length, flag, path)
			} else {
				fmtPrintf("%s%s %s %10x %10d %s\n", dirstr, uint16mod(uint16(mode)), modtime.Format(tf), start, length, path)
			}
			continue
		}

		if isDir {
			if _, err := os.Stat(finalpath); err == nil {
				if err := os.Chmod(finalpath, mode); err != nil {
					fmtMaybeErr(err)
				}
			} else {
				if err := os.MkdirAll(finalpath, mode); err != nil {
					fmtMaybeErr(err)
				}
			}
			continue
		}

		w, err := os.OpenFile(finalpath, os.O_CREATE|os.O_WRONLY, mode)
		if err != nil {
			fmtMaybeErr(err)
			continue
		}

		old, err := a.fd.Seek(0, 1)
		fmtFatalErr(err)

		start, length, _ := a.cursor.get(path)
		_, err = a.fd.Seek(int64(start), 0)
		fmtFatalErr(err)

		var wr int64
		var h []byte

		if flags.checksum {
			wr, h, err = hashcopyN(w, a.fd, int64(length))
			if !bytes.Equal(h, hash[:]) {
				w.Close()
				fmtMaybeErr(ErrCorruptedHash)
				continue
			}
		} else {
			wr, err = io.CopyN(w, a.fd, int64(length))
		}

		if err != nil {
			w.Close()
			fmtMaybeErr(err)
			continue
		}
		if wr != int64(length) {
			w.Close()
			fmtMaybeErr(io.ErrShortWrite)
			continue
		}

		w.Close()
		_, err = a.fd.Seek(old, 0)
		fmtFatalErr(err)
	}

	if flags.action == 'l' {
		fmtPrintln("\nTotal entries:", a.TotalEntries(), ", created at:", a.Created.Format(tf))
		if badFiles > 0 {
			fmtPrintferr("Found %d corrupted files!\n", badFiles)
		}
	}
}
