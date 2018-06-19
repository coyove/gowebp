package main

import (
	"fmt"
	_ "image/jpeg"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	case 'w':
		const tf = "2006-01-02 15:04:05"
		a, err := OpenArchive(flags.paths[0], false)
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

			a.Iterate(func(info *EntryInfo, start, l uint64) error {
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
						info.dirstring(), uint16mod(uint16(info.Mode)),
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
						info.dirstring(), uint16mod(uint16(info.Mode)),
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
