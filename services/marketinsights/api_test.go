package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImportRequiresTokenAndStoresOnlyValidBatch(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "market.json"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	handler := NewAPI(store, "test-import-secret")
	batch := ImportBatch{
		SourceID: "source-a",
		Market:   "PL-IT",
		Offers: []ImportOffer{{
			ExternalID: "offer-1", ObservedAt: time.Now().UTC().Add(-time.Minute),
			Title: "Backend Engineer", Description: "Go developer", RoleCategory: "backend",
		}},
	}
	bodyContent, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("encode import batch: %v", err)
	}
	body := string(bodyContent)

	request := httptest.NewRequest(http.MethodPost, "/admin/import", strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized without token, got %d: %s", response.Code, response.Body)
	}

	request = httptest.NewRequest(http.MethodPost, "/admin/import", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer test-import-secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("expected accepted import, got %d: %s", response.Code, response.Body)
	}
	if got := len(store.Snapshot()); got != 1 {
		t.Fatalf("expected one stored observation, got %d", got)
	}
}

func TestImportDisabledWithoutTokenAndRejectsUnknownFields(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "market.json"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	handler := NewAPI(store, "")
	request := httptest.NewRequest(http.MethodPost, "/admin/import", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected disabled import, got %d", response.Code)
	}

	handler = NewAPI(store, "secret")
	body := `{"source_id":"source-a","market":"PL-IT","extra":"not-allowed","offers":[]}`
	request = httptest.NewRequest(http.MethodPost, "/admin/import", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected unknown field rejection, got %d", response.Code)
	}
}

func TestTrendEndpointRejectsInvalidFiltersAndReturnsSummary(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "market.json"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	handler := NewAPI(store, "")
	request := httptest.NewRequest(http.MethodGet, "/api/market/trends?days=6", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid days rejection, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/market/trends?days=7&role=backend", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected summary, got %d: %s", response.Code, response.Body)
	}
	var summary MarketSummary
	if err := json.Unmarshal(response.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.Market != "PL-IT" || summary.RoleCategory != "backend" || summary.RecentSampleSize != 0 {
		t.Fatalf("unexpected empty summary: %+v", summary)
	}
}
