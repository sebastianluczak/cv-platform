package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenRouterGenerateReturnsValidatedRoles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected request: method %s, authorization %q", r.Method, r.Header.Get("Authorization"))
		}
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.ResponseFormat == nil || request.ResponseFormat.Type != "json_object" {
			t.Fatal("expected JSON response format")
		}
		if !strings.Contains(request.Messages[0].Content, "Do not limit suggestions to IT roles") {
			t.Fatal("system prompt must include roles beyond IT")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"roles\":[{\"title\":\"Cook\",\"fit_summary\":\"Resume mentions kitchen work.\",\"responsibilities\":[\"Prepare meals\",\"Maintain food safety\"]}]}"}}]}`))
	}))
	defer server.Close()

	client := newOpenRouterClient("test-key", "test/model")
	client.httpClient.Transport = redirectTestRequests(server)
	roles, err := client.generate(context.Background(), "kitchen experience")
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].Title != "Cook" {
		t.Fatalf("unexpected roles: %+v", roles)
	}
}

func TestOpenRouterGenerateRejectsInvalidResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		content    string
	}{
		{"provider failure", http.StatusServiceUnavailable, "private provider detail"},
		{"invalid JSON", http.StatusOK, "not json"},
		{"no roles", http.StatusOK, `{"roles":[]}`},
		{"unknown role field", http.StatusOK, `{"roles":[{"title":"Cook","fit_summary":"Fit","responsibilities":["Prepare meals"],"extra":true}]}`},
		{"duplicate roles", http.StatusOK, `{"roles":[{"title":"Cook","fit_summary":"Fit","responsibilities":["Prepare meals"]},{"title":"cook","fit_summary":"Fit","responsibilities":["Prepare meals"]}]}`},
		{"multiple values", http.StatusOK, `{"roles":[{"title":"Cook","fit_summary":"Fit","responsibilities":["Prepare meals"]}]} {}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.statusCode)
				_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, test.content)
			}))
			defer server.Close()
			client := newOpenRouterClient("test-key", "test/model")
			client.httpClient.Transport = redirectTestRequests(server)
			if _, err := client.generate(context.Background(), "resume text"); err == nil {
				t.Fatal("expected response to be rejected")
			} else if strings.Contains(err.Error(), "private provider detail") {
				t.Fatalf("provider response body leaked in error: %v", err)
			}
		})
	}
}

func TestBuildPromptKeepsResumeInJSONData(t *testing.T) {
	prompt, err := buildPrompt("candidate facts")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, `"redacted_resume":"candidate facts"`) {
		t.Fatalf("prompt does not contain resume as explicit JSON data: %s", prompt)
	}
}

type redirectTestTransport struct {
	server    *httptest.Server
	transport http.RoundTripper
}

func redirectTestRequests(server *httptest.Server) http.RoundTripper {
	return redirectTestTransport{server: server, transport: server.Client().Transport}
}

func (transport redirectTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.String() != openRouterEndpoint {
		return nil, fmt.Errorf("unexpected OpenRouter endpoint %q", request.URL)
	}
	redirected := request.Clone(request.Context())
	target, err := http.NewRequestWithContext(request.Context(), request.Method, transport.server.URL, request.Body)
	if err != nil {
		return nil, err
	}
	redirected.URL = target.URL
	redirected.Host = target.Host
	return transport.transport.RoundTrip(redirected)
}
