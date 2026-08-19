package flclash

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureCertificatePersistsAndContainsSAN(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "server.crt")
	keyFile := filepath.Join(dir, "server.key")
	first, err := EnsureCertificate(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EnsureCertificate(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(strings.Split(first, ":")) != 32 {
		t.Fatalf("unexpected fingerprints: %q %q", first, second)
	}
	data, err := os.ReadFile(certFile)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(data)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := cert.VerifyHostname(serverName); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigDefaultsAndRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.json")
	if err := os.WriteFile(path, []byte(`{"username":"u","password":"long-password"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "0.0.0.0:53" || cfg.MaxConnections != 512 || cfg.idleTimeout != 10*time.Minute {
		t.Fatalf("defaults not applied: %#v", cfg)
	}
	if err := os.WriteFile(path, []byte(`{"username":"u","password":"long-password","unknown":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("unknown config field was accepted")
	}
	if err := os.WriteFile(path, []byte(`{"username":"u","password":"long-password"} {}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("trailing JSON value was accepted")
	}
	if err := os.WriteFile(path, []byte(`{"username":"u","password":"short"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("short password was accepted")
	}
}

func TestSOCKS5OverTLSEndToEnd(t *testing.T) {
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echoListener.Close()
	go func() {
		conn, err := echoListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(conn, conn)
	}()

	dir := t.TempDir()
	certFile := filepath.Join(dir, "server.crt")
	keyFile := filepath.Join(dir, "server.key")
	if _, err := EnsureCertificate(certFile, keyFile); err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	serverRaw, clientRaw := net.Pipe()
	serverTLS := tls.Server(serverRaw, &tls.Config{Certificates: []tls.Certificate{certificate}})
	clientTLS := tls.Client(clientRaw, &tls.Config{InsecureSkipVerify: true}) // test-only self-signed certificate
	cfg := Config{Username: "flclash", Password: "secret-enough", handshakeTimeout: 2 * time.Second, idleTimeout: 2 * time.Second}
	go handleConnection(serverTLS, cfg)
	defer clientTLS.Close()
	if err := clientTLS.Handshake(); err != nil {
		t.Fatal(err)
	}

	writeAndReadExact(t, clientTLS, []byte{0x05, 0x01, 0x02}, []byte{0x05, 0x02})
	auth := append([]byte{0x01, byte(len(cfg.Username))}, []byte(cfg.Username)...)
	auth = append(auth, byte(len(cfg.Password)))
	auth = append(auth, []byte(cfg.Password)...)
	writeAndReadExact(t, clientTLS, auth, []byte{0x01, 0x00})

	target := echoListener.Addr().(*net.TCPAddr)
	request := []byte{0x05, 0x01, 0x00, 0x01}
	request = append(request, target.IP.To4()...)
	var port [2]byte
	binary.BigEndian.PutUint16(port[:], uint16(target.Port))
	request = append(request, port[:]...)
	if _, err := clientTLS.Write(request); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(clientTLS, reply); err != nil {
		t.Fatal(err)
	}
	if reply[1] != replySucceeded {
		t.Fatalf("connect reply = %v", reply)
	}
	writeAndReadExact(t, clientTLS, []byte("hello"), []byte("hello"))
}

func TestUDPAssociateIsRejected(t *testing.T) {
	request := []byte{0x05, commandUDPAssociate, 0x00, 0x01, 127, 0, 0, 1, 0, 53}
	_, code, err := readConnectRequest(bytes.NewReader(request))
	if err == nil || code != replyCommandNotSupported {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestConnectRequestSupportsDomainAndIPv6(t *testing.T) {
	domainRequest := []byte{0x05, commandConnect, 0x00, 0x03, 11}
	domainRequest = append(domainRequest, []byte("example.com")...)
	domainRequest = append(domainRequest, 0x01, 0xbb)
	addr, code, err := readConnectRequest(bytes.NewReader(domainRequest))
	if err != nil || code != replySucceeded || addr != "example.com:443" {
		t.Fatalf("domain addr=%q code=%d err=%v", addr, code, err)
	}

	ip := net.ParseIP("2001:db8::1").To16()
	ipv6Request := []byte{0x05, commandConnect, 0x00, 0x04}
	ipv6Request = append(ipv6Request, ip...)
	ipv6Request = append(ipv6Request, 0x00, 0x50)
	addr, code, err = readConnectRequest(bytes.NewReader(ipv6Request))
	if err != nil || code != replySucceeded || addr != "[2001:db8::1]:80" {
		t.Fatalf("IPv6 addr=%q code=%d err=%v", addr, code, err)
	}
}

func TestConnectRequestRejectsDomainControlCharacters(t *testing.T) {
	request := []byte{0x05, commandConnect, 0x00, 0x03, 12}
	request = append(request, []byte("example.com\n")...)
	request = append(request, 0x01, 0xbb)
	if _, _, err := readConnectRequest(bytes.NewReader(request)); err == nil {
		t.Fatal("domain containing a newline was accepted")
	}
}

func TestAuthenticationRejectsWrongPasswordAndNoAuth(t *testing.T) {
	wrongPassword := append([]byte{0x05, 0x01, methodUserPassword, 0x01, 0x01, 'u', 0x05}, []byte("wrong")...)
	rw := &scriptedReadWriter{reader: bytes.NewReader(wrongPassword)}
	if err := authenticate(rw, "u", "right"); err == nil {
		t.Fatal("wrong password was accepted")
	}
	if !bytes.Equal(rw.written.Bytes(), []byte{0x05, methodUserPassword, 0x01, 0x01}) {
		t.Fatalf("unexpected replies: %v", rw.written.Bytes())
	}

	noAuth := &scriptedReadWriter{reader: bytes.NewReader([]byte{0x05, 0x01, 0x00})}
	if err := authenticate(noAuth, "u", "right"); err == nil {
		t.Fatal("no-auth method was accepted")
	}
	if !bytes.Equal(noAuth.written.Bytes(), []byte{0x05, methodNotAcceptable}) {
		t.Fatalf("unexpected no-auth reply: %v", noAuth.written.Bytes())
	}
}

type scriptedReadWriter struct {
	reader  *bytes.Reader
	written bytes.Buffer
}

func (rw *scriptedReadWriter) Read(p []byte) (int, error)  { return rw.reader.Read(p) }
func (rw *scriptedReadWriter) Write(p []byte) (int, error) { return rw.written.Write(p) }

func writeAndReadExact(t *testing.T, conn net.Conn, request, expected []byte) {
	t.Helper()
	if _, err := conn.Write(request); err != nil {
		t.Fatal(err)
	}
	actual := make([]byte, len(expected))
	if _, err := io.ReadFull(conn, actual); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("reply=%v want=%v", actual, expected)
	}
}
