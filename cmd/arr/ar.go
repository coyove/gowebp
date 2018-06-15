package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
	"unsafe"

	"github.com/coyove/common/rand"
)

var one uint64 = 1

// ErrOmitFile omits the file when iterating
var ErrOmitFile = errors.New("")

// ErrCorruptedHash indicates an invalid hash
var ErrCorruptedHash = errors.New("corrupted file")

const (
	dirguid  = "\xd8\x4d\xd3\xd0\x67\x09\x43\x64\x98\x19\x3f\x6e\x61\x4c\x2f\xd4\xd8\x4d\xd3\xd0\x67\x09\x43\x64\x98\x19\x3f\x6e\x61\x4c\x2f\xd4"
	dummy16  = "\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"
	metasize = 24

	DirFlag = 0xfedcba9876543210
)

type EntryInfo struct {
	Path    string
	Modtime time.Time
	Hash    [sha256.Size]byte
	Mode    uint32
	IsDir   bool
	score   byte
}

func (e *EntryInfo) Dirstring() string {
	dir := "d"
	if !e.IsDir {
		dir = "-"
	}
	return dir
}

// Archive represents a standard archive storage
type Archive struct {
	fd       *os.File
	cursor   *uint64map
	size     int64
	path     string
	pathhash map[uint64]*EntryInfo

	Info    os.FileInfo
	Created time.Time
}

// OpenArchive opens an archive with the given path
func OpenArchive(path string, jmpTableOnly bool) (*Archive, error) {
	ar, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := ar.Stat()
	if err != nil {
		return nil, err
	}

	x := &Archive{
		fd:     ar,
		cursor: &uint64map{},
		size:   st.Size(),
		path:   path,
		Info:   st,
	}

	p := [metasize]byte{}
	if _, err := ar.Read(p[:]); err != nil {
		return nil, err
	}
	if string(p[:4]) != "zzz0" {
		return nil, fmt.Errorf("invalid header")
	}

	count := binary.BigEndian.Uint32(p[4:8])
	x.Created = time.Unix(int64(binary.BigEndian.Uint32(p[8:12])), 0)
	if p[12] != *(*byte)(unsafe.Pointer(&one)) {
		return nil, fmt.Errorf("unmatched endianness")
	}

	x.cursor.data = make([][3]uint64, count)
	if _, err := ar.Read(x.cursor.bytes()); err != nil {
		return nil, err
	}

	if jmpTableOnly {
		return x, nil
	}

	x.pathhash = make(map[uint64]*EntryInfo)
	pathbuf := make([]byte, 256)
	for i := uint32(0); i < count; i++ {
		if _, err := ar.Read(p[:2]); err != nil {
			return nil, err
		}

		fi := &EntryInfo{}

		pathlen := int(binary.BigEndian.Uint16(p[:2]))
		if pathlen > len(pathbuf) {
			pathbuf = make([]byte, pathlen)
		}

		if _, err := ar.Read(pathbuf[:4]); err != nil {
			return nil, err
		}
		fi.Mode = binary.BigEndian.Uint32(pathbuf)

		if _, err := ar.Read(pathbuf[:4]); err != nil {
			return nil, err
		}
		fi.Modtime = time.Unix(int64(binary.BigEndian.Uint32(pathbuf)), 0)

		if _, err := ar.Read(pathbuf[:sha256.Size]); err != nil {
			return nil, err
		}
		copy(fi.Hash[:], pathbuf[:sha256.Size])
		fi.IsDir = bytes.Equal(fi.Hash[:], []byte(dirguid))

		if _, err := ar.Read(pathbuf[:pathlen]); err != nil {
			return nil, err
		}
		fi.Path = string(pathbuf[:pathlen])
		x.pathhash[fnv64Sum(fi.Path)] = fi
	}

	return x, nil
}

// Dup duplicates the current archive
// Note that the dupee may be closed already, but the dup one
// will still have a new fd for user to operate with
func (a *Archive) Dup() *Archive {
	ar, err := os.Open(a.path)
	if err != nil {
		return nil
	}
	a2 := *a
	a2.fd = ar
	return &a2
}

// Close closes the archive fd
// Note that it can still be duplicated after closing
func (a *Archive) Close() error {
	return a.fd.Close()
}

func (a *Archive) GetFile(path string) (startPos uint64, size uint64, ok bool) {
	startPos, size, ok = a.cursor.get(path)
	if startPos == DirFlag && size == DirFlag {
		ok = false
	}
	return
}

func (a *Archive) Contains(path string) bool {
	_, _, ok := a.cursor.get(path)
	return ok
}

