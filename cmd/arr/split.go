package main

import (
	"bytes"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"log"
	"net/http"
	"time"

	"sync"

	"github.com/coyove/common/dejavu"
	"github.com/coyove/gowebp"
)

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

type fileref struct {
	sync.Mutex
	m map[string]*Archive
}

func (d *fileref) open(path string) (*Archive, error) {
	d.Lock()
	defer d.Unlock()

	if d.m == nil {
		d.m = make(map[string]*Archive)
	}

	x := d.m[path]
	// AGAIN:
	if x == nil {
		a, err := OpenArchive(path, false)
		if err != nil {
			return nil, err
		}
		x = a
		d.m[path] = a
		a.Close()
		// } else if  x.Info.ModTime().Unix() != time.Now().Unix() {
		// 	x = nil
		// 	log.Println("reload",path)
		// 	goto AGAIN
	}
	return x.Dup(), nil
}

var cofileref = &fileref{}

func split(w http.ResponseWriter, path, name string, jpg bool) {
	start := time.Now()
	a, err := cofileref.open(path)
	if err != nil {
		errImage(w, err.Error())
		return
	}
	defer a.Close()

	log.Println("serve", path, name)
	if a.Contains(name) {
		if jpg {
			p := &bytes.Buffer{}
			if _, err := a.Stream(p, name); err != nil {
				if err == ErrCorruptedHash {
					log.Println("corrupted content:", name)
				} else {
					w.Header().Del("Content-Type")
					errImage(w, err.Error())
				}
			}

			gowebp.DecodeToJPEG(w, p.Bytes(), &jpeg.Options{Quality: 80})
		} else {
			if _, err := a.Stream(w, name); err != nil {
				if err == ErrCorruptedHash {
					log.Println("corrupted content:", name)
				} else {
					w.Header().Del("Content-Type")
					errImage(w, err.Error())
				}
			}
		}
	} else {
		errImage(w, "invalid image index")
	}
	diff := time.Now().Sub(start).Nanoseconds() / 1e6
	if diff > 100 {
		log.Println(path, name, diff, "ms")
	}
}
