package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const maxAvatarBytes int64 = 2 << 20

type avatarImage struct {
	Data     []byte
	MIMEType string
}

func validateAvatarData(data []byte) (avatarImage, error) {
	if len(data) == 0 || int64(len(data)) > maxAvatarBytes {
		return avatarImage{}, errors.New("avatar must be between 1 byte and 2 MiB")
	}
	mimeType := http.DetectContentType(data)
	if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/webp" {
		return avatarImage{}, errors.New("only JPEG, PNG, and WebP avatars are supported")
	}
	return avatarImage{Data: data, MIMEType: mimeType}, nil
}

func storeAvatarImage(ctx context.Context, objects *ObjectStore, image avatarImage) (string, error) {
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}[image.MIMEType]
	id, err := randomID("", 16)
	if err != nil {
		return "", err
	}
	key := "avatars/" + id + ext
	if err := objects.Put(ctx, key, image.MIMEType, image.Data); err != nil {
		return "", err
	}
	return key, nil
}

type avatarDownloader struct {
	lookup func(context.Context, string) ([]net.IPAddr, error)
	dial   func(context.Context, string, string) (net.Conn, error)
}

func newAvatarDownloader() *avatarDownloader {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	return &avatarDownloader{
		lookup: net.DefaultResolver.LookupIPAddr,
		dial:   dialer.DialContext,
	}
}

func (d *avatarDownloader) Fetch(ctx context.Context, rawURL string) (avatarImage, error) {
	parsed, err := validateAvatarURL(rawURL)
	if err != nil {
		return avatarImage{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	transport := &http.Transport{
		Proxy:                 nil,
		DisableCompression:    false,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid avatar address: %w", err)
		}
		addresses, err := d.lookup(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve avatar host: %w", err)
		}
		if len(addresses) == 0 {
			return nil, errors.New("avatar host did not resolve")
		}
		for _, candidate := range addresses {
			if !isPublicIP(candidate.IP) {
				return nil, errors.New("avatar URL resolves to a private or reserved address")
			}
		}
		var lastErr error
		for _, candidate := range addresses {
			conn, err := d.dial(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, fmt.Errorf("connect to avatar host: %w", lastErr)
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("avatar URL redirected too many times")
			}
			_, err := validateAvatarURL(req.URL.String())
			return err
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return avatarImage{}, err
	}
	req.Header.Set("Accept", "image/jpeg,image/png,image/webp")
	req.Header.Set("User-Agent", "haro-bot-avatar-fetcher/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return avatarImage{}, fmt.Errorf("download avatar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return avatarImage{}, fmt.Errorf("avatar server returned %s", resp.Status)
	}
	if resp.ContentLength > maxAvatarBytes {
		return avatarImage{}, errors.New("avatar exceeds 2 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAvatarBytes+1))
	if err != nil {
		return avatarImage{}, fmt.Errorf("read avatar: %w", err)
	}
	return validateAvatarData(data)
}

func validateAvatarURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" {
		return nil, errors.New("avatar_url must be an absolute HTTP or HTTPS URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("avatar_url must use HTTP or HTTPS")
	}
	if parsed.User != nil {
		return nil, errors.New("avatar_url must not contain credentials")
	}
	return parsed, nil
}

var blockedAvatarNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

func isPublicIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() {
		return false
	}
	for _, blocked := range blockedAvatarNetworks {
		if blocked.Contains(addr) {
			return false
		}
	}
	return true
}
