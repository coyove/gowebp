package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unsafe"

	"sync"
)

const maxfile = 2

type archive struct {
	sync.Mutex
	fd     *os.File
	ref    *fileref
	count  int
	cursor *uint64map
	size   int64
	path   string
}

type fileref struct {
	sync.Mutex
	m map[string]*archive
}

func (d *fileref) open(path string) (*archive, error) {
	d.Lock()
	defer d.Unlock()

	if d.m == nil {
		d.m = make(map[string]*archive)
	}

	xpath := path + strconv.Itoa(rand.Intn(maxfile))

	x := d.m[xpath]
	if x == nil {
		ar, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		st, err := ar.Stat()
		if err != nil {
			return nil, err
		}

		x = &archive{
			fd:     ar,
			count:  1,
			cursor: &uint64map{},
			size:   st.Size(),
			ref:    d,
			path:   xpath,
		}
		p := [8]byte{}
		if _, err := ar.Read(p[:]); err != nil {
			return nil, err
		}
		count := binary.BigEndian.Uint64(p[:])
		x.cursor.data = make([][3]uint64, count)

		x.cursor.seal()
		d.m[xpath] = x
	} else {
		x.Lock()
		x.count++
		x.Unlock()
	}
	return x, nil
}

func (d *fileref) close(ar *archive) {
	d.Lock()
	if ar.count--; ar.count == 0 {
		delete(ar.ref.m, ar.path)
		ar.fd.Close()
	}
	d.Unlock()
}

var cofileref = &fileref{}

func init() {
	// go func() {
	// 	for range time.Tick(time.Second) {
	// 		fmt.Println(cofileref.status())
	// 	}
	// }()
}

func split(w http.ResponseWriter, path, name string) {
	ar, err := cofileref.open(path)
	if err != nil {
		errImage(w, err.Error())
		return
	}
	defer cofileref.close(ar)

	if x, l, ok := ar.cursor.get(name); ok {
		ar.Lock()
		if _, err := ar.fd.Seek(int64(x), 0); err != nil {
			ar.Unlock()
			errImage(w, err.Error())
			return
		}

		w.Header().Add("Content-Type", "image/webp")
		wr, err := io.CopyN(w, ar.fd, int64(l))
		ar.Unlock()

		if err != nil || wr != int64(l) {
			errImage(w, fmt.Sprintf("%v, written: %d/%d", err, wr, int64(l)))
			return
		}
	} else {
		errImage(w, "invalid image index")
	}
}

func splitInfo(path string) {
	ar, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer ar.Close()
	readInt64 := func() int64 {
		p := [8]byte{}
		if _, err := ar.Read(p[:]); err != nil {
			log.Fatal(err)
		}
		return int64(binary.BigEndian.Uint64(p[:]))
	}
	count := readInt64()
	if count == -1 {
		return
	}
	cursors := make([]int64, count)
	pathes := make([]string, count)
	p := [248]byte{}
	for i := int64(0); i < count; i++ {
		cursors[i] = readInt64()
		if _, err := ar.Read(p[:]); err != nil {
			log.Fatal(err)
		}
		pathes[i] = strings.TrimSpace(string(p[:]))
	}
	st, err := ar.Stat()
	if err != nil {
		log.Fatal(err)
	}
	for idx, c := range cursors {
		if _, err := ar.Seek(c+headerSize, 0); err != nil {
			log.Fatal(err)
		}

		end := st.Size() - 16 - 16
		if idx < len(cursors)-1 {
			end = cursors[idx+1] - 16
		}
		ln := end - c
		if _, err := ar.Seek(end+headerSize, 0); err != nil {
			log.Fatal(err)
		}
		log.Println("name:", pathes[idx], "size:", ln)
	}
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
	var r reflect.SliceHeader
	r.Len = x.Len * 24
	r.Cap = x.Cap * 24
	r.Data = x.Data
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
