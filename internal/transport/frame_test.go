package transport

import (
	"bufio"
	"bytes"
	"testing"
)

type shortWriter struct{ bytes.Buffer }

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > 3 {
		p = p[:3]
	}
	return w.Buffer.Write(p)
}

func TestEncryptedFrameRoundTripWithShortWrites(t *testing.T) {
	c := NewCipher("test-password")
	w := &shortWriter{}
	want := Frame{Type: FrameSocks, ConnID: 42, Payload: []byte("hello tunnel")}
	if err := WriteFrame(w, c, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(bufio.NewReader(bytes.NewReader(w.Bytes())), c)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != want.Type || got.ConnID != want.ConnID || !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestEncryptedFrameRejectsTampering(t *testing.T) {
	c := NewCipher("test-password")
	var b bytes.Buffer
	if err := WriteFrame(&b, c, Frame{Type: FrameSocks, ConnID: 1, Payload: []byte("secret")}); err != nil {
		t.Fatal(err)
	}
	data := b.Bytes()
	data[len(data)-1] ^= 0xff
	if _, err := ReadFrame(bufio.NewReader(bytes.NewReader(data)), c); err == nil {
		t.Fatal("tampered encrypted frame was accepted")
	}
}
