package flclash

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// EnsureCertificate 复用已有证书，或生成包含 tunnel.local SAN 的十年期证书。
// 返回 Mihomo fingerprint 字段使用的 SHA-256 指纹。
func EnsureCertificate(certFile, keyFile string) (string, error) {
	certExists := fileExists(certFile)
	keyExists := fileExists(keyFile)
	if certExists != keyExists {
		return "", fmt.Errorf("flclash: 证书和私钥必须同时存在或同时不存在")
	}
	if !certExists {
		if err := generateCertificate(certFile, keyFile); err != nil {
			return "", err
		}
	}
	data, err := os.ReadFile(certFile)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", fmt.Errorf("flclash: %s 不是有效的 PEM 证书", certFile)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}
	if err := cert.VerifyHostname(serverName); err != nil {
		return "", fmt.Errorf("flclash: 证书不包含 %s SAN: %w", serverName, err)
	}
	if _, err := os.Stat(keyFile); err != nil {
		return "", err
	}
	return formatFingerprint(cert.Raw), nil
}

func generateCertificate(certFile, keyFile string) error {
	if filepath.Clean(certFile) == filepath.Clean(keyFile) {
		return fmt.Errorf("flclash: cert_file 和 key_file 不能相同")
	}
	if err := os.MkdirAll(filepath.Dir(certFile), 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(keyFile), 0700); err != nil {
		return err
	}
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return err
	}
	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: serverName, Organization: []string{"TunnelProxy"}},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.AddDate(10, 0, 0),
		DNSNames:     []string{serverName},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return err
	}
	if err := writePrivateFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})); err != nil {
		return err
	}
	if err := writePrivateFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})); err != nil {
		return err
	}
	return nil
}

func writePrivateFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func formatFingerprint(raw []byte) string {
	sum := sha256.Sum256(raw)
	hexValue := strings.ToUpper(hex.EncodeToString(sum[:]))
	parts := make([]string, 0, len(hexValue)/2)
	for i := 0; i < len(hexValue); i += 2 {
		parts = append(parts, hexValue[i:i+2])
	}
	return strings.Join(parts, ":")
}
