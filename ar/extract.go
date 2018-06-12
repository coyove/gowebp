package ar

import (
	"os"
	"path/filepath"
	"sort"
)

type ExtractOptions struct {
	OnBeforeExtractingEntry func(info *EntryInfo)
}

// func makeDirForFile(filename string) {
// 	dir := filepath.Dir(filename)
// 	os.MkdirAll(dir, )
// }

// Extract extracts the archive to the given path
// if the path doesn't exist, it will be created first
func (a *Archive) Extract(path string, options ExtractOptions) (int, error) {

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
		if options.OnBeforeExtractingEntry != nil {
			options.OnBeforeExtractingEntry(dir)
		}
		p := filepath.Join(path, dir.Path)
		if err := os.MkdirAll(p, os.FileMode(dir.Mode)); err != nil {
			return 0, err
		}
	}

	for _, fi := range a.pathhash {
		if fi.IsDir {
			continue
		}
		if options.OnBeforeExtractingEntry != nil {
			options.OnBeforeExtractingEntry(fi)
		}
		p := filepath.Join(path, fi.Path)
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE, os.FileMode(fi.Mode))
		if err != nil {
			return 0, err
		}
		if _, err := a.Stream(f, fi.Path); err != nil {
			return 0, err
		}
		f.Close()
	}

	return 0, nil
}
