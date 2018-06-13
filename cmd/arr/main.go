package main

import (
	"bytes"
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
)

// var merger = flag.String("a", "", "directory to archive")
// var verbose = flag.Bool("v", false, "verbose output")
// var checksum = flag.Bool("checksum", false, "verify the sha256 of every file in the archive")
// var webserver = flag.String("w", "", "serve arrpkg over HTTP")
// var deloriginal = flag.Bool("xx", false, "delete original files after archiving")
// var splitter = flag.String("l", "", "list archive content")
// var extractor = flag.String("x", "", "extract the archive")

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

func doArchive(path string, x bool) {
	start := time.Now()
	lastp := ""
	_p := func(p string) string {
		p, _ = filepath.Rel(path, p)
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

	if !x {
		arpath := filepath.Join(filepath.Dir(path), filepath.Base(path)+".arrpkg")

		fmtPrintln("Archiving:", path)
		fmtPrintln("Output:   ", arpath)
		count, i := 0, 0
		_, err := ArchiveDir(path, arpath, ArchiveOptions{
			DelOriginal:    flags.deloriginal,
			DelImmediately: flags.delimm,
			OnIteratingFiles: func(path string, info os.FileInfo, err error) error {
				fmtPrintf("\r[%s] Search base: %s", _t(), _p(path))
				return nil
			},
			OnEndIterating: func(c int) {
				count = c
				fmtPrintf("\r\n[%s] Found %d files, start archiving...\n", _t(), c)
			},
			OnOpeningFile: func(path string) (*os.File, os.FileInfo, error) {
				st, err := os.Stat(path)
				if err != nil {
					return nil, nil, err
				}
				i++
				if st.IsDir() {
					fmtPrintf("\r[%s] [%02d%%] [%10s] %s", _t(), (i * 100 / count), "   Fdir   ", _p(path))
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
	} else {
		a, err := OpenArchive(path, false)
		if err != nil {
			fmtPrintferr("Error: %v\n", err)
			os.Exit(1)
		}

		fmtPrintln("Extract:", path, ", Size:", humansize(a.Info.Size()))
		fmtPrintln("Output :", flags.xdest)

		i, count := 0, a.TotalEntries()
		Extract(flags.xdest)

		fmtPrintln("\nFinished in", time.Now().Sub(start).Nanoseconds()/1e6, "ms")
	}
}

func doList(path string) {
	a, err := OpenArchive(path, false)
	if err != nil {
		fmtPrintferr("Error: %v\n", err)
		os.Exit(1)
	}

	const tf = "2006-01-02 15:04:05"
	if flags.checksum {
		fmtPrintf("Mode       Modtime                 Offset       Size  H\n\n")
	} else {
		fmtPrintf("Mode       Modtime                 Offset       Size\n\n")
	}

	var badFiles = 0
	a.Iterate(func(info *EntryInfo, start, l uint64) error {
		if flags.checksum {
			flag := " · "
			if !info.IsDir {
				_, err := a.Stream(ioutil.Discard, info.Path)
				if err == ErrCorruptedHash {
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
}

func main() {
	parseFlags()

	fmtPrintln("\nArr archive tool", runtime.GOOS, runtime.GOARCH, runtime.Version(), "\n")

	switch flags.action {
	case 'a':
		for _, path := range flags.paths {
			doArchive(path, false)
		}
	case 'l':
		for _, path := range flags.paths {
			doList(path)
		}
	case 'x':
		for _, path := range flags.paths {
			doArchive(path, true)
		}
	case 'w':
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			uri := r.URL.Path
			if len(uri) <= 1 {
				a, err := OpenArchive(flags.paths[0], false)
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
					`, flags.paths[0], a.TotalEntries(), a.Created.Format(tf))))

				a.Iterate(func(info *EntryInfo, start, l uint64) error {
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
			split(w, flags.paths[0], uri[1:])
		})

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			panic(err)
		}
		fmt.Println("Server started at", listener.Addr())
		http.Serve(listener, nil)
	}
}
