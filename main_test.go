package main

import (
	_ "image/jpeg"
	"log"
	"net/http"
	"sync"
	"testing"
)

func Test_main(t *testing.T) {
	get := func(w *sync.WaitGroup) {
		defer w.Done()
		resp, err := http.Get("http://127.0.0.1:8888/100.webp")
		if err != nil {

			return
		}
		defer resp.Body.Close()
	}

	for i := 0; i < 1000; i++ {
		w := &sync.WaitGroup{}
		for c := 0; c < 100; c++ {
			w.Add(1)
			go get(w)
		}
		w.Wait()
		log.Println(i)
	}
}
