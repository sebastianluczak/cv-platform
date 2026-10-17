package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeOfferStoresOnlyDerivedSignals(t *testing.T) {
	observedAt := time.Date(2026, time.October, 17, 6, 0, 0, 0, time.FixedZone("CEST", 2*60*60))
	observation, err := normalizeOffer("authorized-source", ImportOffer{
		ExternalID: "record-123", ObservedAt: observedAt,
		Title: "Backend Engineer", Description: "Go and PostgreSQL. Contact jane@example.com.",
		RoleCategory: "backend",
	})
	if err != nil {
		t.Fatalf("normalize offer: %v", err)
	}
	if observation.ObservedAt.Location() != time.UTC {
		t.Fatalf("expected normalized UTC timestamp; got %s", observation.ObservedAt.Location())
	}
	if len(observation.OfferID) != 64 || observation.SourceID != "authorized-source" {
		t.Fatalf("unexpected observation identity: %+v", observation)
	}
	if !contains(observation.SkillIDs, "go") || !contains(observation.SkillIDs, "postgresql") {
		t.Fatalf("expected shared skill patterns; got %v", observation.SkillIDs)
	}
	if contains(observation.SkillIDs, "backend") || contains(observation.SkillIDs, "backend engineer") {
		t.Fatalf("role labels must not be counted as skills: %v", observation.SkillIDs)
	}
	encoded, err := json.Marshal(observation)
	if err != nil {
		t.Fatalf("marshal observation: %v", err)
	}
	if strings.Contains(string(encoded), "jane@example.com") || strings.Contains(string(encoded), "Backend Engineer") {
		t.Fatalf("observation persisted raw advertisement content: %s", encoded)
	}
}

func TestMarketSkillsCanonicalizeAliasesAndExcludeRoleLabels(t *testing.T) {
	skills := marketSkillsInText("Backend engineer with Go, Golang, PostgreSQL, and K8s")
	if len(skills) != 3 || !contains(skills, "go") || !contains(skills, "postgresql") || !contains(skills, "kubernetes") {
		t.Fatalf("expected canonical market skill identifiers, got %v", skills)
	}
}

func TestImportIsIdempotentPerSourceOfferAndUTCDate(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "nested", "market.json"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	first := ImportBatch{
		SourceID: "source-a", Market: "PL-IT",
		Offers: []ImportOffer{
			{ExternalID: "offer-1", ObservedAt: time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC), Title: "Backend", Description: "Go", RoleCategory: "backend"},
			{ExternalID: "offer-1", ObservedAt: time.Date(2026, 10, 17, 2, 0, 0, 0, time.UTC), Title: "Backend", Description: "Java", RoleCategory: "backend"},
		},
	}
	if _, err := store.Import(context.Background(), first); err != nil {
		t.Fatalf("first import: %v", err)
	}
	first.Offers[0].Description = "Rust"
	if _, err := store.Import(context.Background(), ImportBatch{SourceID: "source-a", Market: "PL-IT", Offers: first.Offers[:1]}); err != nil {
		t.Fatalf("repeat import: %v", err)
	}
	if got := len(store.Snapshot()); got != 2 {
		t.Fatalf("expected one observation per UTC date, got %d", got)
	}
	if !contains(store.Snapshot()[0].SkillIDs, "rust") {
		t.Fatalf("same-day repeat did not replace the observation: %+v", store.Snapshot()[0])
	}

	reloaded, err := NewStore(filepath.Join(filepath.Dir(filepath.Dir(store.path)), "nested", "market.json"))
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	if got := len(reloaded.Snapshot()); got != 2 {
		t.Fatalf("expected two persisted observations after reload, got %d", got)
	}
}

func TestImportRejectsInvalidBatchWithoutChangingStore(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "market.json"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	batch := ImportBatch{SourceID: "source-a", Market: "PL-IT", Offers: []ImportOffer{
		{ExternalID: "valid", ObservedAt: time.Now().UTC(), Title: "Engineer", Description: "Go", RoleCategory: "backend"},
		{ExternalID: "invalid", ObservedAt: time.Now().UTC(), Title: "Engineer", Description: "Go", RoleCategory: "unknown"},
	}}
	if _, err := store.Import(context.Background(), batch); err == nil {
		t.Fatal("expected invalid batch to fail")
	}
	if got := len(store.Snapshot()); got != 0 {
		t.Fatalf("invalid batch changed store: %d observations", got)
	}
	if _, err := os.Stat(store.path); !os.IsNotExist(err) {
		t.Fatalf("invalid batch created the persistent store: %v", err)
	}
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
