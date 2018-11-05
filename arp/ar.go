package arp

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"reflect"
	"sort"
	"time"
	"unsafe"
)

const Header = "zzz0"

// One is a uint64 1
var One uint64 = 1

// ErrOmitFile omits the file when iterating
var ErrOmitFile = errors.New("")

// ErrCorruptedHash indicates an invalid hash
var ErrCorruptedHash = errors.New("corrupted file")

var ErrInvalidHeader = errors.New("invalid magic header")

const (
	DirGUID  = "\xd8\x4d\xd3\xd0\x67\x09\x43\x64\x98\x19\x3f\x6e\x61\x4c\x2f\xd4\xd8\x4d\xd3\xd0\x67\x09\x43\x64\x98\x19\x3f\x6e\x61\x4c\x2f\xd4"
	MetaSize = 24
	DirFlag  = 0xfedcba9876543210
	ErrFlag  = 0xfedcbacccccccccc
)

type EntryInfo struct {
	Path    string
	Modtime time.Time
	Hash    [sha256.Size]byte
	Mode    uint32
	IsDir   bool
	score   byte
}

// Archive represents a standard archive storage
type Archive struct {
	Fd       *os.File
	Cursor   *Uint64OneTwoMap
	Size     int64
	Path     string
	pathhash map[uint64]*EntryInfo
	Info     os.FileInfo
	Created  time.Time
	Password string
}

// DumpArchiveJmpTable dumps the header
func DumpArchiveJmpTable(path, dumppath string) error {
	ar, err := os.Open(path)
	if err != nil {
		return err
	}
	defer ar.Close()

	df, err := os.Create(dumppath)
	if err != nil {
		return err
	}
	defer df.Close()

	p, err := DumpArchiveJmpTableBytes(ar)
	if err != nil {
		return err
	}

	_, err = df.Write(p)
	return err
}

func DumpArchiveJmpTableBytes(ar io.Reader) ([]byte, error) {
	cursor := &Uint64OneTwoMap{}
	p := [MetaSize]byte{}
	if _, err := ar.Read(p[:]); err != nil {
		return nil, err
	}
	if string(p[:4]) != Header {
		return nil, ErrInvalidHeader
	}

	count := binary.BigEndian.Uint32(p[4:8])
	if p[12] != *(*byte)(unsafe.Pointer(&One)) {
		return nil, fmt.Errorf("unmatched endianness")
	}

	cursor.Data = make([][3]uint64, count)
	if _, err := ar.Read(cursor.Bytes()); err != nil {
		return nil, err
	}

	return append(p[:], cursor.Bytes()...), nil
}

// OpenArchive opens an archive with the given path
func OpenArchive(path string, password string, jmpTableOnly bool) (*Archive, error) {
	ar, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := ar.Stat()
	if err != nil {
		return nil, err
	}

	x, err := OpenArchiveBytes(ar, password, jmpTableOnly)
	if err != nil {
		return nil, err
	}

	x.Fd = ar
	x.Size = st.Size()
	x.Path = path
	x.Info = st
	return x, nil
}

