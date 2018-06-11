package ar

import (
	"os"
	"path/filepath"
	"sort"
)

type ExtractOptions struct {
	OnBeforeExtractingFile func(path string)
}

// func makeDirForFile(filename string) {
// 	dir := filepath.Dir(filename)
// 	os.MkdirAll(dir, )
// }

// Extract extracts the archive to the given path
// if the path doesn't exist, it will be created first
func (a *Archive) Extract(path string, options ExtractOptions) (int, error) {
	if len(a.pathhash) == 1 {
		// if mode, _, _, ok := a.GetFileInfo("."); ok {
		// 	if options.OnBeforeExtractingFile != nil {
		// 		options.OnBeforeExtractingFile(".")
		// 	}

		// 	fn := filepath.Base(a.path)
		// 	fn = fn[:len(fn)-len(filepath.Ext(fn))]

		// 	f, err := os.OpenFile(filepath.Join(path, fn), os.O_WRONLY|os.O_CREATE, os.FileMode(mode))
		// 	if err != nil {
		// 		return 0, err
		// 	}
		// 	if _, err := a.Stream(f, "."); err != nil {
		// 		return 0, err
		// 	}
		// 	return 1, f.Close()
		// }
	}

	dirsSort := make([]*EntryInfo, 0, len(a.pathhash)/2)
	for _, fi := range a.pathhash {
		if !fi.IsDir {
			continue
		}
		dirsSort = append(dirsSort, fi)
		fi.score = 0
		for _, ch := range fi.Path {
			if ch == '/' {
				fi.score++
			}
		}
	}

	sort.Slice(dirsSort, func(i, j int) bool { return dirsSort[i].score < dirsSort[j].score })

	for _, dir := range dirsSort {
		p := filepath.Join(path, dir.Path)
		os.MkdirAll(p, os.FileMode(dir.Mode))
	}

	return 0, nil
}
