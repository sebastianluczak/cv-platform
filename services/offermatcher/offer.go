package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"brodaty.dev/cv-platform/common/skillpatterns"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	maxOfferTextBytes = 100 << 10
	maxOfferPageBytes = 2 << 20
	maxOfferURLLength = 2048
	offerFetchTimeout = 15 * time.Second
)

var (
	requiredHeading = regexp.MustCompile(`(?i)^\s*(?:#+\s*)?(?:requirements?|required(?:\s+(?:skills?|qualifications?|experience|competencies))?|must[- ]have(?:\s+skills?)?|minimum (?:qualifications|requirements)|what you(?:'ll| will) need|what you bring)\b\s*:?\s*(.*)$`)
	otherHeading    = regexp.MustCompile(`(?i)^\s*(?:preferred(?:\s+(?:skills?|qualifications?|experience))?|nice[- ]to[- ]have(?: skills?)?|responsibilities|what you(?:'ll| will) do|about (?:the role|us|you)|benefits|our offer|qualifications|role overview|job description)\s*:?\s*(.*)$`)
)

func fetchOfferText(ctx context.Context, rawURL string) (string, error) {
	if len(rawURL) > maxOfferURLLength {
		return "", fmt.Errorf("offer URL exceeds %d characters", maxOfferURLLength)
	}
	parsedURL, err := validateOfferURL(rawURL)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, offerFetchTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("create offer request: %w", err)
	}
	request.Header.Set("Accept", "text/html, text/plain, application/xhtml+xml")
	request.Header.Set("User-Agent", "cv-platform-offermatcher/1.0")

	client := &http.Client{
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           safeDialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 8 * time.Second,
			IdleConnTimeout:       30 * time.Second,
			MaxIdleConnsPerHost:   1,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 {
				return fmt.Errorf("offer URL exceeded redirect limit")
			}
			if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
				return fmt.Errorf("offer URL redirect downgraded from HTTPS")
			}
			_, err := validateOfferURL(req.URL.String())
			return err
		},
	}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("fetch offer URL: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("offer URL returned HTTP %d", response.StatusCode)
	}

	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	if mediaType != "text/html" && mediaType != "application/xhtml+xml" && mediaType != "text/plain" {
		return "", fmt.Errorf("offer URL has unsupported content type %q", mediaType)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maxOfferPageBytes+1))
	if err != nil {
		return "", fmt.Errorf("read offer response: %w", err)
	}
	if len(content) > maxOfferPageBytes {
		return "", fmt.Errorf("offer page exceeds %d bytes", maxOfferPageBytes)
	}
	if !utf8.Valid(content) {
		return "", fmt.Errorf("offer page must use UTF-8 encoding")
	}
	if mediaType == "text/plain" {
		return strings.TrimSpace(string(content)), nil
	}
	text, err := htmlText(content)
	if err != nil {
		return "", fmt.Errorf("parse offer HTML: %w", err)
	}
	return text, nil
}

func validateOfferURL(rawURL string) (*url.URL, error) {
	parsedURL, err := url.ParseRequestURI(strings.TrimSpace(rawURL))
	if err != nil || parsedURL == nil || parsedURL.Host == "" {
		return nil, fmt.Errorf("offer URL must be an absolute HTTP or HTTPS URL")
	}
	parsedURL.Scheme = strings.ToLower(parsedURL.Scheme)
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("offer URL must use HTTP or HTTPS")
	}
	if parsedURL.User != nil {
		return nil, fmt.Errorf("offer URL must not contain credentials")
	}
	host := strings.TrimSuffix(strings.ToLower(parsedURL.Hostname()), ".")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return nil, fmt.Errorf("offer URL host is not publicly accessible")
	}
	address, addressErr := netip.ParseAddr(host)
	if addressErr != nil && !strings.Contains(host, ".") {
		return nil, fmt.Errorf("offer URL host is not publicly accessible")
	}
	port := parsedURL.Port()
	defaultPort := "80"
	if parsedURL.Scheme == "https" {
		defaultPort = "443"
	}
	if port != "" && port != defaultPort {
		return nil, fmt.Errorf("offer URL must use the default HTTP or HTTPS port")
	}
	if addressErr == nil && !isPublicAddress(address) {
		return nil, fmt.Errorf("offer URL host is not publicly accessible")
	}
	parsedURL.Fragment = ""
	return parsedURL, nil
}

var blockedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func isPublicAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() ||
		address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	for _, network := range blockedNetworks {
		if network.Contains(address) {
			return false
		}
	}
	return true
}

func safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("split offer host address: %w", err)
	}
	if parsed, err := netip.ParseAddr(host); err == nil {
		if !isPublicAddress(parsed) {
			return nil, fmt.Errorf("offer URL host is not publicly accessible")
		}
		return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(parsed.String(), port))
	}

	resolver := net.Resolver{}
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve offer host %q: %w", host, err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("offer host %q has no addresses", host)
	}
	for _, resolved := range addresses {
		if !isPublicAddress(resolved) {
			return nil, fmt.Errorf("offer host %q resolves to a non-public address", host)
		}
	}
	var dialErrors []string
	for _, resolved := range addresses {
		connection, err := (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(resolved.String(), port))
		if err == nil {
			return connection, nil
		}
		dialErrors = append(dialErrors, err.Error())
	}
	return nil, fmt.Errorf("connect to offer host %q: %s", host, strings.Join(dialErrors, "; "))
}

func htmlText(content []byte) (string, error) {
	document, err := html.Parse(bytes.NewReader(content))
	if err != nil {
		return "", err
	}
	var builder strings.Builder
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.DataAtom {
			case atom.Head, atom.Script, atom.Style, atom.Noscript, atom.Svg, atom.Template:
				return
			}
			if isBlockElement(node.DataAtom) {
				builder.WriteByte('\n')
			}
		}
		if node.Type == html.TextNode {
			builder.WriteString(node.Data)
			builder.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
		if node.Type == html.ElementNode && isBlockElement(node.DataAtom) {
			builder.WriteByte('\n')
		}
	}
	visit(document)
	lines := strings.Split(builder.String(), "\n")
	var normalized []string
	for index, line := range lines {
		lines[index] = strings.Join(strings.Fields(line), " ")
		if lines[index] == "" {
			continue
		}
		normalized = append(normalized, lines[index])
	}
	return strings.TrimSpace(strings.Join(normalized, "\n")), nil
}

func isBlockElement(element atom.Atom) bool {
	switch element {
	case atom.Address, atom.Article, atom.Aside, atom.Blockquote, atom.Br, atom.Dd,
		atom.Details, atom.Div, atom.Dl, atom.Dt, atom.Fieldset, atom.Figcaption,
		atom.Figure, atom.Footer, atom.Form, atom.H1, atom.H2, atom.H3, atom.H4,
		atom.H5, atom.H6, atom.Header, atom.Hr, atom.Li, atom.Main, atom.Nav,
		atom.Ol, atom.P, atom.Pre, atom.Section, atom.Table, atom.Tbody, atom.Td,
		atom.Tfoot, atom.Th, atom.Thead, atom.Tr, atom.Ul:
		return true
	default:
		return false
	}
}

func requiredSkills(text string) ([]string, bool) {
	lines := strings.Split(text, "\n")
	var required strings.Builder
	inRequiredSection := false
	foundRequiredSection := false
	for _, line := range lines {
		if match := requiredHeading.FindStringSubmatch(line); match != nil {
			foundRequiredSection = true
			inRequiredSection = true
			required.WriteString(match[1])
			required.WriteByte('\n')
			continue
		}
		if inRequiredSection && otherHeading.MatchString(line) {
			inRequiredSection = false
			continue
		}
		if inRequiredSection {
			required.WriteString(line)
			required.WriteByte('\n')
		}
	}
	source := text
	if foundRequiredSection {
		source = required.String()
	}
	return skillpatterns.PatternsInText(source), foundRequiredSection
}

func requiredSkillPercent(required, candidate []string) (int, []string, []string, bool) {
	if len(required) == 0 {
		return 0, nil, nil, false
	}
	candidateSet := make(map[string]struct{}, len(candidate))
	for _, skill := range candidate {
		candidateSet[strings.ToLower(skill)] = struct{}{}
	}
	matched := make([]string, 0, len(required))
	missing := make([]string, 0, len(required))
	for _, skill := range required {
		if _, ok := candidateSet[strings.ToLower(skill)]; ok {
			matched = append(matched, skill)
		} else {
			missing = append(missing, skill)
		}
	}
	return (100*len(matched) + len(required)/2) / len(required), matched, missing, true
}

func validPublicURLForInput(rawURL string) bool {
	_, err := validateOfferURL(rawURL)
	return err == nil
}
