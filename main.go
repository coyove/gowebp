package main

import (
	"encoding/binary"
	"flag"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/coyove/common/dejavu"
)

var merger = flag.String("m", "", "")
var splitter = flag.String("s", "", "")
var listen = flag.String("l", ":8888", "")
var guid = []byte("\xd8\x4d\xd3\xd0\x67\x09\x43\x64\x98\x19\x3f\x6e\x61\x4c\x2f\xd4")
var eoa = []byte("\xdd\x32\xea\x0d\x87\xd4\x4e\x05\xab\x32\x0a\xee\x75\x47\xd9\x58")
var dummy1k = strings.Repeat("\x00", 1024)

const headerSize = 1024 * 1024

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
	for i := 0; i < len(message); i++ {
		dejavu.DrawText(canvas, string(message[i]), x, y+dejavu.Height, image.White)
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

func merge(path string) {
	ar, err := os.Create(filepath.Join(path, "merge"))
	if err != nil {
		log.Fatal(err)
	}
	defer ar.Close()

	for i := 0; i < 1024; i++ {
		ar.WriteString(dummy1k)
	}

	var cursor int64
	cursors, pathes := make([]int64, 0), make([]string, 0)
	basepath := path
	filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if !strings.HasSuffix(path, ".webp") {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()

		cursors = append(cursors, cursor)
		n, err := io.Copy(ar, file)
		if err != nil {
			log.Fatal(err)
		}
		ar.Write(guid)
		cursor += n + 16
		path, _ = filepath.Rel(basepath, path)
		if len(path) > 248 {
			pathes = append(pathes, path[len(path)-248:])
		} else {
			pathes = append(pathes, path+strings.Repeat(" ", 248-len(path)))
		}
		return nil
	})
	ar.Write(eoa)

	p := [256]byte{}
	binary.BigEndian.PutUint64(p[:8], uint64(len(cursors)))
	ar.WriteAt(p[:8], 0)
	for i, c := range cursors {
		binary.BigEndian.PutUint64(p[:8], uint64(c))
		copy(p[8:], pathes[i])
		ar.WriteAt(p[:], int64(i*256+8))
	}
}

func main() {
	flag.Parse()

	if *merger != "" {
		merge(*merger)
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

		uri = strings.Replace(uri[1:], "thumb/", "", -1)
		parts := strings.Split(uri, "/")
		if len(parts) != 3 {
			w.WriteHeader(400)
			return
		}

		mergepath := "./gallery/" + parts[0] + "/" + parts[1] + "/merge"
		w.Header().Add("Access-Control-Allow-Origin", "*")
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
