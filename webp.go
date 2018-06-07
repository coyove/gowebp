package gowebp

/*
#cgo CFLAGS: -I./libwebp-1.0.0/src
#cgo LDFLAGS: -L./libwebp-1.0.0/src -l webp -l m
#include <stdlib.h>
#include <string.h>
#include <webp/encode.h>
#include <webp/decode.h>

struct Result {
	uint8_t  *output;
	uint32_t len;
} Result;

struct Result* Encode(const uint8_t* rgba, int width, int height, int stride, float qf) {
	struct Result* ret = malloc(sizeof(struct Result));
	ret->len = (uint32_t)WebPEncodeRGBA(rgba, width, height, stride, qf, &ret->output);
	return ret;
}

void Free(struct Result* r) {
	WebPFree(r->output);
	free(r);
}

int IsWebp(const uint8_t* data, size_t data_size) {
	return WebPGetInfo(data, data_size, NULL, NULL);
}

*/
import "C"

import (
	"bytes"
	"image"
	"image/draw"
	"io"
	"reflect"
	"unsafe"
)

type result struct {
	output uintptr
	len    uint32
}

func encodeWebp(w io.Writer, img image.Image, quality int) {
	width := img.Bounds().Dx()
	height := img.Bounds().Dy()

	if _, ok := img.(*image.RGBA); !ok {
		canvas := image.NewRGBA(img.Bounds())
		draw.Draw(canvas, canvas.Bounds(), img, image.ZP, draw.Src)
		img = canvas
	}

	p := img.(*image.RGBA)
	x := C.Encode((*C.uint8_t)(&p.Pix[0]), C.int(width), C.int(height), C.int(p.Stride), C.float(quality))
	r := (*result)(unsafe.Pointer(x))
	src := reflect.SliceHeader{}
	src.Data = r.output
	src.Len = int(r.len)
	src.Cap = int(r.len)
	io.Copy(w, bytes.NewReader(*(*[]byte)(unsafe.Pointer(&src))))
	C.Free(x)
}

func isValidWebp(p []byte) bool {
	return int(C.IsWebp((*C.uint8_t)(&p[0]), C.size_t(len(p)))) == 1
}