// Stream streams the given file into w
func (a *Archive) Stream(w io.Writer, path string) (int64, error) {
	start, length, ok := a.cursor.get(path)
	if !ok {
		return 0, fmt.Errorf("can't stream %s", path)
	}
	if _, err := a.fd.Seek(int64(start), 0); err != nil {
		return 0, err
	}

	var wr int64
	var err error

	if a.pathhash == nil {
		wr, err = io.CopyN(w, a.fd, int64(length))
	} else {
		var h []byte
		wr, h, err = hashcopyN(w, a.fd, int64(length))
		if !bytes.Equal(h, a.pathhash[fnv64Sum(path)].Hash[:]) {
			return wr, ErrCorruptedHash
		}
	}

	if err != nil {
		return 0, err
	}
	if wr != int64(length) {
		return 0, io.ErrShortWrite
	}
	return wr, nil
}

func (a *Archive) TotalEntries() int {
	return len(a.cursor.data)
}

// GetInfo returns the basic info of a file in a fast way
func (a *Archive) GetInfo(path string) (info *EntryInfo, ok bool) {
	if a.pathhash == nil {
		return nil, false
	}
	h := fnv64Sum(path)
	fi, ok := a.pathhash[h]
	return fi, ok
}

// Iterate iterates through files in the archive
func (a *Archive) Iterate(cb func(*EntryInfo, uint64, uint64) error) error {
	for _, x := range a.cursor.data {
		y := a.pathhash[x[0]]
		if y.IsDir {
			x[1], x[2] = 0, 0
		}
		if err := cb(y, x[1], x[2]); err != nil {
			return err
		}
	}
	return nil
}

