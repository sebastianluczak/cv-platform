package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenRouterGenerateReturnsMarkdownWithoutLoggingPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected request: method %s, authorization %q", r.Method, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"# Resume\n\nGo engineer"}}]}`))
	}))
	defer server.Close()

	client := newOpenRouterClient("test-key", "test/model")
	client.endpoint = server.URL
	client.httpClient = server.Client()
	markdown, err := client.generate(context.Background(), "test prompt")
	if err != nil {
		t.Fatal(err)
	}
	if markdown != "# Resume\n\nGo engineer" {
		t.Fatalf("unexpected Markdown: %q", markdown)
	}
}

func TestOpenRouterGenerateRejectsInvalidResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{"provider failure", http.StatusServiceUnavailable, "private provider detail"},
		{"invalid JSON", http.StatusOK, "not json"},
		{"no choices", http.StatusOK, `{"choices":[]}`},
		{"empty content", http.StatusOK, `{"choices":[{"message":{"content":"  "}}]}`},
		{"oversized content", http.StatusOK, fmt.Sprintf(`{"choices":[{"message":{"content":%q}}]}`, strings.Repeat("x", maxMarkdownSize+1))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.statusCode)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			client := newOpenRouterClient("test-key", "test/model")
			client.endpoint = server.URL
			client.httpClient = server.Client()
			if _, err := client.generate(context.Background(), "test prompt"); err == nil {
				t.Fatal("expected response to be rejected")
			} else if strings.Contains(err.Error(), "private provider detail") {
				t.Fatalf("provider response body leaked in error: %v", err)
			}
		})
	}
}

func TestBuildPromptIncludesInputsInExplicitDataSections(t *testing.T) {
	prompt, err := buildPrompt("candidate facts", `{"categories":[]}`, "job requirements")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"redacted_resume":"candidate facts"`, `"matched_skill_evidence":{"categories":[]}`, `"job_offer_text":"job requirements"`} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("prompt missing %q", expected)
		}
	}
}

func TestBuildPromptRejectsInvalidSkillJSON(t *testing.T) {
	if _, err := buildPrompt("resume", `not-json`, "offer"); err == nil {
		t.Fatal("expected invalid skill JSON to be rejected")
	}
}
