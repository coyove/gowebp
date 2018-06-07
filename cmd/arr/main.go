package main

import (
	"bytes"
	"flag"
	"fmt"
	_ "image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/coyove/gowebp/ar"
)

var merger = flag.String("a", "", "directory to archive")
var verbose = flag.Bool("v", false, "verbose output")
var splitter = flag.String("l", "", "list archive content")

func humansize(size int64) string {
	var psize string
	if size < 1024*1024 {
		psize = fmt.Sprintf("%.2f KB", float64(size)/1024)
	} else if size < 1024*1024*1024 {
		psize = fmt.Sprintf("%.2f MB", float64(size)/1024/1024)
	} else if size < 1024*1024*1024*1024 {
		psize = fmt.Sprintf("%.2f GB", float64(size)/1024/1024/1024)
	} else {
		psize = fmt.Sprintf("%.2f TB", float64(size)/1024/1024/1024/1024)
	}
	return psize
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

func fmtPrintln(args ...interface{}) {
	if !*verbose {
		return
	}
	fmt.Println(args...)
}

func fmtPrintf(format string, args ...interface{}) {
	if !*verbose {
		return
	}
	fmt.Printf(format, args...)
}

func fmtPrintferr(format string, args ...interface{}) {
	os.Stderr.WriteString(fmt.Sprintf(format, args...))
}

func main() {
	flag.Parse()

	if *splitter != "" {
		*verbose = true
	}

	fmtPrintln("\nArr archive tool", runtime.GOOS, runtime.GOARCH, runtime.Version(), "\n")

	if *merger != "" {
		start := time.Now()
		lastp := ""
		_p := func(p string) string {
			p, _ = filepath.Rel(*merger, p)
			if lastp == "" || len(p) >= len(lastp) {
				lastp = p
				return p
			}
			n := len(lastp) - len(p)
			lastp = p
			return p + strings.Repeat(" ", n)
		}
		_t := func() string {
			secs := int64(time.Now().Sub(start).Seconds())
			hrs := secs / 3600
			mins := (secs - hrs*3600) / 60
			secs = secs - hrs*3600 - mins*60
			return fmt.Sprintf("%02d:%02d:%02d", hrs, mins, secs)
		}

		arpath := filepath.Join(filepath.Dir(*merger), filepath.Base(*merger)+".arrpkg")

		fmtPrintln("Archiving:", *merger)
		fmtPrintln("Output:   ", arpath)
		count, i := 0, 0
		_, err := ar.ArchiveDir(*merger, arpath, ar.ArchiveOptions{
			OnIteratingFiles: func(path string, info os.FileInfo, err error) error {
				fmtPrintf("\r[%s] Search base: %s", _t(), _p(path))
				return nil
			},
			OnEndIterating: func(pathes []string) {
				count = len(pathes)
				fmtPrintf("\r\n[%s] Found %d files, start archiving...\n", _t(), count)
			},
			OnOpeningFile: func(path string) (*os.File, os.FileInfo, error) {
				file, err := os.Open(path)
				if err != nil {
					return nil, nil, err
				}
				st, err := os.Stat(path)
				if err != nil {
					return nil, nil, err
				}
				i++
				fmtPrintf("\r[%s] [%02d%%] [%10s] %s", _t(), (i * 100 / count), humansize(st.Size()), _p(path))
				return file, st, nil
			},
		})
		if err != nil {
			fmtPrintferr("\nError: %v\n", err)
			os.Exit(1)
		}

		st, _ := os.Stat(arpath)
		size := st.Size()
		fmtPrintln("\nFinished in", time.Now().Sub(start).Nanoseconds()/1e6, "ms, size:", size, "bytes /", humansize(size))
		return
	}

	if *splitter != "" {
		ar, err := ar.OpenArchive(*splitter, false)
		if err != nil {
			fmtPrintferr("Error: %v\n", err)
			os.Exit(1)
		}

		const tf = "2006-01-02 15:04:05"
		fmtPrintf("Mode      Modtime (UTC)           Offset       Size\n\n")
		ar.Iterate(func(path string, mode uint32, modtime time.Time, start, l uint64) error {
			fmtPrintf("%s %s %10x %10d %s\n", uint16mod(uint16(mode)),
				modtime.Format(tf), start, l, path,
			)
			return nil
		})

		fmtPrintln("\nTotal files:", ar.TotalFiles(), ", created at:", ar.Created.Format(tf))
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

		mergepath := "./gallery/" + parts[0] + "/merge.pkg"
		fullpath := "./gallery/" + parts[0] + "/" + parts[1] + "/" + parts[2]

		if !strings.HasSuffix(mergepath, ".webp") {
			http.ServeFile(w, r, fullpath)
			return
		}
		if _, err := os.Stat(mergepath); err == nil {
			split(w, mergepath, parts[2])
		} else {
			http.ServeFile(w, r, fullpath)
		}
	})

	// log.SetFlags(log.Lshortfile | log.Ltime | log.Lmicroseconds)
	// log.Println("hello", *listen)
	// http.ListenAndServe(*listen, nil)
}
