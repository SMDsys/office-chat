package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// loadOrCreateTLSCert загружает сертификат и ключ. Если файлов нет или
// сертификат скоро истечёт, создаёт самоподписанный сертификат для
// localhost и всех текущих IP-адресов компьютера.
func loadOrCreateTLSCert(dataDir, certPath, keyPath string) (tls.Certificate, string, error) {
	if certPath == "" || keyPath == "" {
		certPath = filepath.Join(dataDir, "tls", "cert.pem")
		keyPath = filepath.Join(dataDir, "tls", "key.pem")
	}
	if c, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil && certUsable(c) {
		return c, certPath, nil
	}
	if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
		return tls.Certificate{}, "", err
	}
	certPEM, keyPEM, err := selfSignedPEM()
	if err != nil {
		return tls.Certificate{}, "", err
	}
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return tls.Certificate{}, "", err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, "", err
	}
	c, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	return c, certPath, nil
}

func certUsable(c tls.Certificate) bool {
	if len(c.Certificate) == 0 {
		return false
	}
	leaf, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil {
		return false
	}
	if !time.Now().Add(30 * 24 * time.Hour).Before(leaf.NotAfter) {
		return false
	}
	// Адреса компьютера могли измениться (DHCP): тогда сертификат нужно
	// пересоздать, иначе браузер ругается на несовпадение имени.
	dns, ips := certHosts()
	for _, h := range dns {
		if leaf.VerifyHostname(h) != nil {
			return false
		}
	}
	for _, ip := range ips {
		if leaf.VerifyHostname(ip.String()) != nil {
			return false
		}
	}
	return true
}

func certHosts() ([]string, []net.IP) {
	host, _ := os.Hostname()
	dns := []string{"localhost"}
	if host != "" && host != "localhost" {
		dns = append(dns, host)
	}
	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	for _, s := range localIPs() {
		if ip := net.ParseIP(s); ip != nil {
			ips = append(ips, ip)
		}
	}
	return dns, ips
}

func selfSignedPEM() (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		return nil, nil, err
	}
	dns, ips := certHosts()
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Офис-чат", Organization: []string{"Офис-чат"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dns,
		IPAddresses:           ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

func tlsConfig(c tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{c},
		MinVersion:   tls.VersionTLS12,
	}
}

func tlsHint(certPath string) string {
	return fmt.Sprintf("Сертификат: %s (самоподписанный, браузер покажет предупреждение один раз)", certPath)
}
