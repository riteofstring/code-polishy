package artifactsecurity

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var scannerTrustVerificationTime = time.Date(2026, time.October, 22, 2, 0, 0, 0, time.UTC)

func TestScannerTrustBundleOmitsDistrustedEMudhraRoots(t *testing.T) {
	distrusted := map[string]string{
		"8d417db2dd8bf5e3084d1e3f196d583849d81bdd4c00c70b9d39369e96b8c782": "emSign ECC Root CA - G3",
		"b7408b4d2be0238ba37004dd34e276c6019bd2f24c9db7d4980f5f6c359a4bcc": "emSign Root CA - C1",
		"eabc185c4e82d942b1a5978ba3c0181487d6b3b9974e5c49f72f6d0bd9637150": "emSign ECC Root CA - C3",
	}
	for _, certificate := range readCertificates(t, scannerTrustBundlePath(t)) {
		digest := sha256.Sum256(certificate.RawSubjectPublicKeyInfo)
		if name, found := distrusted[hex.EncodeToString(digest[:])]; found {
			t.Fatalf("scanner trust bundle still trusts %s", name)
		}
	}
}

func TestScannerTrustBundleRejectsTheEMudhraTLSCrossCertificate(t *testing.T) {
	cross := readCertificates(t, filepath.Join("testdata", "emsign-root-tls-ca-g3-cross.pem"))[0]
	shipped := scannerTrustPool(t)
	_, err := cross.Verify(x509.VerifyOptions{Roots: shipped, CurrentTime: scannerTrustVerificationTime, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	var unknown x509.UnknownAuthorityError
	if !errors.As(err, &unknown) {
		t.Fatalf("emSign Root TLS CA - G3 must have no trusted issuer in the scanner bundle, got %v", err)
	}
	withDistrustedRoot := scannerTrustPool(t)
	withDistrustedRoot.AddCert(readCertificates(t, filepath.Join("testdata", "emsign-ecc-root-ca-g3.pem"))[0])
	if _, err := cross.Verify(x509.VerifyOptions{Roots: withDistrustedRoot, CurrentTime: scannerTrustVerificationTime, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Fatalf("cross-certificate control must verify through emSign ECC Root CA - G3: %v", err)
	}
}

func TestScannerTrustBundleAuthenticatesDatabaseHosts(t *testing.T) {
	shipped := scannerTrustPool(t)
	for host, fixture := range map[string]string{
		"ghcr.io":                              "ghcr-io-chain.pem",
		"pkg-containers.githubusercontent.com": "pkg-containers-chain.pem",
	} {
		chain := readCertificates(t, filepath.Join("testdata", fixture))
		intermediates := x509.NewCertPool()
		for _, certificate := range chain[1:] {
			intermediates.AddCert(certificate)
		}
		if _, err := chain[0].Verify(x509.VerifyOptions{DNSName: host, Roots: shipped, Intermediates: intermediates, CurrentTime: scannerTrustVerificationTime}); err != nil {
			t.Fatalf("scanner trust bundle rejects %s: %v", host, err)
		}
	}
}

func scannerTrustBundlePath(t *testing.T) string {
	t.Helper()
	policyRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(policyRoot, "artifact-security", "scanner-ca-certificates.crt")
}

func scannerTrustPool(t *testing.T) *x509.CertPool {
	t.Helper()
	pool := x509.NewCertPool()
	for _, certificate := range readCertificates(t, scannerTrustBundlePath(t)) {
		pool.AddCert(certificate)
	}
	return pool
}

func readCertificates(t *testing.T, path string) []*x509.Certificate {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var certificates []*x509.Certificate
	for block, rest := pem.Decode(payload); block != nil; block, rest = pem.Decode(rest) {
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		certificates = append(certificates, certificate)
	}
	if len(certificates) == 0 {
		t.Fatalf("%s contains no certificates", path)
	}
	return certificates
}
