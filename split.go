package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"

	"sync"
)

const maxfile = 2

type archive struct {
	sync.Mutex
	fd     *os.File
	ref    *fileref
	count  int
	cursor map[string][2]int64
	size   int64
	path   string
}

type fileref struct {
	sync.Mutex
	m map[string]*archive
}

func (d *fileref) status() string {
	d.Lock()
	defer d.Unlock()

	buf := &bytes.Buffer{}
	for k, v := range d.m {
		buf.WriteString(fmt.Sprintf("%s(%v): %d\n", k, v.fd, v.count))
	}
	return buf.String()
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
			cursor: make(map[string][2]int64),
			size:   st.Size(),
			ref:    d,
			path:   xpath,
		}
		readInt64 := func() int64 {
			p := [8]byte{}
			if _, err := ar.Read(p[:]); err != nil {
				return -1
			}
			return int64(binary.BigEndian.Uint64(p[:]))
		}

		count := readInt64()
		lastc, lastp := int64(-1), ""
		if count == -1 {
			return nil, fmt.Errorf("invalid counter: %d", count)
		}
		p := [248]byte{}
		for i := int64(0); i < count; i++ {
			c := readInt64()
			if c == -1 {
				return nil, fmt.Errorf("invalid cursor: %d", c)
			}
			if _, err := ar.Read(p[:]); err != nil {
				return nil, err
			}

			path := strings.TrimSpace(string(p[:]))
			if lastc != -1 {
				x.cursor[lastp] = [2]int64{lastc, c - lastc - 16}
			}
			lastc, lastp = c, path
		}
		if count > 0 {
			x.cursor[lastp] = [2]int64{lastc, x.size - lastc - 16 - 16}
		}
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

	if x, ok := ar.cursor[name]; ok {
		ar.Lock()
		if _, err := ar.fd.Seek(x[0]+headerSize, 0); err != nil {
			ar.Unlock()
			errImage(w, err.Error())
			return
		}

		w.Header().Add("Content-Type", "image/webp")
		wr, err := io.CopyN(w, ar.fd, x[1])
		ar.Unlock()

		if err != nil || wr != x[1] {
			errImage(w, fmt.Sprintf("%v, written: %d/%d", err, wr, x[1]))
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
