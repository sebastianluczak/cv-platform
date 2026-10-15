package main

import (
	"net/netip"
	"reflect"
	"testing"
)

func TestValidateOfferURLRejectsUnsafeDestinations(t *testing.T) {
	for _, rawURL := range []string{
		"file:///etc/passwd",
		"http://localhost/job",
		"http://127.0.0.1/job",
		"http://10.0.0.1/job",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]/job",
		"http://example.com:8080/job",
		"https://user:password@example.com/job",
	} {
		if validPublicURLForInput(rawURL) {
			t.Errorf("expected unsafe URL to be rejected: %s", rawURL)
		}
	}
}

func TestValidateOfferURLAcceptsPublicHTTPAndHTTPS(t *testing.T) {
	for _, rawURL := range []string{"https://example.com/jobs/1", "http://example.org/offer"} {
		if !validPublicURLForInput(rawURL) {
			t.Errorf("expected valid public URL syntax: %s", rawURL)
		}
	}
}

func TestIsPublicAddressRejectsSpecialRanges(t *testing.T) {
	for _, value := range []string{
		"127.0.0.1",
		"10.10.0.1",
		"100.64.0.1",
		"169.254.169.254",
		"192.0.2.1",
		"198.18.0.1",
		"2001:db8::1",
		"2002::1",
	} {
		if isPublicAddress(netip.MustParseAddr(value)) {
			t.Errorf("expected %s to be rejected", value)
		}
	}
	if !isPublicAddress(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("expected public IPv4 address to be allowed")
	}
}

func TestSafeDialRejectsPrivateAddressBeforeConnecting(t *testing.T) {
	if _, err := safeDialContext(t.Context(), "tcp", "127.0.0.1:80"); err == nil {
		t.Fatal("expected private address to be rejected before dialing")
	}
}

func TestHTMLTextDropsHeadScriptAndStyleContent(t *testing.T) {
	text, err := htmlText([]byte(`<html><head><title>Do not include Java</title></head><body><h2>Required Skills</h2><p>Go &amp; Kubernetes</p><script>awsSecret()</script><style>.hidden { color:red }</style></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	if text != "Required Skills\nGo & Kubernetes" {
		t.Fatalf("unexpected extracted text: %q", text)
	}
}

func TestRequiredSkillsUsesRequiredSectionAndFallsBackWhenAbsent(t *testing.T) {
	required, found := requiredSkills("Required Qualifications:\nGo, Kubernetes\nPreferred Skills:\nReact, AWS")
	if !found {
		t.Fatal("expected explicit required section")
	}
	if !reflect.DeepEqual(required, []string{"go", "kubernetes"}) {
		t.Fatalf("unexpected required skills: %v", required)
	}

	required, found = requiredSkills("Backend engineer with Go and PostgreSQL. Docker experience preferred.")
	if found {
		t.Fatal("did not expect an explicit required section")
	}
	if !reflect.DeepEqual(required, []string{"backend", "go", "postgresql", "backend engineer", "docker"}) {
		t.Fatalf("unexpected fallback skills: %v", required)
	}
}

func TestRequiredSkillPercentReportsMatchedAndMissing(t *testing.T) {
	percent, matched, missing, scored := requiredSkillPercent(
		[]string{"go", "kubernetes", "terraform"},
		[]string{"Go", "Terraform"},
	)
	if !scored || percent != 67 || !reflect.DeepEqual(matched, []string{"go", "terraform"}) ||
		!reflect.DeepEqual(missing, []string{"kubernetes"}) {
		t.Fatalf("unexpected result: percent=%d matched=%v missing=%v scored=%v", percent, matched, missing, scored)
	}
	if _, _, _, scored := requiredSkillPercent(nil, []string{"go"}); scored {
		t.Fatal("expected empty requirement set to be unscorable")
	}
}
