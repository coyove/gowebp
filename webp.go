package gowebp

/*
#cgo CFLAGS: -I./libwebp-1.0.0/src
#cgo LDFLAGS: -L./libwebp-1.0.0/src -l webp -l m
#include <stdlib.h>
#include <string.h>
#include <webp/encode.h>
#include <webp/decode.h>

// use 64bit integers to avoid any potential paddings
struct Result {
	uint8_t  *output;
	uint64_t len;
	uint64_t width;
	uint64_t height;
} Result;

struct ResultYUV {
	uint8_t  *output;
	uint8_t  *u;
	uint8_t  *v;
	uint64_t width;
	uint64_t height;
	uint64_t stride;
	uint64_t uv_stride;
} ResultYUV;

struct Result* Encode(const uint8_t* rgba, int width, int height, int stride, float qf) {
	struct Result* ret = malloc(sizeof(struct Result));
	ret->len = (uint64_t)WebPEncodeRGBA(rgba, width, height, stride, qf, &ret->output);
	return ret;
}

struct Result* Decode(const uint8_t* data, size_t data_len) {
	struct Result* ret = malloc(sizeof(struct Result));
	int width, height;
	ret->output = WebPDecodeRGBA(data, data_len, &width, &height);
	ret->width = (uint64_t)width;
	ret->height = (uint64_t)height;
	ret->len = ret->width * ret->height * 4;
	return ret;
}

struct ResultYUV* DecodeYUV(const uint8_t* data, size_t data_len) {
	struct ResultYUV* ret = malloc(sizeof(struct ResultYUV));
	int width, height, stride, uv_stride;
	ret->output = WebPDecodeYUV(data, data_len, &width, &height, &ret->u, &ret->v, &stride, &uv_stride);
	ret->width = (uint64_t)width;
	ret->height = (uint64_t)height;
	ret->stride = (uint64_t)stride;
	ret->uv_stride = (uint64_t)uv_stride;
	return ret;
}

void Free(struct Result* r) {
	WebPFree(r->output);
	free(r);
}

void FreeYUV(struct ResultYUV* r) {
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
	"errors"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"reflect"
	"unsafe"
)

type result struct {
	output        uintptr
	len           uint64
	width, height uint64
}

type resultyuv struct {
	output, u, v     uintptr
	width, height    uint64
	stride, uvstride uint64
}

// EncodeLossy encodes img into webp and writes it to w
func EncodeLossy(w io.Writer, img image.Image, quality int) bool {
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

	if r.output == 0 {
		C.Free(x)
		return false
	}

	src := reflect.SliceHeader{}
	src.Data = r.output
	src.Len = int(r.len)
	src.Cap = int(r.len)
	io.Copy(w, bytes.NewReader(*(*[]byte)(unsafe.Pointer(&src))))
	C.Free(x)

	return true
}

// Decode decodes webp into image.RGBA
func Decode(webp []byte) image.Image {
	x := C.Decode((*C.uint8_t)(&webp[0]), C.size_t(len(webp)))
	r := (*result)(unsafe.Pointer(x))
	if r.output != 0 {
		src := reflect.SliceHeader{}
		src.Data = r.output
		src.Len = int(r.len)
		src.Cap = int(r.len)

		img := image.NewRGBA(image.Rect(0, 0, int(r.width), int(r.height)))
		copy(img.Pix, *(*[]byte)(unsafe.Pointer(&src)))
		C.Free(x)
		return img
	}
	C.Free(x)
	return nil
}

// DecodeYCbCr decodes webp into image.YCbCr
func DecodeYCbCr(webp []byte) image.Image {
	x := C.DecodeYUV((*C.uint8_t)(&webp[0]), C.size_t(len(webp)))
	r := (*resultyuv)(unsafe.Pointer(x))
	if r.output != 0 {
		src := reflect.SliceHeader{}
		src.Data = r.output
		src.Len = int(r.width * r.height)
		src.Cap = src.Len

		yuv := image.NewYCbCr(image.Rect(0, 0, int(r.width), int(r.height)), image.YCbCrSubsampleRatio420)

		copy(yuv.Y, *(*[]byte)(unsafe.Pointer(&src)))

		src.Data = r.v
		src.Len = int((r.width + 1) / 2 * (r.height + 1) / 2)
		src.Cap = src.Len
		copy(yuv.Cr, *(*[]byte)(unsafe.Pointer(&src)))

		src.Data = r.u
		copy(yuv.Cb, *(*[]byte)(unsafe.Pointer(&src)))

		C.FreeYUV(x)
		return yuv
	}
	C.FreeYUV(x)
	return nil
}

func IsWebPFormat(p []byte) bool {
	return int(C.IsWebp((*C.uint8_t)(&p[0]), C.size_t(len(p)))) == 1
}

// DecodeToJPEG is a more efficient way to decode webp into jpeg and directly write it into w
func DecodeToJPEG(w io.Writer, webp []byte, options *jpeg.Options) error {
	x := C.DecodeYUV((*C.uint8_t)(&webp[0]), C.size_t(len(webp)))
	r := (*resultyuv)(unsafe.Pointer(x))
	if r.output != 0 {
		yuv := &image.YCbCr{
			SubsampleRatio: image.YCbCrSubsampleRatio420,
			YStride:        int(r.width),
			CStride:        int((r.width + 1) / 2),
			Rect:           image.Rect(0, 0, int(r.width), int(r.height)),
		}

		uintptrSlice := func(data uintptr, len int) []byte {
			return *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{
				Data: data,
				Len:  len,
				Cap:  len,
			}))
		}

		yuv.Y = uintptrSlice(r.output, int(r.width*r.height))
		yuv.Cb = uintptrSlice(r.u, int((r.width+1)/2*(r.height+1)/2))
		yuv.Cr = uintptrSlice(r.v, int((r.width+1)/2*(r.height+1)/2))

		err := jpeg.Encode(w, yuv, options)

		C.FreeYUV(x)
		return err
	}
	C.FreeYUV(x)
	return errors.New("webp decode failed")
}
