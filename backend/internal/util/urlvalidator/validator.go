package urlvalidator

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// IPResolver is the subset of net.Resolver used by the outbound safety dialer.
// It is intentionally small so DNS behavior can be tested without touching the
// process-wide resolver.
type IPResolver interface {
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
}

// DialContextFunc matches net.Dialer.DialContext.
type DialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

var blockedSpecialUsePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // current network
	netip.MustParsePrefix("100.64.0.0/10"),   // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
	netip.MustParsePrefix("192.88.99.0/24"),  // deprecated 6to4 relay anycast
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmark testing
	netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved/future use
	netip.MustParsePrefix("100::/64"),        // discard-only address block
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
}

type ValidationOptions struct {
	AllowedHosts     []string
	RequireAllowlist bool
	AllowPrivate     bool
}

// ValidateHTTPURL validates an outbound HTTP/HTTPS URL.
//
// It provides a single validation entry point that supports:
// - scheme 校验（https 或可选允许 http）
// - 可选 allowlist（支持 *.example.com 通配）
// - allow_private_hosts 策略（阻断 localhost/私网字面量 IP）
//
// 注意：DNS Rebinding 防护（解析后 IP 校验）应在实际发起请求时执行，避免 TOCTOU。
func ValidateHTTPURL(raw string, allowInsecureHTTP bool, opts ValidationOptions) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("url is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid url: %s", trimmed)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && (!allowInsecureHTTP || scheme != "http") {
		return "", fmt.Errorf("invalid url scheme: %s", parsed.Scheme)
	}

	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return "", errors.New("invalid host")
	}
	if !opts.AllowPrivate && isBlockedHost(host) {
		return "", fmt.Errorf("host is not allowed: %s", host)
	}

	if port := parsed.Port(); port != "" {
		num, err := strconv.Atoi(port)
		if err != nil || num <= 0 || num > 65535 {
			return "", fmt.Errorf("invalid port: %s", port)
		}
	}

	allowlist := normalizeAllowlist(opts.AllowedHosts)
	if opts.RequireAllowlist && len(allowlist) == 0 {
		return "", errors.New("allowlist is not configured")
	}
	if len(allowlist) > 0 && !isAllowedHost(host, allowlist) {
		return "", fmt.Errorf("host is not allowed: %s", host)
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func ValidateURLFormat(raw string, allowInsecureHTTP bool) (string, error) {
	// 最小格式校验：仅保证 URL 可解析且 scheme 合规，不做白名单/私网/SSRF 校验
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("url is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid url: %s", trimmed)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && (!allowInsecureHTTP || scheme != "http") {
		return "", fmt.Errorf("invalid url scheme: %s", parsed.Scheme)
	}

	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return "", errors.New("invalid host")
	}

	if port := parsed.Port(); port != "" {
		num, err := strconv.Atoi(port)
		if err != nil || num <= 0 || num > 65535 {
			return "", fmt.Errorf("invalid port: %s", port)
		}
	}

	return strings.TrimRight(trimmed, "/"), nil
}

func ValidateHTTPSURL(raw string, opts ValidationOptions) (string, error) {
	return ValidateHTTPURL(raw, false, opts)
}

// ValidateResolvedIP validates every address returned by DNS.
//
// This function is useful as an early policy check. It does not by itself pin
// the subsequent TCP connection to those addresses. Callers that connect
// directly should use NewValidatedDialContext so validation and connection use
// the same resolved IP and cannot be separated by DNS rebinding.
func ValidateResolvedIP(host string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := resolvePublicIPs(ctx, net.DefaultResolver, host)
	return err
}

// NewValidatedDialContext returns a direct TCP dialer that resolves the target,
// rejects private/special-use addresses, and then connects to the exact IP that
// was validated. The original hostname remains on the HTTP request, so Host and
// TLS SNI/certificate verification continue to use the requested hostname.
//
// This dialer must only be installed for direct target connections. It must not
// replace the dialer used to connect to an HTTP or SOCKS proxy, because in those
// cases address is the proxy endpoint rather than the upstream target.
func NewValidatedDialContext(resolver IPResolver, dial DialContextFunc) DialContextFunc {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if dial == nil {
		dial = (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	}

	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid dial address %q: %w", address, err)
		}

		ips, err := resolvePublicIPs(ctx, resolver, host)
		if err != nil {
			return nil, err
		}

		var dialErrs []error
		for _, ip := range ips {
			if network == "tcp4" && ip.To4() == nil {
				continue
			}
			if network == "tcp6" && ip.To4() != nil {
				continue
			}

			conn, dialErr := dial(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			dialErrs = append(dialErrs, fmt.Errorf("dial resolved ip %s: %w", ip, dialErr))
		}

		if len(dialErrs) == 0 {
			return nil, fmt.Errorf("dns resolution for %s returned no address compatible with %s", host, network)
		}
		return nil, errors.Join(dialErrs...)
	}
}

func resolvePublicIPs(ctx context.Context, resolver IPResolver, host string) ([]net.IP, error) {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return nil, errors.New("host is empty")
	}

	if literal := net.ParseIP(host); literal != nil {
		if err := validatePublicIP(literal); err != nil {
			return nil, err
		}
		return []net.IP{literal}, nil
	}

	ips, err := resolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("dns resolution failed for %s: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("dns resolution returned no addresses for %s", host)
	}

	validated := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if err := validatePublicIP(ip); err != nil {
			// Reject the whole hostname if any answer is unsafe. Otherwise an
			// attacker could place a private address later in the answer set and
			// rely on client fallback behavior.
			return nil, err
		}
		validated = append(validated, ip)
	}
	return validated, nil
}

func validatePublicIP(ip net.IP) error {
	if ip == nil {
		return errors.New("resolved ip is nil")
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return fmt.Errorf("resolved ip %s is not allowed", ip.String())
	}

	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return fmt.Errorf("resolved ip %s is invalid", ip.String())
	}
	addr = addr.Unmap()
	for _, prefix := range blockedSpecialUsePrefixes {
		if prefix.Contains(addr) {
			return fmt.Errorf("resolved ip %s is not allowed", ip.String())
		}
	}
	return nil
}

func normalizeAllowlist(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(values))
	for _, v := range values {
		entry := strings.ToLower(strings.TrimSpace(v))
		if entry == "" {
			continue
		}
		if host, _, err := net.SplitHostPort(entry); err == nil {
			entry = host
		}
		normalized = append(normalized, entry)
	}
	return normalized
}

func isAllowedHost(host string, allowlist []string) bool {
	for _, entry := range allowlist {
		if entry == "" {
			continue
		}
		if strings.HasPrefix(entry, "*.") {
			suffix := strings.TrimPrefix(entry, "*.")
			if host == suffix || strings.HasSuffix(host, "."+suffix) {
				return true
			}
			continue
		}
		if host == entry {
			return true
		}
	}
	return false
}

func isBlockedHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return validatePublicIP(ip) != nil
	}
	return false
}
