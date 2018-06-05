package main

import (
	"encoding/binary"
	"flag"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/coyove/common/dejavu"
)

var merger = flag.String("m", "", "")
var splitter = flag.String("s", "", "")
var dir = flag.String("x", "", "")
var listen = flag.String("l", ":8888", "")
var guid = []byte("\xd8\x4d\xd3\xd0\x67\x09\x43\x64\x98\x19\x3f\x6e\x61\x4c\x2f\xd4")
var eoa = []byte("\xdd\x32\xea\x0d\x87\xd4\x4e\x05\xab\x32\x0a\xee\x75\x47\xd9\x58")

const dummy8 = "\x00\x00\x00\x00\x00\x00\x00\x00"
const headerSize = 1024 * 1024 * 8

func errImage(w http.ResponseWriter, message string) {
	const columns = 32
	rows := len(message) / columns
	if rows*columns != len(message) {
		rows++
	}
	x, y, width := 0, 0, dejavu.Width*columns
	if len(message) < columns {
		width = len(message) * dejavu.Width
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, (dejavu.FullHeight+2)*rows))
	draw.Draw(canvas, canvas.Bounds(), image.White, image.ZP, draw.Src)
	for i := 0; i < len(message); i++ {
		dejavu.DrawText(canvas, string(message[i]), x, y+dejavu.Height, image.Black)
		if i%columns == columns-1 {
			y += dejavu.FullHeight + 2
			x = 0
		} else {
			x += dejavu.Width
		}
	}
	w.Header().Add("Content-Type", "image/png")
	png.Encode(w, canvas)
}

func merge(path string, deleteoriginal bool) {
	os.Remove(filepath.Join(path, "merge"))

	full := make([]string, 0)
	basepath := path
	pathbuflen := 0

	filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		// if !strings.HasSuffix(path, ".webp") && !strings.HasSuffix(path, ".json") {
		// 	return nil
		// }
		full = append(full, path)
		pathbuflen += len(path) + 2
		return nil
	})

	ar, err := os.Create(filepath.Join(path, "merge"))
	if err != nil {
		log.Fatal(err)
	}
	defer ar.Close()

	p, count := [24]byte{}, len(full)
	binary.BigEndian.PutUint64(p[:8], uint64(count))
	ar.Write(p[:8])

	headerlen := (len(p)*count + pathbuflen + 7) / 8 * 8

	for i := 0; i < headerlen/8; i++ {
		ar.WriteString(dummy8)
	}

	headerlen += 8

	cursor, pcursor := int64(headerlen), int64(count*len(p)+8)
	m := uint64map{}
	for _, path := range full {
		file, err := os.Open(path)
		if err != nil {
			log.Fatal(err)
		}

		path, _ = filepath.Rel(basepath, path)

		binary.BigEndian.PutUint16(p[:2], uint16(len(path)))
		ar.WriteAt(p[:2], pcursor)
		ar.WriteAt([]byte(path), pcursor+2)
		pcursor += 2 + int64(len(path))

		n, err := io.Copy(ar, file)
		if err != nil {
			log.Fatal(err)
		}
		ar.Write(guid)

		// binary.BigEndian.PutUint64(p[:8], fnv64Sum(path))
		// binary.BigEndian.PutUint64(p[8:16], uint64(cursor))
		// binary.BigEndian.PutUint64(p[16:], uint64(n))
		// ar.WriteAt(p[:], 8+int64(i*len(p)))
		m.push(path, uint64(cursor), uint64(n))

		cursor += n + 16
		file.Close()
	}

	m.seal()
	ar.WriteAt(m.bytes(), 8)
	if deleteoriginal {
		for _, p := range full {
			os.Remove(p)
		}
	}
}

func main() {
	flag.Parse()

	if *dir != "" {
		dirs, _ := ioutil.ReadDir(*dir)
		for _, d := range dirs {
			if !d.IsDir() {
				continue
			}
			p := filepath.Join(*dir, d.Name())
			merge(p, true)
		}
		return
	}

	if *merger != "" {
		merge(*merger, false)
		return
	}

	if *splitter != "" {
		splitInfo(*splitter)
		return
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		uri := r.URL.Path
		if len(uri) < 1 {
			w.WriteHeader(400)
			return
		}

		uri = strings.Replace(uri[1:], "thumbs/", "", -1)
		parts := strings.Split(uri, "/")
		if len(parts) != 3 {
			w.WriteHeader(400)
			return
		}

		w.Header().Add("Access-Control-Allow-Origin", "*")

		mergepath := "./gallery/" + parts[0] + "/" + parts[1] + "/merge.webp"
		if !strings.HasSuffix(mergepath, ".webp") {
			http.ServeFile(w, r, "./gallery/"+parts[0]+"/"+parts[1]+"/"+parts[2])
			return
		}
		if _, err := os.Stat(mergepath); err == nil {
			split(w, mergepath, parts[2])
		} else {
			http.ServeFile(w, r, "./gallery/"+parts[0]+"/"+parts[1]+"/"+parts[2])
		}
	})

	log.SetFlags(log.Lshortfile)
	log.Println("hello", *listen)
	http.ListenAndServe(*listen, nil)
}
