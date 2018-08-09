package main

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	_ "image/jpeg"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"github.com/coyove/common/rand"
	"github.com/coyove/gowebp/arp"
)

func main() {
	parseFlags()

	fmtPrintln("\nArr archive tool", runtime.GOOS, runtime.GOARCH, runtime.Version(), "\n")

	switch flags.action {
	case 'a':
		for _, path := range flags.paths {
			arpath := filepath.Join(filepath.Dir(path), filepath.Base(path)+".arrpkg")
			ArchiveDir(path, arpath)
		}
	case 'l':
		for _, path := range flags.paths {
			Extract(path, "")
		}
	case 'x':
		for _, path := range flags.paths {
			Extract(path, flags.xdest)
		}
	case 'j':
		for _, path := range flags.paths {
			arp.DumpArchiveJmupTable(path, path+".jmp")
		}
	case 'w':
		const tf = "2006-01-02 15:04:05"
		a, err := arp.OpenArchive(flags.paths[0], false)
		if err != nil {
			fmtPrintferr("Error: %v\n", err)
			os.Exit(1)
		}

		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			uri := r.URL.Path
			if len(uri) == 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			uri = uri[1:]
			if strings.HasSuffix(uri, "/") {
				uri = uri[:len(uri)-1]
			}
			if _, _, ok := a.GetFile(uri); ok {
				a.Stream(w, uri)
				return
			}

			w.Write([]byte(fmt.Sprintf(`
					<html><meta charset="utf-8">
					<title>%s</title>
					<style>*{font-size:12px;font-family:"Lucida Console",Monaco,monospace}td,div{padding:4px}td{white-space:nowrap;width:1px}</style>
					<style>.dir{font-weight:bold}a{text-decoration:none}a:hover{text-decoration:underline}</style>
					<script>function up(){var p=location.href.split('/');p.pop();location.href=p.join('/')}</script>
					<div>Total entries: %d, created at: %s</div>
					<table border=1 style="border-collapse:collapse">
					<tr><td> Mode </td><td> Modtime </td><td> Offset </td><td align=right> Size </td><td></td></tr>
					<tr><td></td><td></td><td></td><td></td><td class=dir><a href="javascript:up()">..</a></td></tr>
					`, flags.paths[0], a.TotalEntries(), a.Created.Format(tf))))

			a.Iterate(func(info *arp.EntryInfo, start, l uint64) error {
				if !isUnder(uri, info.Path) || info.Path == "." {
					return nil
				}

				if info.IsDir {
					w.Write([]byte(fmt.Sprintf(`<tr>
							<td>%s%s</td>
							<td>%s</td>
							<td>Fdir</td>
							<td align=right>-</td>
							<td class=dir><a href='/%s'>%s</a></td>
						</tr>`,
						info.Dirstring(), uint16mod(uint16(info.Mode)),
						info.Modtime.Format(tf), info.Path, filepath.Base(info.Path),
					)))
				} else {
					w.Write([]byte(fmt.Sprintf(`<tr>
						<td>%s%s</td>
						<td>%s</td>
						<td>0x%010x</td>
						<td align=right>%d</td>
						<td><a href='/%s'>%s</a></td>
					</tr>`,
						info.Dirstring(), uint16mod(uint16(info.Mode)),
						info.Modtime.Format(tf), start, l, info.Path, filepath.Base(info.Path),
					)))
				}
				return nil
			})

			w.Write([]byte("</table></html>"))
		})

		if flags.listen == "" {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				panic(err)
			}
			fmt.Println("Server started at", listener.Addr())
			http.Serve(listener, nil)
		} else {
			fmt.Println("Server started at", flags.listen)
			http.ListenAndServe(flags.listen, nil)
		}

	case 'W':
		basepath := flags.paths[0]
		fmt.Println("Serving base:", basepath)

		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			uri := r.URL.Path
			if len(uri) < 1 {
				w.WriteHeader(400)
				return
			}

			uri = strings.Replace(uri[1:], "thumbs/", "", -1)
			parts := strings.Split(uri, "/")

			w.Header().Add("Access-Control-Allow-Origin", "*")
			mergepath := filepath.Join(basepath, parts[0]+".arrpkg")
			fullpath := filepath.Join(basepath, uri)

			if _, err := os.Stat(mergepath); err == nil {
				split(w, mergepath, strings.Join(parts[1:], "/"))
			} else {
				http.ServeFile(w, r, fullpath)
			}
		})

		fmt.Println("Server started at", flags.listen)
		http.ListenAndServe(flags.listen, nil)
	}
}

