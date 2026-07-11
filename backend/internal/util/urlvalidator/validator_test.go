package urlvalidator

import (
	"context"
	"net"
	"testing"
)

func TestValidateURLFormat(t *testing.T) {
	if _, err := ValidateURLFormat("", false); err == nil {
		t.Fatalf("expected empty url to fail")
	}
	if _, err := ValidateURLFormat("://bad", false); err == nil {
		t.Fatalf("expected invalid url to fail")
	}
	if _, err := ValidateURLFormat("http://example.com", false); err == nil {
		t.Fatalf("expected http to fail when allow_insecure_http is false")
	}
	if _, err := ValidateURLFormat("https://example.com", false); err != nil {
		t.Fatalf("expected https to pass, got %v", err)
	}
	if _, err := ValidateURLFormat("http://example.com", true); err != nil {
		t.Fatalf("expected http to pass when allow_insecure_http is true, got %v", err)
	}
	if _, err := ValidateURLFormat("https://example.com:bad", true); err == nil {
		t.Fatalf("expected invalid port to fail")
	}

	// 验证末尾斜杠被移除
	normalized, err := ValidateURLFormat("https://example.com/", false)
	if err != nil {
		t.Fatalf("expected trailing slash url to pass, got %v", err)
	}
	if normalized != "https://example.com" {
		t.Fatalf("expected trailing slash to be removed, got %s", normalized)
	}

	// 验证多个末尾斜杠被移除
	normalized, err = ValidateURLFormat("https://example.com///", false)
	if err != nil {
		t.Fatalf("expected multiple trailing slashes to pass, got %v", err)
	}
	if normalized != "https://example.com" {
		t.Fatalf("expected all trailing slashes to be removed, got %s", normalized)
	}

	// 验证带路径的 URL 末尾斜杠被移除
	normalized, err = ValidateURLFormat("https://example.com/api/v1/", false)
	if err != nil {
		t.Fatalf("expected trailing slash url with path to pass, got %v", err)
	}
	if normalized != "https://example.com/api/v1" {
		t.Fatalf("expected trailing slash to be removed from path, got %s", normalized)
	}
}

func TestValidateHTTPURL(t *testing.T) {
	if _, err := ValidateHTTPURL("http://example.com", false, ValidationOptions{}); err == nil {
		t.Fatalf("expected http to fail when allow_insecure_http is false")
	}
	if _, err := ValidateHTTPURL("http://example.com", true, ValidationOptions{}); err != nil {
		t.Fatalf("expected http to pass when allow_insecure_http is true, got %v", err)
	}
	if _, err := ValidateHTTPURL("https://example.com", false, ValidationOptions{RequireAllowlist: true}); err == nil {
		t.Fatalf("expected require allowlist to fail when empty")
	}
	if _, err := ValidateHTTPURL("https://example.com", false, ValidationOptions{AllowedHosts: []string{"api.example.com"}}); err == nil {
		t.Fatalf("expected host not in allowlist to fail")
	}
	if _, err := ValidateHTTPURL("https://api.example.com", false, ValidationOptions{AllowedHosts: []string{"api.example.com"}}); err != nil {
		t.Fatalf("expected allowlisted host to pass, got %v", err)
	}
	if _, err := ValidateHTTPURL("https://sub.api.example.com", false, ValidationOptions{AllowedHosts: []string{"*.example.com"}}); err != nil {
		t.Fatalf("expected wildcard allowlist to pass, got %v", err)
	}
	if _, err := ValidateHTTPURL("https://localhost", false, ValidationOptions{AllowPrivate: false}); err == nil {
		t.Fatalf("expected localhost to be blocked when allow_private_hosts is false")
	}
}

type staticResolver struct {
	ips []net.IP
	err error
}

func (r staticResolver) LookupIP(_ context.Context, _, _ string) ([]net.IP, error) {
	return r.ips, r.err
}

func TestNewValidatedDialContextPinsValidatedIP(t *testing.T) {
	var dialed string
	dial := NewValidatedDialContext(staticResolver{ips: []net.IP{net.ParseIP("93.184.216.34")}}, func(_ context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" {
			t.Fatalf("unexpected network: %s", network)
		}
		dialed = address
		client, server := net.Pipe()
		t.Cleanup(func() {
			_ = client.Close()
			_ = server.Close()
		})
		return client, nil
	})

	conn, err := dial(context.Background(), "tcp", "api.example.com:443")
	if err != nil {
		t.Fatalf("validated dial failed: %v", err)
	}
	_ = conn.Close()
	if dialed != "93.184.216.34:443" {
		t.Fatalf("expected validated IP to be dialed, got %s", dialed)
	}
}

func TestNewValidatedDialContextRejectsMixedPublicPrivateAnswers(t *testing.T) {
	called := false
	dial := NewValidatedDialContext(staticResolver{ips: []net.IP{
		net.ParseIP("93.184.216.34"),
		net.ParseIP("127.0.0.1"),
	}}, func(_ context.Context, _, _ string) (net.Conn, error) {
		called = true
		return nil, nil
	})

	_, err := dial(context.Background(), "tcp", "api.example.com:443")
	if err == nil {
		t.Fatal("expected private DNS answer to be rejected")
	}
	if called {
		t.Fatal("underlying dialer must not run after unsafe resolution")
	}
}

func TestNewValidatedDialContextRejectsPrivateLiteral(t *testing.T) {
	called := false
	dial := NewValidatedDialContext(staticResolver{}, func(_ context.Context, _, _ string) (net.Conn, error) {
		called = true
		return nil, nil
	})

	_, err := dial(context.Background(), "tcp", "10.0.0.7:8080")
	if err == nil {
		t.Fatal("expected private literal IP to be rejected")
	}
	if called {
		t.Fatal("underlying dialer must not run for a private literal")
	}
}

func TestValidatePublicIPRejectsSpecialUseRanges(t *testing.T) {
	blocked := []string{
		"0.0.0.1",
		"100.64.0.1",
		"192.0.2.10",
		"198.18.0.1",
		"198.51.100.10",
		"203.0.113.10",
		"240.0.0.1",
		"100::1",
		"2001:db8::1",
		"::ffff:127.0.0.1",
	}
	for _, raw := range blocked {
		t.Run(raw, func(t *testing.T) {
			if err := validatePublicIP(net.ParseIP(raw)); err == nil {
				t.Fatalf("expected special-use address %s to be rejected", raw)
			}
		})
	}

	if err := validatePublicIP(net.ParseIP("93.184.216.34")); err != nil {
		t.Fatalf("expected public IPv4 address to pass: %v", err)
	}
	if err := validatePublicIP(net.ParseIP("2606:4700:4700::1111")); err != nil {
		t.Fatalf("expected public IPv6 address to pass: %v", err)
	}
}
