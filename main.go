package main

import (
	"bytes"
	"flag"
	"fmt"
	_ "image/jpeg"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coyove/gowebp/ar"
)

var merger = flag.String("m", "", "")
var splitter = flag.String("s", "", "")
var dir = flag.String("x", "", "")
var listen = flag.String("l", ":8888", "")

func main() {
	flag.Parse()

	if *dir != "" {
		dirs, _ := ioutil.ReadDir(*dir)
		for _, d := range dirs {
			if !d.IsDir() {
				continue
			}
			p := filepath.Join(*dir, d.Name())

			ar.ArchiveDir(p, filepath.Join(p, "merge.pkg"), true)
		}
		return
	}

	if *merger != "" {
		start := time.Now()
		fmt.Println(ar.ArchiveDir(*merger, filepath.Join(*merger, "merge.pkg"), false))
		fmt.Println(time.Now().Sub(start).Nanoseconds()/1e6, "ms")
		return
	}

	if *splitter != "" {
		ar, err := ar.OpenArchive(*splitter, false)
		fmt.Println(err)
		fmt.Printf("\nMode      Modtime (UTC)           Offset       Size\n\n")
		ar.Iterate(func(path string, mode uint16, modtime time.Time, start, l uint64) error {
			fmt.Printf("%s %s %010x %10d %s\n", uint16mod(mode),
				modtime.UTC().Format("2006-01-02 15:04:05"), start, l, path,
			)
			return nil
		})

		fmt.Println("Total files:", ar.TotalFiles())
		return
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		uri := r.URL.Path
		if len(uri) < 1 {
			w.WriteHeader(400)
			return
		}

		if false {
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
		} else {
			split(w, `C:\Users\zhangz\Desktop\coyote\go\merge.pkg`, uri[1:])
		}
	})

	log.SetFlags(log.Lshortfile | log.Ltime | log.Lmicroseconds)
	log.Println("hello", *listen)
	http.ListenAndServe(*listen, nil)
}

func uint16mod(m uint16) string {
	buf := &bytes.Buffer{}
	for i := 0; i < 9; i++ {
		if m<<uint16(7+i)>>15 == 1 {
			buf.WriteByte("rwx"[i%3])
		} else {
			buf.WriteString("-")
		}
	}
	return buf.String()
}
