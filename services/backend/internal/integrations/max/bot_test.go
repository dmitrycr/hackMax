package max

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	maxbot "github.com/max-messenger/max-bot-api-client-go"
	"github.com/max-messenger/max-bot-api-client-go/schemes"
)

type fakeHTTP func(*http.Request) (*http.Response, error)

func (f fakeHTTP) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestEchoSendsOriginalTextToIncomingChat(t *testing.T) {
	const text = "  Привет!\nВторая строка <текст>  "
	calls := 0
	api, err := maxbot.New("test-token", maxbot.WithHTTPClient(fakeHTTP(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/messages" || r.URL.Query().Get("chat_id") != "123" {
			t.Fatalf("unexpected destination: %s %s", r.Method, r.URL)
		}
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Text != "Эхо: "+text {
			t.Fatalf("echo changed incoming text: %q", body.Text)
		}
		if _, ok := r.Context().Deadline(); !ok {
			t.Fatal("send must have a deadline")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"message":{"body":{"mid":"test"}}}`)), Header: make(http.Header)}, nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	update := &schemes.MessageCreatedUpdate{Message: schemes.Message{
		Recipient: schemes.Recipient{ChatId: 123}, Body: schemes.MessageBody{Text: text},
	}}
	if err := echo(context.Background(), api, update); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected one send, got %d", calls)
	}
}

func TestEchoIgnoresNonMessageAndBotUpdates(t *testing.T) {
	for _, update := range []schemes.UpdateInterface{
		nil,
		(*schemes.MessageCreatedUpdate)(nil),
		&schemes.MessageCreatedUpdate{Message: schemes.Message{Recipient: schemes.Recipient{ChatId: 123}, Sender: schemes.User{IsBot: true}, Body: schemes.MessageBody{Text: "echo"}}},
	} {
		// A nil client ensures these events cannot reach the sending code.
		if err := echo(context.Background(), nil, update); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRunRejectsMissingToken(t *testing.T) {
	if err := Run(context.Background(), " ", Options{}); err == nil {
		t.Fatal("missing token must fail before network access")
	}
}

func TestEchoWithoutChatIDRepliesToSenderAndHandlesAttachment(t *testing.T) {
	calls := 0
	api, err := maxbot.New("test-token", maxbot.WithHTTPClient(fakeHTTP(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("user_id") != "456" || r.URL.Query().Get("chat_id") != "" {
			t.Fatal("reply must be addressed to sender, not an empty chat")
		}
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Text != "Эхо: [сообщение без текста или вложение]" {
			t.Fatalf("unexpected reply %q", body.Text)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"message":{}}`)), Header: make(http.Header)}, nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	update := &schemes.MessageCreatedUpdate{Message: schemes.Message{Sender: schemes.User{UserId: 456}}}
	if err := echo(context.Background(), api, update); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected one send, got %d", calls)
	}
	update.Message.Sender.UserId = 0
	if err := echo(context.Background(), api, update); err == nil {
		t.Fatal("missing recipient must fail")
	}
	if calls != 1 {
		t.Fatal("must not send without recipient")
	}
}

func TestTLSWorkaroundIsOptInAndDoesNotMutateDefaultTransport(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	base := http.DefaultTransport.(*http.Transport)
	insecureBefore := base.TLSClientConfig != nil && base.TLSClientConfig.InsecureSkipVerify
	for _, skip := range []bool{false, true} {
		client, err := newHTTPClient(Options{InsecureSkipVerify: skip})
		if err != nil {
			t.Fatal(err)
		}
		defer client.CloseIdleConnections()
		if client.Transport == base {
			t.Fatal("MAX must use a cloned transport")
		}
		if client.Transport.(*http.Transport).TLSClientConfig == base.TLSClientConfig {
			t.Fatal("MAX must not share TLS configuration with the default transport")
		}
		resp, err := client.Get(server.URL)
		if resp != nil {
			resp.Body.Close()
		}
		if skip && err != nil {
			t.Fatalf("workaround must allow test certificate: %v", err)
		}
		if !skip && err == nil {
			t.Fatal("verification must reject untrusted certificate by default")
		}
	}
	// Transport.Clone may initialize standard HTTP/2 settings on the base.
	// The security setting, however, must remain unchanged.
	insecureAfter := base.TLSClientConfig != nil && base.TLSClientConfig.InsecureSkipVerify
	if insecureAfter != insecureBefore {
		t.Fatal("global TLS configuration was changed")
	}
}

func TestEmbeddedRootCertificate(t *testing.T) {
	block, rest := pem.Decode(russianRootCA)
	if block == nil || block.Type != "CERTIFICATE" || strings.TrimSpace(string(rest)) != "" {
		t.Fatal("expected exactly one PEM certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !cert.IsCA || cert.Subject.CommonName != "Russian Trusted Root CA" || cert.CheckSignatureFrom(cert) != nil {
		t.Fatal("expected self-signed Russian Trusted Root CA")
	}
	if now := time.Now(); now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		t.Fatal("embedded CA is outside its validity period; review the official replacement")
	}
	if fmt.Sprintf("%x", sha256.Sum256(cert.Raw)) != "d26d2d0231b7c39f92cc738512ba54103519e4405d68b5bd703e9788ca8ecf31" {
		t.Fatal("CA fingerprint changed; review its source and update the recorded fingerprint")
	}
	client, err := newHTTPClient(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	config := client.Transport.(*http.Transport).TLSClientConfig
	if config.InsecureSkipVerify {
		t.Fatal("TLS verification must be enabled by default")
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: config.RootCAs}); err != nil {
		t.Fatalf("embedded root is not in the MAX trust pool: %v", err)
	}
}

func TestAddedCAEnablesTLSButStillVerifiesHostname(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := newHTTPClientWithCA(Options{}, ca)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("trusted certificate should succeed with verification enabled: %v", err)
	}
	resp.Body.Close()
	if len(resp.TLS.VerifiedChains) == 0 {
		t.Fatal("certificate chain was not verified")
	}
	wrongHost, err := newHTTPClientWithCA(Options{}, ca)
	if err != nil {
		t.Fatal(err)
	}
	defer wrongHost.CloseIdleConnections()
	wrongHost.Transport.(*http.Transport).TLSClientConfig.ServerName = "wrong-host.invalid"
	if resp, err := wrongHost.Get(server.URL); err == nil {
		resp.Body.Close()
		t.Fatal("trusted CA must not bypass hostname validation")
	}
	if _, err := newHTTPClientWithCA(Options{}, []byte("not a certificate")); err == nil {
		t.Fatal("invalid certificate must not silently fall back")
	}
}

// Opt-in network check. Sends no token, reads no .env and never polls updates.
func TestMAXLiveTLS(t *testing.T) {
	if os.Getenv("HACKMAX_TEST_MAX_TLS") != "1" {
		t.Skip("set HACKMAX_TEST_MAX_TLS=1 for unauthenticated HTTPS check")
	}
	client, err := newHTTPClient(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, maxbot.DefaultAPIURL+"me", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.TLS == nil || len(resp.TLS.VerifiedChains) == 0 {
		t.Fatal("server TLS chain was not verified")
	}
	t.Logf("MAX HTTPS: verified chain and hostname; HTTP %d (no token sent)", resp.StatusCode)
}
