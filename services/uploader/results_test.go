package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResultHandlerValidatesMethodAndJobID(t *testing.T) {
	handler := resultHandler(nil, "resumes")
	tests := []struct {
		name       string
		method     string
		jobID      string
		wantStatus int
	}{
		{name: "method", method: http.MethodPost, wantStatus: http.StatusMethodNotAllowed},
		{name: "invalid ID", method: http.MethodGet, jobID: "../other", wantStatus: http.StatusBadRequest},
		{name: "missing ID", method: http.MethodGet, wantStatus: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "/results?job_id="+test.jobID, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d; want %d", response.Code, test.wantStatus)
			}
		})
	}
}

func TestValidMatchResultRequiresVersionJobIDAndAllCategoryScores(t *testing.T) {
	jobID := "123e4567-e89b-12d3-a456-426614174000"
	result := map[string]any{
		"version":   1,
		"job_id":    jobID,
		"scored_at": time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		"categories": []map[string]any{
			{"category": "backend", "score": 1, "evidence": []string{}},
			{"category": "devops", "score": 2, "evidence": []string{"docker"}},
			{"category": "management", "score": 3, "evidence": []string{"roadmap"}},
			{"category": "architecture", "score": 4, "evidence": []string{"system design"}},
			{"category": "qa", "score": 5, "evidence": []string{"testing"}},
			{"category": "frontend", "score": 1, "evidence": []string{}},
			{"category": "data", "score": 2, "evidence": []string{"analytics"}},
			{"category": "security", "score": 3, "evidence": []string{"encryption"}},
		},
	}
	content, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	categories := result["categories"].([]map[string]any)
	for left, right := 0, len(categories)-1; left < right; left, right = left+1, right-1 {
		categories[left], categories[right] = categories[right], categories[left]
	}
	firstCategory := categories[0]
	content, err = json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !validMatchResult(content, jobID) {
		t.Fatal("expected valid result to pass validation regardless of category order")
	}
	if validMatchResult(content, "123e4567-e89b-12d3-a456-426614174001") {
		t.Fatal("result for another job should be rejected")
	}
	categories[0] = categories[1]
	content, err = json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if validMatchResult(content, jobID) {
		t.Fatal("duplicate categories should be rejected")
	}
	categories[0] = firstCategory

	result["categories"] = append(categories,
		map[string]any{"category": "product", "score": 1, "evidence": []string{}},
		map[string]any{"category": "mobile", "score": 1, "evidence": []string{}},
		map[string]any{"category": "embedded", "score": 1, "evidence": []string{}},
		map[string]any{"category": "platform", "score": 1, "evidence": []string{}},
		map[string]any{"category": "support", "score": 1, "evidence": []string{}},
		map[string]any{"category": "business", "score": 1, "evidence": []string{}},
	)
	content, err = json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !validMatchResult(content, jobID) {
		t.Fatal("expected result with expanded categories to pass validation")
	}
}
