package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxImportBodySize = 10 << 20

type API struct {
	store       *Store
	importToken string
	now         func() time.Time
}

func NewAPI(store *Store, importToken string) http.Handler {
	api := &API{store: store, importToken: importToken, now: time.Now}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", api.health)
	mux.HandleFunc("GET /api/market/roles", api.roles)
	mux.HandleFunc("GET /api/market/trends", api.trends)
	mux.HandleFunc("POST /admin/import", api.importOffers)
	return mux
}

func (api *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (api *API) roles(w http.ResponseWriter, _ *http.Request) {
	roles := make([]string, 0, len(roleCategories))
	for role := range roleCategories {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	writeJSON(w, http.StatusOK, map[string][]string{"roles": roles})
}

func (api *API) trends(w http.ResponseWriter, r *http.Request) {
	days := defaultWindowDays
	if value := r.URL.Query().Get("days"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || validateWindowDays(parsed) != nil {
			writeAPIError(w, http.StatusBadRequest, "days must be an integer between 7 and 90")
			return
		}
		days = parsed
	}
	roleCategory := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("role")))
	if roleCategory != "" {
		if _, exists := roleCategories[roleCategory]; !exists {
			writeAPIError(w, http.StatusBadRequest, "role is not a supported Poland IT category")
			return
		}
	}
	for key := range r.URL.Query() {
		if key != "days" && key != "role" {
			writeAPIError(w, http.StatusBadRequest, "unsupported query parameter")
			return
		}
	}
	summary := buildMarketSummary(api.store.Snapshot(), days, roleCategory, api.now())
	writeJSON(w, http.StatusOK, summary)
}

func (api *API) importOffers(w http.ResponseWriter, r *http.Request) {
	if api.importToken == "" {
		writeAPIError(w, http.StatusServiceUnavailable, "market data import is not configured")
		return
	}
	if !validBearerToken(r.Header.Get("Authorization"), api.importToken) {
		writeAPIError(w, http.StatusUnauthorized, "valid bearer token is required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBodySize)
	content, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "request body is invalid or exceeds the size limit")
		return
	}
	var batch ImportBatch
	if err := decodeOneJSON(content, &batch); err != nil {
		writeAPIError(w, http.StatusBadRequest, "request body must be exactly one valid import JSON object")
		return
	}
	count, err := api.store.Import(r.Context(), batch)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			writeAPIError(w, http.StatusRequestTimeout, "market import was cancelled")
			return
		}
		log.Printf("Market import rejected: %s", safeLogField(err.Error()))
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"accepted": count, "status": "stored", "market": "PL-IT",
		"methodology_version": methodologyVersion,
	})
}

func validBearerToken(header, expected string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	provided := []byte(strings.TrimSpace(strings.TrimPrefix(header, prefix)))
	expectedBytes := []byte(expected)
	return len(provided) == len(expectedBytes) && subtle.ConstantTimeCompare(provided, expectedBytes) == 1
}

func decodeOneJSON(content []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request contains trailing JSON")
	}
	return nil
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("Write market API response: %v", err)
	}
}

func safeLogField(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.ReplaceAll(value, "\n", " ")
}