// ArchiveDir archives the given directory into an archive
// struct:
// +---------------+----------+-------+------+-------+------+-- - -
// | 24b metatable | jmptable | file1 | guid | file2 | guid | ...
// +---------------+----------+-------+------+-------+------+-- - -
// Currently there are only 3 fields in metatable:
//    (4b) Magic code, current: zzz0
// 1. (4b) Total files
// 2. (4b) Archive created time
// 3. (1b) Endianness, 1: Big endian, 0: Little endian
func ArchiveDir(dirpath, arpath string) {
	pathbuflen, full := 0, make([]string, 0)
	o := newoneliner()

	var pathslist *os.File
	var pathslistpath string
	var totalFoundEntries int
	const fileHdr = 2 + 4 + 4 + sha256.Size

	fmtPrintln("Archiving:", dirpath)
	fmtPrintln("Output:   ", arpath)

	filepath.Walk(dirpath, func(path string, info os.FileInfo, err error) error {
		path = strings.Replace(path, "\\", "/", -1)

		if err != nil {
			return err
		}
		info2, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !os.SameFile(info, info2) {
			// the path points to a symbolic link, we don't support it
			return nil
		}
		if path == arpath {
			return nil
		}

		if len(path) > 65535 {
			fmtFatalErr(fmt.Errorf("found a path longer than 65535, you serious?"))
		}

		if pathslist != nil {
			pathslist.WriteString(path + "\n")
		} else {
			full = append(full, path)
			if len(full) > 1024 {
				pathslistpath = arpath + "." + fmt.Sprintf("%x", rand.New().Fetch(4)) + ".paths"
				pathslist, _ = os.Create(pathslistpath)
				pathslist.WriteString(strings.Join(full, "\n") + "\n")
				full = full[:0]
			}
		}
		totalFoundEntries++

		path = rel(dirpath, path)
		fmtPrintf("\r[%s] Search base: %s", o.elapsed(), o.fill(path))

		pathbuflen += len(path) + fileHdr
		// info.Mode(): uint32
		// info.ModTime(): uint32
		return nil
	})

	fmtPrintf("\r[%s] Search base: found %d files, start archiving...\n", o.elapsed(), totalFoundEntries)

	ar, err := os.Create(arpath)
	fmtFatalErr(err)

	defer ar.Close()

	p, count := [metasize]byte{'z', 'z', 'z', '0'}, totalFoundEntries

	binary.BigEndian.PutUint32(p[4:8], uint32(count))
	binary.BigEndian.PutUint32(p[8:12], uint32(time.Now().Unix()))
	p[12] = *(*byte)(unsafe.Pointer(&one))

	_, err = ar.Write(p[:])
	fmtFatalErr(err)

	headerlen := (metasize*count + pathbuflen + 15) / 16 * 16
	// log.Println(headerlen, count, pathbuflen)
	for i := 0; i < headerlen/16; i++ {
		_, err := ar.WriteString(dummy16)
		fmtFatalErr(err)
	}

	cursor, pcursor := int64(headerlen+metasize), int64(count)*metasize+metasize
	m := uint64map{}
	iteratePaths(full, pathslist, func(i int, path string) {
		var file *os.File
		var st os.FileInfo
		var err error

		st, err = os.Stat(path)
		if err != nil {
			fmtMaybeErr(path, err)
			return
		}
		if !st.IsDir() {
			file, err = os.Open(path)
		}
		if err != nil {
			fmtMaybeErr(path, err)
			return
		}

		finalpath := rel(dirpath, path)
		finalpath = strings.Replace(finalpath, "\\", "/", -1)
		fmtPrintf("\r[%s] [%02d%%] ", o.elapsed(), (i * 100 / totalFoundEntries))

		if st.IsDir() {
			fmtPrintf("[   Fdir   ] %s", o.fill(finalpath))
		} else {
			fmtPrintf("[%10s] %s", humansize(st.Size()), o.fill(finalpath))
		}

		binary.BigEndian.PutUint16(p[:2], uint16(len(finalpath)))
		binary.BigEndian.PutUint32(p[2:6], uint32(st.Mode()))
		binary.BigEndian.PutUint32(p[6:10], uint32(st.ModTime().Unix()))

		_, err = ar.WriteAt(p[:10], pcursor)
		fmtFatalErr(err)
		pcursor += 10

		if st.IsDir() {
			// for directories, they have no real contents
			// so we will use dirFlag as hash to identify

			_, err = ar.WriteAt([]byte(dirguid), pcursor)
			fmtFatalErr(err)
			pcursor += sha256.Size

			_, err = ar.WriteAt([]byte(finalpath), pcursor)
			fmtFatalErr(err)

			pcursor += int64(len(finalpath))
			m.push(finalpath, DirFlag, DirFlag)
			return
		}

		// append the file content to the end of the archive
		n, h, err := hashcopy(ar, file)
		if err != nil {
			fmtMaybeErr(path, err)
			return
		}

		// write hash at pcursor
		_, err = ar.WriteAt(h, pcursor)
		fmtFatalErr(err)

		pcursor += sha256.Size
		_, err = ar.WriteAt([]byte(finalpath), pcursor)
		fmtFatalErr(err)

		pcursor += int64(len(finalpath))
		m.push(finalpath, uint64(cursor), uint64(n))

		cursor += n
		if err := file.Close(); err != nil {
			fmtMaybeErr(path, err)
			return
		}

		if flags.deloriginal && flags.delimm && !st.IsDir() {
			os.Remove(full[i])
		}
	})

	m.seal()
	ar.WriteAt(m.bytes(), metasize)

	if flags.deloriginal {
		for _, p := range full {
			os.Remove(p)
		}
	}

	if pathslist != nil {
		pathslist.Close()
		os.Remove(pathslistpath)
	}

	st, _ := os.Stat(arpath)
	size := st.Size()
	fmtPrintln("\nFinished in", o.elapsed(), ", size:", size, "bytes /", humansize(size))
}

type uint64map struct {
	data [][3]uint64
}

func fnv64Sum(data string) uint64 {
	const prime64 = 1099511628211
	var hash uint64
	for _, c := range data {
		hash *= prime64
		hash ^= uint64(c)
	}
	return hash
}

func (m *uint64map) push(k string, v, l uint64) {
	if m.data == nil {
		m.data = make([][3]uint64, 0)
	}
	m.data = append(m.data, [3]uint64{fnv64Sum(k), v, l})
}

func (m *uint64map) bytes() []byte {
	if m.data == nil {
		return nil
	}
	x := (*reflect.SliceHeader)(unsafe.Pointer(&m.data))
	r := reflect.SliceHeader{
		Len:  x.Len * 24,
		Cap:  x.Cap * 24,
		Data: x.Data,
	}
	return *(*[]byte)(unsafe.Pointer(&r))
}

func (m *uint64map) seal() {
	sort.Slice(m.data, func(i, j int) bool {
		return m.data[i][0] < m.data[j][0]
	})
}

func (m *uint64map) get(key string) (uint64, uint64, bool) {
	var start, end, k uint64 = 0, uint64(len(m.data)), fnv64Sum(key)
AGAIN:
	if start >= end {
		return 0, 0, false
	}
	mid := (start + end) / 2
	if x := m.data[mid]; k == x[0] {
		return x[1], x[2], true
	} else if k < x[0] {
		end = mid
		goto AGAIN
	} else {
		start = mid + 1
		goto AGAIN
	}
}
