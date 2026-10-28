package email

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/textproto"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type testSMTPServer struct {
	addr     string
	pool     *x509.CertPool
	messages chan string
	mu       sync.Mutex
	failRcpt bool
}

func startTestSMTPServer(t *testing.T) *testSMTPServer {
	t.Helper()
	cert, pool := testCertificate(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &testSMTPServer{addr: listener.Addr().String(), pool: pool, messages: make(chan string, 8)}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go server.serve(conn, cert)
		}
	}()
	return server
}

func testCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)
	return cert, pool
}

func (s *testSMTPServer) serve(conn net.Conn, cert tls.Certificate) {
	defer conn.Close()
	tc := textproto.NewConn(conn)
	_ = tc.PrintfLine("220 test ESMTP")
	secured := false
	for {
		line, err := tc.ReadLine()
		if err != nil {
			return
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "EHLO", "HELO":
			_ = tc.PrintfLine("250-test")
			if !secured {
				_ = tc.PrintfLine("250-STARTTLS")
			} else {
				_ = tc.PrintfLine("250-AUTH PLAIN")
			}
			_ = tc.PrintfLine("250 OK")
		case "STARTTLS":
			_ = tc.PrintfLine("220 Ready")
			tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn = tlsConn
			tc = textproto.NewConn(conn)
			secured = true
		case "AUTH":
			_ = tc.PrintfLine("235 Authentication successful")
		case "MAIL":
			_ = tc.PrintfLine("250 Sender accepted")
		case "RCPT":
			s.mu.Lock()
			fail := s.failRcpt
			s.mu.Unlock()
			if fail {
				_ = tc.PrintfLine("550 Recipient rejected")
				continue
			}
			_ = tc.PrintfLine("250 Recipient accepted")
		case "DATA":
			_ = tc.PrintfLine("354 End data with <CR><LF>.<CR><LF>")
			data, err := tc.ReadDotBytes()
			if err != nil {
				return
			}
			select {
			case s.messages <- string(data):
			default:
			}
			_ = tc.PrintfLine("250 Queued")
		case "QUIT":
			_ = tc.PrintfLine("221 Bye")
			return
		default:
			_ = tc.PrintfLine("250 OK")
		}
	}
}

func TestSMTPSenderRequiresSTARTTLSAndDelivers(t *testing.T) {
	server := startTestSMTPServer(t)
	sender, err := NewSMTPSender(SMTPConfig{
		Addr: server.addr, Host: "localhost", From: "noreply@example.test",
		TLS: &tls.Config{RootCAs: server.pool, ServerName: "localhost", MinVersion: tls.VersionTLS12},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = sender.Send(context.Background(), Message{
		To: "user@example.test", Subject: "Reset your password",
		Body:      "Open https://app.example.test/reset-password?token=secret-token",
		MessageID: "<stable@example.test>",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	select {
	case message := <-server.messages:
		if !strings.Contains(message, "Subject: Reset your password") || !strings.Contains(message, "token=secret-token") {
			t.Fatalf("message = %s", message)
		}
		if !strings.Contains(message, "Message-ID: <stable@example.test>") {
			t.Fatalf("missing stable message id: %s", message)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SMTP server received no message")
	}
}

func TestRenderMultipartEmail(t *testing.T) {
	wire := render("noreply@example.test", Message{
		To: "user@example.test", Subject: "Reset", Body: "Plain fallback <safe>",
		HTMLBody: "<html><body><a href=\"https://example.test/reset?token=abc\">Reset</a></body></html>",
	})
	message, err := mail.ReadMessage(bytes.NewReader(wire))
	if err != nil {
		t.Fatal(err)
	}
	mediaType, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("Content-Type = %q: %v", mediaType, err)
	}
	parts := multipart.NewReader(message.Body, params["boundary"])
	for i, kind := range []string{"text/plain", "text/html"} {
		part, err := parts.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		partType, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil || partType != kind {
			t.Fatalf("part %d type = %q: %v", i, partType, err)
		}
		body, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 && !strings.Contains(string(body), "Plain fallback") || i == 1 && !strings.Contains(string(body), "token=abc") {
			t.Fatalf("part %d content unexpected", i)
		}
	}
	if _, err := parts.NextPart(); err != io.EOF {
		t.Fatalf("extra MIME part: %v", err)
	}
}

func TestSMTPSenderRejectsHeaderInjection(t *testing.T) {
	sender, err := NewSMTPSender(SMTPConfig{Addr: "127.0.0.1:1", Host: "localhost", From: "noreply@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	err = sender.Send(context.Background(), Message{To: "user@example.test\r\nBcc: victim@example.test", Subject: "x"})
	if err == nil || !strings.Contains(err.Error(), "newline") {
		t.Fatalf("header injection = %v", err)
	}
}

// TestSMTPSenderLiveConnects is opt-in and verifies real relay credentials
// without sending a message. It is skipped unless SMTP_LIVE_TEST=1.
func TestSMTPSenderLiveConnects(t *testing.T) {
	if os.Getenv("SMTP_LIVE_TEST") != "1" {
		t.Skip("SMTP_LIVE_TEST is not enabled")
	}
	mode := os.Getenv("SMTP_TLS_MODE")
	sender, err := NewSMTPSender(SMTPConfig{
		Addr:        os.Getenv("SMTP_ADDR"),
		Host:        os.Getenv("SMTP_HOST"),
		Username:    os.Getenv("SMTP_USERNAME"),
		Password:    os.Getenv("SMTP_PASSWORD"),
		From:        os.Getenv("SMTP_FROM"),
		ImplicitTLS: mode == "implicit" || mode == "",
		Timeout:     15 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Ping(context.Background()); err != nil {
		t.Fatalf("live SMTP ping: %v", err)
	}
}
