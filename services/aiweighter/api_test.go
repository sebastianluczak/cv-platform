package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testMatchID = "223e4567-e89b-12d3-a456-426614174000"
const testWeighID = "323e4567-e89b-12d3-a456-426614174000"

func TestCreateWeightRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"invalid match ID", `{"match_id":"bad","consent":true}`},
		{"missing consent", `{"match_id":"` + testMatchID + `"}`},
		{"consent declined", `{"match_id":"` + testMatchID + `","consent":false}`},
		{"unknown field", `{"match_id":"` + testMatchID + `","consent":true,"extra":1}`},
		{"multiple JSON values", `{"match_id":"` + testMatchID + `","consent":true}{}`},
	}
	handler := newAIweighterHandler(nil, "", "", nil)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/weights", strings.NewReader(test.body))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected HTTP 400; got %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestWeightEndpointsRejectUnsupportedMethodsAndIDs(t *testing.T) {
	handler := newAIweighterHandler(nil, "", "", nil)
	request := httptest.NewRequest(http.MethodGet, "/weights", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected HTTP 405; got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/weights/not-a-uuid", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 for invalid weigh ID; got %d", response.Code)
	}
}

func TestNewUUIDReturnsRFC4122V4(t *testing.T) {
	id, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	if !idPattern.MatchString(id) || id[14] != '4' {
		t.Fatalf("expected UUID v4; got %q", id)
	}
}