// OpenArchiveBytes opens an archive from an io.Reader
func OpenArchiveBytes(ar io.Reader, password string, jmpTableOnly bool) (*Archive, error) {
	x := &Archive{
		Cursor:   &Uint64OneTwoMap{},
		Password: password,
	}

	p := [MetaSize]byte{}
	if _, err := ar.Read(p[:]); err != nil {
		return nil, err
	}
	if string(p[:4]) != Header {
		return nil, ErrInvalidHeader
	}

	count := binary.BigEndian.Uint32(p[4:8])
	x.Created = time.Unix(int64(binary.BigEndian.Uint32(p[8:12])), 0)
	if p[12] != *(*byte)(unsafe.Pointer(&One)) {
		return nil, fmt.Errorf("unmatched endianness")
	}

	x.Cursor.Data = make([][3]uint64, count)
	if _, err := ar.Read(x.Cursor.Bytes()); err != nil {
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
		if pathlen == 0 {
			continue
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
		fi.IsDir = bytes.Equal(fi.Hash[:], []byte(DirGUID))

		if _, err := ar.Read(pathbuf[:pathlen]); err != nil {
			return nil, err
		}

		fi.Path = string(x.DecodeBytes(pathbuf[:pathlen]))
		x.pathhash[fnv64Sum(fi.Path)] = fi
	}

	return x, nil
}

func (a *Archive) DecodeBytes(in []byte) []byte {
	buf, _ := ioutil.ReadAll(WrapReaderWriter(bytes.NewReader(in), nil, a.Password))
	return buf
}

// Dup duplicates the current archive
// Note that the dupee may be closed already, but the dup one
// will still have a new fd for user to operate with
func (a *Archive) Dup() *Archive {
	ar, err := os.Open(a.Path)
	if err != nil {
		return nil
	}
	a2 := *a
	a2.Fd = ar
	return &a2
}

// Close closes the archive fd
// Note that it can still be duplicated after closing
func (a *Archive) Close() error {
	return a.Fd.Close()
}

func (a *Archive) GetFile(path string) (startPos uint64, size uint64, ok bool) {
	startPos, size, ok = a.Cursor.Get(path)
	if startPos == DirFlag && size == DirFlag {
		ok = false
	}
	if startPos == ErrFlag && size == ErrFlag {
		ok = false
	}
	return
}

func (a *Archive) Contains(path string) bool {
	_, _, ok := a.Cursor.Get(path)
	return ok
}

// Stream streams the given file into w
func (a *Archive) Stream(w io.Writer, path string) (int64, error) {
	start, length, ok := a.Cursor.Get(path)
	if !ok {
		return 0, fmt.Errorf("can't stream %s", path)
	}
	if start == ErrFlag {
		return 0, fmt.Errorf("%s is a bad file", path)
	}

	if _, err := a.Fd.Seek(int64(start), 0); err != nil {
		return 0, err
	}

	var wr int64
	var err error

	w = WrapReaderWriter(nil, w, a.Password)

	if a.pathhash == nil {
		wr, err = io.CopyN(w, a.Fd, int64(length))
	} else {
		var h []byte
		wr, h, err = HashCopyN(w, a.Fd, int64(length))
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
	return len(a.Cursor.Data)
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
	for _, x := range a.Cursor.Data {
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

type Uint64OneTwoMap struct {
	Data [][3]uint64
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

func (m *Uint64OneTwoMap) Push(k string, v, l uint64) {
	if m.Data == nil {
		m.Data = make([][3]uint64, 0)
	}
	m.Data = append(m.Data, [3]uint64{fnv64Sum(k), v, l})
}

func (m *Uint64OneTwoMap) Bytes() []byte {
	if m.Data == nil {
		return nil
	}
	x := (*reflect.SliceHeader)(unsafe.Pointer(&m.Data))
	r := reflect.SliceHeader{
		Len:  x.Len * 24,
		Cap:  x.Cap * 24,
		Data: x.Data,
	}
	return *(*[]byte)(unsafe.Pointer(&r))
}

func (m *Uint64OneTwoMap) Seal() {
	sort.Slice(m.Data, func(i, j int) bool {
		return m.Data[i][0] < m.Data[j][0]
	})
}

func (m *Uint64OneTwoMap) Get(key string) (uint64, uint64, bool) {
	var start, end, k uint64 = 0, uint64(len(m.Data)), fnv64Sum(key)
AGAIN:
	if start >= end {
		return 0, 0, false
	}
	mid := (start + end) / 2
	if x := m.Data[mid]; k == x[0] {
		return x[1], x[2], true
	} else if k < x[0] {
		end = mid
		goto AGAIN
	} else {
		start = mid + 1
		goto AGAIN
	}
}
