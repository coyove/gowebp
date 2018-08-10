package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coyove/gowebp/arp"

	"github.com/coyove/common/rand"
)

func generateRandomDirectory(root string) {
	r := rand.New()
	randomname := func() string { return fmt.Sprintf("%x", r.Fetch(r.Intn(10)+10)) }

	current := []string{root}
	createFile := func() {
		current = append(current, randomname())
		path := strings.Join(current, "/")
		f, _ := os.Create(path)

		for i := 0; i < 10; i++ {
			f.Write(r.Fetch(r.Intn(1000)))
		}
		f.Close()
		current = current[:len(current)-1]
	}

	actions := []func(){
		createFile,
		createFile,
		createFile,
		createFile,
		func() {
			current = append(current, randomname())
			path := strings.Join(current, "/")
			os.MkdirAll(path, 0777)
		},
		func() {
			if len(current) == 1 {
				return
			}
			current = current[:len(current)-1]
		},
	}

	count := r.Intn(64) + 64
	for i := 0; i < count; i++ {
		actions[r.Intn(len(actions))]()
	}
}

func TestMainFuzzy(t *testing.T) {
	const src = "=test1"
	os.RemoveAll(src)
	os.Mkdir(src, 0777)
	generateRandomDirectory(src)

	ArchiveDir(src, src+".arrpkg")
	ar, err := arp.OpenArchive(src+".arrpkg", false)
	if err != nil {
		t.Fatal(err)
	}

	defer ar.Close()
	filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}

		buf, _ := ioutil.ReadFile(path)

		path = strings.Replace(path, src, "", 1)
		if len(path) == 0 {
			return nil
		}

		path = strings.Replace(path[1:], "\\", "/", -1)

		buf2 := &bytes.Buffer{}
		w, err := ar.Stream(buf2, path)
		if err != nil {
			t.Fatal(err)
		}
		if int64(w) != info.Size() {
			t.Fatal("size not matched")
		}

		if !bytes.Equal(buf2.Bytes(), buf) {
			t.Fatal(path, "content not matched")
		}
		return nil
	})

	ar.Close()
	os.Remove(src + ".arrpkg")
	os.RemoveAll(src)
}
