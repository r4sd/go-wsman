package wsman

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testCA はテスト用の自己署名 CA とクライアント/サーバー証明書を保持する。
type testCA struct {
	caCert     *x509.Certificate
	caKey      *ecdsa.PrivateKey
	caCertPEM  []byte
	caPool     *x509.CertPool
	serverCert tls.Certificate
	clientCert tls.Certificate
}

// newTestCA はテスト用の CA、サーバー証明書、クライアント証明書を動的生成する。
func newTestCA(t *testing.T) *testCA {
	t.Helper()

	// CA キーペア生成
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("CA キー生成に失敗: %v", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "Test CA",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(1 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("CA 証明書の作成に失敗: %v", err)
	}

	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		t.Fatalf("CA 証明書のパースに失敗: %v", err)
	}

	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})

	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)

	// サーバー証明書生成
	serverCert := generateCert(t, caCert, caKey, "127.0.0.1", false)

	// クライアント証明書生成
	clientCert := generateCert(t, caCert, caKey, "test-client", true)

	return &testCA{
		caCert:     caCert,
		caKey:      caKey,
		caCertPEM:  caCertPEM,
		caPool:     caPool,
		serverCert: serverCert,
		clientCert: clientCert,
	}
}

// generateCert は CA で署名された証明書と秘密鍵のペアを生成する。
func generateCert(t *testing.T, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, cn string, isClient bool) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("キー生成に失敗 (%s): %v", cn, err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName: cn,
		},
		NotBefore: time.Now().Add(-1 * time.Hour),
		NotAfter:  time.Now().Add(1 * time.Hour),
	}

	if isClient {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
		template.KeyUsage = x509.KeyUsageDigitalSignature
	} else {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		template.KeyUsage = x509.KeyUsageDigitalSignature
		template.IPAddresses = []net.IP{net.IPv4(127, 0, 0, 1)}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("証明書の作成に失敗 (%s): %v", cn, err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("秘密鍵のマーシャルに失敗 (%s): %v", cn, err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("tls.X509KeyPair に失敗 (%s): %v", cn, err)
	}

	return tlsCert
}

// writePEMFiles は証明書と秘密鍵を一時ファイルに書き出す。
func writePEMFiles(t *testing.T, cert tls.Certificate) (certFile, keyFile string) {
	t.Helper()

	dir := t.TempDir()

	// 証明書 PEM
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})
	certFile = filepath.Join(dir, "client.crt")
	if err := os.WriteFile(certFile, certPEM, 0600); err != nil {
		t.Fatalf("証明書ファイルの書き込みに失敗: %v", err)
	}

	// 秘密鍵 PEM
	keyDER, err := x509.MarshalECPrivateKey(cert.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		t.Fatalf("秘密鍵のマーシャルに失敗: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	keyFile = filepath.Join(dir, "client.key")
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatalf("秘密鍵ファイルの書き込みに失敗: %v", err)
	}

	return certFile, keyFile
}

// writeCAFile は CA 証明書を一時ファイルに書き出す。
func writeCAFile(t *testing.T, caPEM []byte) string {
	t.Helper()

	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(caFile, caPEM, 0600); err != nil {
		t.Fatalf("CA ファイルの書き込みに失敗: %v", err)
	}

	return caFile
}

// newMutualTLSServer はクライアント証明書認証を要求するテスト用 HTTPS サーバーを作成する。
func newMutualTLSServer(t *testing.T, ca *testCA, handler http.Handler) *httptest.Server {
	t.Helper()

	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{ca.serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    ca.caPool,
	}
	server.StartTLS()

	return server
}

func TestWithCertificate_ConfiguresTransport(t *testing.T) {
	ca := newTestCA(t)

	t.Run("WithCertificate でファイルパスからクライアントが作成される", func(t *testing.T) {
		certFile, keyFile := writePEMFiles(t, ca.clientCert)
		caFile := writeCAFile(t, ca.caCertPEM)

		client, err := NewClient("https://host:5986/wsman",
			WithCertificate(certFile, keyFile),
			WithCACert(caFile),
		)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		if client == nil {
			t.Fatal("client is nil")
		}
	})

	t.Run("WithCertificateConfig で tls.Certificate を直接渡してクライアントが作成される", func(t *testing.T) {
		client, err := NewClient("https://host:5986/wsman",
			WithCertificateConfig(ca.clientCert),
		)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		if client == nil {
			t.Fatal("client is nil")
		}
	})

	t.Run("WithCACert で CA 証明書ファイルからクライアントが作成される", func(t *testing.T) {
		caFile := writeCAFile(t, ca.caCertPEM)

		client, err := NewClient("https://host:5986/wsman",
			WithCACert(caFile),
		)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		if client == nil {
			t.Fatal("client is nil")
		}
	})
}

