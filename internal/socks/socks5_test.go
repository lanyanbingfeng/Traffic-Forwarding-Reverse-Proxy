package socks

import (
	"bytes"
	"testing"
)

type bufferReadWriter struct {
	r *bytes.Reader
	w bytes.Buffer
}

func (rw *bufferReadWriter) Read(p []byte) (int, error)  { return rw.r.Read(p) }
func (rw *bufferReadWriter) Write(p []byte) (int, error) { return rw.w.Write(p) }

func TestHandshakeRequiresOfferedNoAuth(t *testing.T) {
	rw := &bufferReadWriter{r: bytes.NewReader([]byte{version, 1, 0x02})}
	if err := handshake(rw); err == nil {
		t.Fatal("handshake accepted an unsupported authentication method")
	}
	if got := rw.w.Bytes(); !bytes.Equal(got, []byte{version, 0xff}) {
		t.Fatalf("reply = %v", got)
	}
}

func TestHandshakeAcceptsNoAuth(t *testing.T) {
	rw := &bufferReadWriter{r: bytes.NewReader([]byte{version, 2, 0x02, methodNoAuth})}
	if err := handshake(rw); err != nil {
		t.Fatal(err)
	}
	if got := rw.w.Bytes(); !bytes.Equal(got, []byte{version, methodNoAuth}) {
		t.Fatalf("reply = %v", got)
	}
}
