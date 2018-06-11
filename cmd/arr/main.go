package main

import (
	"bytes"
	"flag"
	"fmt"
	_ "image/jpeg"
	"io/ioutil"
	"net"
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
var checksum = flag.Bool("checksum", false, "verify the sha256 of every file in the archive")
var webserver = flag.String("w", "", "serve arrpkg over HTTP")
var deloriginal = flag.Bool("xx", false, "delete original files after archiving")
var splitter = flag.String("l", "", "list archive content")
var extractor = flag.String("x", "", "extract the archive")

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
			DelOriginal: *deloriginal,
			OnIteratingFiles: func(path string, info os.FileInfo, err error) error {
				fmtPrintf("\r[%s] Search base: %s", _t(), _p(path))
				return nil
			},
			OnEndIterating: func(paths []string) {
				count = len(paths)
				fmtPrintf("\r\n[%s] Found %d files, start archiving...\n", _t(), count)
			},
			OnOpeningFile: func(path string) (*os.File, os.FileInfo, error) {
				st, err := os.Stat(path)
				if err != nil {
					return nil, nil, err
				}
				i++
				if st.IsDir() {
					fmtPrintf("\r[%s] [%02d%%] [%10s] %s", _t(), (i * 100 / count), "directory", _p(path))
					return nil, st, nil
				}
				file, err := os.Open(path)
				if err != nil {
					return nil, nil, err
				}
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
		a, err := ar.OpenArchive(*splitter, false)
		if err != nil {
			fmtPrintferr("Error: %v\n", err)
			os.Exit(1)
		}

		const tf = "2006-01-02 15:04:05"
		if *checksum {
			fmtPrintf("Mode      Modtime                 Offset       Size  H\n\n")
		} else {
			fmtPrintf("Mode      Modtime                 Offset       Size\n\n")
		}

		var badFiles = 0
		a.Iterate(func(info *ar.EntryInfo, start, l uint64) error {
			if *checksum {
				flag := " · "
				if !info.IsDir {
					_, err := a.Stream(ioutil.Discard, info.Path)
					if err == ar.ErrCorruptedHash {
						badFiles++
						flag = " X "
					}
				}
				fmtPrintf("%s%s %s %10x %10d %s %s\n", info.Dirstring(), uint16mod(uint16(info.Mode)), info.Modtime.Format(tf), start, l, flag, info.Path)
			} else {
				fmtPrintf("%s%s %s %10x %10d %s\n", info.Dirstring(), uint16mod(uint16(info.Mode)), info.Modtime.Format(tf), start, l, info.Path)
			}
			return nil
		})

		fmtPrintln("\nTotal entries:", a.TotalEntries(), ", created at:", a.Created.Format(tf))
		if badFiles > 0 {
			fmtPrintferr("Found %d corrupted files!\n", badFiles)
		}
		return
	}

	if *extractor != "" {
		a, err := ar.OpenArchive(*extractor, false)
		if err != nil {
			fmtPrintferr("Error: %v\n", err)
			os.Exit(1)
		}
		a.Extract("D:/aaa", ar.ExtractOptions{})
		return
	}

	if *webserver != "" {
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			uri := r.URL.Path
			if len(uri) <= 1 {
				a, err := ar.OpenArchive(*webserver, false)
				if err != nil {
					fmtPrintferr("Error: %v\n", err)
					os.Exit(1)
				}

				const tf = "2006-01-02 15:04:05"

				w.Write([]byte(fmt.Sprintf(`
					<html>
					<title>%s</title>
					<style>*{font-size:12px;font-family:"Lucida Console",Monaco,monospace}td,div{padding:4px}td{white-space:nowrap;width:1px}</style>
					<div>Total entries: %d, created at: %s</div>
					<table border=1 style="border-collapse:collapse">
						<tr><td> Mode </td><td> Modtime </td><td> Offset </td><td align=right> Size </td><td></td></tr>
					`, *webserver, a.TotalEntries(), a.Created.Format(tf))))

				a.Iterate(func(info *ar.EntryInfo, start, l uint64) error {
					w.Write([]byte(fmt.Sprintf(`<tr>
						<td>%s%s</td>
						<td>%s</td>
						<td>0x%010x</td>
						<td align=right>%d</td>
						<td><a href='/%s'>%s</a></td>
					</tr>`,
						info.Dirstring(), uint16mod(uint16(info.Mode)),
						info.Modtime.Format(tf), start, l, info.Path, info.Path,
					)))
					return nil
				})

				w.Write([]byte("</table></html>"))
				a.Close()
				return
			}
			split(w, *webserver, uri[1:])
		})

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			panic(err)
		}
		fmt.Println("Server started at", listener.Addr())
		http.Serve(listener, nil)
	}
}