func TestWithCertificate_ErrorCases(t *testing.T) {
	t.Run("存在しない証明書ファイルでエラーが返る", func(t *testing.T) {
		_, err := NewClient("https://host:5986/wsman",
			WithCertificate("/nonexistent/client.crt", "/nonexistent/client.key"),
		)
		if err == nil {
			t.Fatal("存在しないファイルでエラーが返されなかった")
		}
	})

	t.Run("存在しない CA ファイルでエラーが返る", func(t *testing.T) {
		_, err := NewClient("https://host:5986/wsman",
			WithCACert("/nonexistent/ca.crt"),
		)
		if err == nil {
			t.Fatal("存在しない CA ファイルでエラーが返されなかった")
		}
	})

	t.Run("不正な証明書ファイルでエラーが返る", func(t *testing.T) {
		dir := t.TempDir()
		certFile := filepath.Join(dir, "bad.crt")
		keyFile := filepath.Join(dir, "bad.key")
		if err := os.WriteFile(certFile, []byte("not a cert"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(keyFile, []byte("not a key"), 0600); err != nil {
			t.Fatal(err)
		}

		_, err := NewClient("https://host:5986/wsman",
			WithCertificate(certFile, keyFile),
		)
		if err == nil {
			t.Fatal("不正なファイルでエラーが返されなかった")
		}
	})

	t.Run("不正な CA ファイルでエラーが返る", func(t *testing.T) {
		dir := t.TempDir()
		caFile := filepath.Join(dir, "bad-ca.crt")
		if err := os.WriteFile(caFile, []byte("not a ca cert"), 0600); err != nil {
			t.Fatal(err)
		}

		_, err := NewClient("https://host:5986/wsman",
			WithCACert(caFile),
		)
		if err == nil {
			t.Fatal("不正な CA ファイルでエラーが返されなかった")
		}
	})
}

func TestWithCertificate_MutualTLS(t *testing.T) {
	ca := newTestCA(t)

	t.Run("証明書認証でモックサーバーに接続できる（WithCertificateConfig + WithCACert）", func(t *testing.T) {
		responseXML := loadGolden(t, "get_response_computersystem.xml")

		server := newMutualTLSServer(t, ca, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
			_, _ = w.Write(responseXML)
		}))
		defer server.Close()

		caFile := writeCAFile(t, ca.caCertPEM)

		client, err := NewClient(server.URL,
			WithCertificateConfig(ca.clientCert),
			WithCACert(caFile),
		)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		resp, err := client.Get(context.Background(), "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}

		name := resp.Property("Name")
		if name != "SERVER01" {
			t.Errorf("Name = %q, want %q", name, "SERVER01")
		}
	})

	t.Run("証明書認証でモックサーバーに接続できる（WithCertificate ファイルパス版）", func(t *testing.T) {
		responseXML := loadGolden(t, "get_response_computersystem.xml")

		server := newMutualTLSServer(t, ca, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
			_, _ = w.Write(responseXML)
		}))
		defer server.Close()

		certFile, keyFile := writePEMFiles(t, ca.clientCert)
		caFile := writeCAFile(t, ca.caCertPEM)

		client, err := NewClient(server.URL,
			WithCertificate(certFile, keyFile),
			WithCACert(caFile),
		)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		resp, err := client.Get(context.Background(), "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}

		name := resp.Property("Name")
		if name != "SERVER01" {
			t.Errorf("Name = %q, want %q", name, "SERVER01")
		}
	})

	t.Run("クライアント証明書なしで mTLS サーバーに接続するとエラー", func(t *testing.T) {
		server := newMutualTLSServer(t, ca, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		caFile := writeCAFile(t, ca.caCertPEM)

		// CA のみ設定、クライアント証明書なし
		client, err := NewClient(server.URL,
			WithCACert(caFile),
		)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		_, err = client.Get(context.Background(), "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem")
		if err == nil {
			t.Fatal("クライアント証明書なしでエラーが返されなかった")
		}
	})
}
