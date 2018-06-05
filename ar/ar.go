package ar

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
	"unsafe"
)

var one uint64 = 1

const (
	guid     = "\xd8\x4d\xd3\xd0\x67\x09\x43\x64\x98\x19\x3f\x6e\x61\x4c\x2f\xd4"
	dummy16  = "\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"
	metasize = 24
)

type fileinfo struct {
	path    string
	mode    uint32
	modtime uint32
}

// Archive represents a standard archive storage
type Archive struct {
	fd       *os.File
	cursor   *uint64map
	size     int64
	path     string
	pathhash map[uint64]fileinfo
	Info     os.FileInfo
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

	count := binary.BigEndian.Uint64(p[4:12])
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

	x.pathhash = make(map[uint64]fileinfo)
	pathbuf := make([]byte, 256)
	for i := uint64(0); i < count; i++ {
		if _, err := ar.Read(p[:2]); err != nil {
			return nil, err
		}
		pathlen := int(binary.BigEndian.Uint16(p[:2]))
		if pathlen > len(pathbuf) {
			pathbuf = make([]byte, pathlen)
		}
		if _, err := ar.Read(pathbuf[:4]); err != nil {
			return nil, err
		}
		mode := binary.BigEndian.Uint32(pathbuf)
		if _, err := ar.Read(pathbuf[:4]); err != nil {
			return nil, err
		}
		modtime := binary.BigEndian.Uint32(pathbuf)
		if _, err := ar.Read(pathbuf[:pathlen]); err != nil {
			return nil, err
		}
		path := string(pathbuf[:pathlen])
		x.pathhash[fnv64Sum(path)] = fileinfo{path, mode, modtime}
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
	return a.cursor.get(path)
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

	wr, err := io.CopyN(w, a.fd, int64(length))
	if err != nil {
		return 0, err
	}
	if wr != int64(length) {
		return 0, io.ErrShortWrite
	}
	return wr, nil
}

func (a *Archive) TotalFiles() int {
	return len(a.cursor.data)
}

// GetFileInfo returns the basic info of a file in a fast way
func (a *Archive) GetFileInfo(path string) (mode uint16, modtime time.Time, ok bool) {
	h := fnv64Sum(path)
	var fi fileinfo
	if fi, ok = a.pathhash[h]; !ok {
		return
	}

	mode = uint16(fi.mode)
	modtime = time.Unix(int64(fi.modtime), 0)
	return
}

// Iterate iterates through files in the archive
func (a *Archive) Iterate(cb func(string, uint16, time.Time, uint64, uint64) error) error {
	for _, x := range a.cursor.data {
		y := a.pathhash[x[0]]
		err := cb(y.path, uint16(y.mode), time.Unix(int64(y.modtime), 0), x[1], x[2])
		if err != nil {
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
// Currently there are only two fields in metatable:
//    (4b) Magic code, current: zzz0
// 1. (8b) Total files
// 2. (1b) Endianness, 1: Big endian, 0: Little endian
func ArchiveDir(dirpath, arpath string, deloriginal bool) (int, error) {
	full := make([]string, 0)
	pathbuflen := 0

	os.Remove(arpath)

	if err := filepath.Walk(dirpath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		full = append(full, path)
		if len(path) > 65535 {
			panic("really?")
		}
		path, _ = filepath.Rel(dirpath, path)
		pathbuflen += len(path) + 2 + 4 + 4
		// info.Mode(): uint32
		// info.ModTime(): uint32
		return nil
	}); err != nil {
		return 0, err
	}

	ar, err := os.Create(arpath)
	if err != nil {
		return 0, err
	}
	defer ar.Close()

	p, count := [metasize]byte{'z', 'z', 'z', '0'}, len(full)

	binary.BigEndian.PutUint64(p[4:12], uint64(count))
	p[12] = *(*byte)(unsafe.Pointer(&one))
	if _, err := ar.Write(p[:]); err != nil {
		return 0, err
	}

	headerlen := (metasize*count + pathbuflen + 15) / 16 * 16
	// log.Println(headerlen, count, pathbuflen)
	for i := 0; i < headerlen/16; i++ {
		if _, err := ar.WriteString(dummy16); err != nil {
			return 0, err
		}
	}

	cursor, pcursor := int64(headerlen+metasize), int64(count)*metasize+metasize
	m := uint64map{}
	for _, path := range full {
		file, err := os.Open(path)
		if err != nil {
			return 0, err
		}

		st, err := os.Stat(path)
		path, _ = filepath.Rel(dirpath, path)
		path = strings.Replace(path, "\\", "/", -1)

		binary.BigEndian.PutUint16(p[:2], uint16(len(path)))
		if err == nil {
			binary.BigEndian.PutUint32(p[2:6], uint32(st.Mode()))
			binary.BigEndian.PutUint32(p[6:10], uint32(st.ModTime().Unix()))
		} else {
			binary.BigEndian.PutUint64(p[2:], 0)
		}

		if _, err := ar.WriteAt(p[:10], pcursor); err != nil {
			return 0, err
		}
		if _, err := ar.WriteAt([]byte(path), pcursor+10); err != nil {
			return 0, err
		}
		pcursor += 2 + 4 + 4 + int64(len(path))

		n, err := io.Copy(ar, file)
		if err != nil {
			return 0, err
		}
		if _, err := ar.WriteString(guid); err != nil {
			return 0, err
		}
		m.push(path, uint64(cursor), uint64(n))

		cursor += n + 16
		if err := file.Close(); err != nil {
			return 0, err
		}
	}

	m.seal()
	ar.WriteAt(m.bytes(), metasize)

	if deloriginal {
		for _, p := range full {
			os.Remove(p)
		}
	}

	return count, nil
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
