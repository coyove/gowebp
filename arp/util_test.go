package arp

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/coyove/common/rand"
)

func TestHashCopy(t *testing.T) {
	src := fmt.Sprintf("%x", rand.New().Fetch(10))
	dst := &bytes.Buffer{}

	w, hash, _ := HashCopy(dst, strings.NewReader(src))
	if int(w) != len(src) {
		t.Error(w)
	}

	if fmt.Sprintf("%x", hash) != fmt.Sprintf("%x", sha256.Sum256([]byte(src))) {
		t.Error(hash)
	}

	dst.Reset()
	w, hash, _ = HashCopyN(dst, strings.NewReader(src), 10)
	if w != 10 {
		t.Error(w)
	}

	if fmt.Sprintf("%x", hash) != fmt.Sprintf("%x", sha256.Sum256([]byte(src)[:10])) {
		t.Error(hash)
	}

}
