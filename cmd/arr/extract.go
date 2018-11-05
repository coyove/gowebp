package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/coyove/gowebp/arp"
)

// Extract extracts the archive to the given path
// if the path doesn't exist, it will be created first
func Extract(arpath, destpath string, password string) {
	const tf = "2006-01-02 15:04:05"
	var badFiles = 0
	var o = newoneliner()

	a, err := arp.OpenArchive(arpath, password, true)
	fmtFatalErr(err)
	defer a.Close()

	fmtPrintln("Source:", arpath, "(", humansize(a.Info.Size()), "/", len(a.Cursor.Data), "files )")
	if flags.action == 'l' {
		if flags.checksum {
			fmtPrintf("\nMode       Modtime                 Offset       Size  H\n\n")
		} else {
			fmtPrintf("\nMode       Modtime                 Offset       Size\n\n")
		}
	} else {
		fmtPrintln("Output:", destpath)
	}

	pathbuf := make([]byte, 256)
	count := uint32(len(a.Cursor.Data))

	p := [arp.MetaSize]byte{}
	for i := uint32(0); i < count; i++ {
		_, err = a.Fd.Read(p[:2])
		fmtFatalErr(err)

		pathlen := int(binary.BigEndian.Uint16(p[:2]))
		if pathlen > len(pathbuf) {
			pathbuf = make([]byte, pathlen)
		}
		if pathlen > 4096 {
			fmtFatalErr(fmt.Errorf("unexpected long path: %d", pathlen))
		}

		_, err = a.Fd.Read(pathbuf[:4])
		fmtFatalErr(err)
		mode := os.FileMode(binary.BigEndian.Uint32(pathbuf))

		_, err = a.Fd.Read(pathbuf[:4])
		modtime := time.Unix(int64(binary.BigEndian.Uint32(pathbuf)), 0)
		fmtFatalErr(err)

		_, err = a.Fd.Read(pathbuf[:sha256.Size])
		fmtFatalErr(err)

		hash := [sha256.Size]byte{}
		copy(hash[:], pathbuf[:sha256.Size])
		isDir := bytes.Equal(pathbuf[:sha256.Size], []byte(arp.DirGUID))

		_, err = a.Fd.Read(pathbuf[:pathlen])
		fmtFatalErr(err)

		path := string(a.DecodeBytes(pathbuf[:pathlen]))
		finalpath := filepath.Join(destpath, path)

		// list the content and continue reading
		if flags.action == 'l' {
			start, length, _ := a.Cursor.Get(path)
			flag := " . "
			if isDir {
				start, length = 0, 0
			}

			modestr := uint32mod(uint32(mode))
			if flags.checksum {
				if !isDir {
					old, err := a.Fd.Seek(0, 1)
					fmtFatalErr(err)
					if _, err = a.Stream(ioutil.Discard, path); err == arp.ErrCorruptedHash {
						badFiles++
						flag = " X "
					}
					_, err = a.Fd.Seek(old, 0)
					fmtFatalErr(err)
				}

				fmtPrintf("%s %s %10x %10d %s %s\n", modestr, modtime.Format(tf), start, length, flag, shortenPath(path))
			} else {
				fmtPrintf("%s %s %10x %10d %s\n", modestr, modtime.Format(tf), start, length, shortenPath(path))
			}
			continue
		}

		fmtPrintf("\r[%s] [%02d%%] ", o.elapsed(), (i * 100 / count))

		// do the extraction
		if isDir {
			fmtPrintf("[   Fdir   ] %s", o.fill(path))

			if _, err := os.Stat(finalpath); err == nil {
				if err := os.Chmod(finalpath, mode); err != nil {
					fmtMaybeErr(finalpath, err)
				}
			} else {
				if err := os.MkdirAll(finalpath, mode); err != nil {
					fmtMaybeErr(finalpath, err)
				}
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(finalpath), mode); err != nil {
			fmtMaybeErr(finalpath, err)
			continue
		}

		w, err := os.OpenFile(finalpath, os.O_CREATE|os.O_WRONLY, mode)
		if err != nil {
			fmtMaybeErr(finalpath, err)
			continue
		}

		old, err := a.Fd.Seek(0, 1)
		fmtFatalErr(err)

		_, length, _ := a.Cursor.Get(path)
		fmtPrintf("[%10s] %s", humansize(int64(length)), o.fill(path))

		if _, err := a.Stream(w, path); err != nil {
			w.Close()
			fmtMaybeErr(finalpath, err)
			if flags.checksum && err == arp.ErrCorruptedHash {
				badFiles++
			}
			continue
		}
		w.Close()

		_, err = a.Fd.Seek(old, 0)
		fmtFatalErr(err)
	}

	if flags.action == 'l' {
		fmtPrintln("\nTotal entries:", a.TotalEntries(), ", created at:", a.Created.Format(tf))
	} else {
		fmtPrintln("\nFinished in", o.elapsed())
	}

	if badFiles > 0 {
		fmtPrintferr("Found %d corrupted files\n", badFiles)
	}
}