// ArchiveDir archives the given directory into an archive
// struct:
// +---------------+----------+-------+------+-------+------+-- - -
// | 24b metatable | jmptable | file1 | guid | file2 | guid | ...
// +---------------+----------+-------+------+-------+------+-- - -
// Currently there are only 3 fields in metatable:
//    (4b) Magic code, current: zzz0
// 1. (4b) Total files
// 2. (4b) Archive created time
// 3. (1b) Endianness, 1: Big endian, 0: Little endian
func ArchiveDir(dirpath, arpath string) {
	const dummy16 = "\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"
	pathbuflen, full := 0, make([]string, 0)
	o := newoneliner()

	var pathslist *os.File
	var pathslistpath string
	var totalFoundEntries int
	const fileHdr = 2 + 4 + 4 + sha256.Size

	fmtPrintln("Archiving:", dirpath)
	fmtPrintln("Output:   ", arpath)

	filepath.Walk(dirpath, func(path string, info os.FileInfo, err error) error {
		path = strings.Replace(path, "\\", "/", -1)

		if err != nil {
			return err
		}
		info2, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !os.SameFile(info, info2) {
			// the path points to a symbolic link, we don't support it
			return nil
		}
		if path == arpath {
			return nil
		}

		if len(path) > 65535 {
			fmtFatalErr(fmt.Errorf("found a path longer than 65535, you serious?"))
		}

		if pathslist != nil {
			pathslist.WriteString(path + "\n")
		} else {
			full = append(full, path)
			if len(full) > 1024 {
				pathslistpath = arpath + "." + fmt.Sprintf("%x", rand.New().Fetch(4)) + ".paths"
				pathslist, _ = os.Create(pathslistpath)
				pathslist.WriteString(strings.Join(full, "\n") + "\n")
				full = full[:0]
			}
		}
		totalFoundEntries++

		path = rel(dirpath, path)
		fmtPrintf("\r[%s] Search base: %s", o.elapsed(), o.fill(path))

		pathbuflen += len(path) + fileHdr
		// info.Mode(): uint32
		// info.ModTime(): uint32
		return nil
	})

	fmtPrintf("\r[%s] Search base: found %d files, start archiving...\n", o.elapsed(), totalFoundEntries)

	ar, err := os.Create(arpath)
	fmtFatalErr(err)

	defer ar.Close()

	p, count := [arp.MetaSize]byte{'z', 'z', 'z', '0'}, totalFoundEntries

	binary.BigEndian.PutUint32(p[4:8], uint32(count))
	binary.BigEndian.PutUint32(p[8:12], uint32(time.Now().Unix()))
	p[12] = *(*byte)(unsafe.Pointer(&arp.One))

	_, err = ar.Write(p[:])
	fmtFatalErr(err)

	headerlen := (arp.MetaSize*count + pathbuflen + 15) / 16 * 16
	// log.Println(headerlen, count, pathbuflen)
	for i := 0; i < headerlen/16; i++ {
		_, err := ar.WriteString(dummy16)
		fmtFatalErr(err)
	}

	cursor, pcursor := int64(headerlen+arp.MetaSize), int64(count)*arp.MetaSize+arp.MetaSize
	m := arp.Uint64OneTwoMap{}
	iteratePaths(full, pathslist, func(i int, path string) {
		finalpath := rel(dirpath, path)
		finalpath = strings.Replace(finalpath, "\\", "/", -1)

		var file *os.File
		var st os.FileInfo
		var err error

		st, err = os.Stat(path)
		if err != nil {
			fmtMaybeErr(path, err)
			// if users chose to ignore errors, we will still insert the entry into jmptable
			// but with arp.ErrFlag.
			m.Push(finalpath, arp.ErrFlag, arp.ErrFlag)
			return
		}
		if !st.IsDir() {
			file, err = os.Open(path)
		}
		if err != nil {
			fmtMaybeErr(path, err)
			m.Push(finalpath, arp.ErrFlag, arp.ErrFlag)
			return
		}

		fmtPrintf("\r[%s] [%02d%%] ", o.elapsed(), (i * 100 / totalFoundEntries))

		if st.IsDir() {
			fmtPrintf("[   Fdir   ] %s", o.fill(finalpath))
		} else {
			fmtPrintf("[%10s] %s", humansize(st.Size()), o.fill(finalpath))
		}

		binary.BigEndian.PutUint16(p[:2], uint16(len(finalpath)))
		binary.BigEndian.PutUint32(p[2:6], uint32(st.Mode()))
		binary.BigEndian.PutUint32(p[6:10], uint32(st.ModTime().Unix()))

		_, err = ar.WriteAt(p[:10], pcursor)
		fmtFatalErr(err)
		pcursor += 10

		if st.IsDir() {
			// for directories, they have no real contents
			// so we will use arp.DirFlag as hash to identify
			_, err = ar.WriteAt([]byte(arp.DirGUID), pcursor)
			fmtFatalErr(err)
			pcursor += sha256.Size

			_, err = ar.WriteAt([]byte(finalpath), pcursor)
			fmtFatalErr(err)

			pcursor += int64(len(finalpath))
			m.Push(finalpath, arp.DirFlag, arp.DirFlag)
			return
		}

		// append the file content to the end of the archive
		beforeAppend, err := ar.Seek(0, 1)
		fmtFatalErr(err)

		n, h, err := arp.HashCopy(ar, file)
		if err != nil {
			fmtMaybeErr(path, err)
			m.Push(finalpath, arp.ErrFlag, arp.ErrFlag)
			pcursor -= 10
			_, err = ar.Seek(beforeAppend, 0)
			fmtFatalErr(err)
			return
		}

		// write hash at pcursor
		_, err = ar.WriteAt(h, pcursor)
		fmtFatalErr(err)

		pcursor += sha256.Size
		_, err = ar.WriteAt([]byte(finalpath), pcursor)
		fmtFatalErr(err)

		pcursor += int64(len(finalpath))
		m.Push(finalpath, uint64(cursor), uint64(n))

		cursor += n
		if err := file.Close(); err != nil {
			fmtMaybeErr(path, err)
			return
		}

		if flags.deloriginal && flags.delimm && !st.IsDir() {
			os.Remove(full[i])
		}
	})

	m.Seal()
	ar.WriteAt(m.Bytes(), arp.MetaSize)

	if flags.deloriginal {
		for _, p := range full {
			os.Remove(p)
		}
	}

	if pathslist != nil {
		pathslist.Close()
		os.Remove(pathslistpath)
	}

	st, _ := os.Stat(arpath)
	size := st.Size()
	fmtPrintln("\nFinished in", o.elapsed(), ", size:", size, "bytes /", humansize(size))
}
