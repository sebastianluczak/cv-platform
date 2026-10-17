package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"brodaty.dev/cv-platform/common/skillpatterns"
)

const (
	storeVersion       = 1
	methodologyVersion = 1
	maxStoreSize       = 128 << 20
	maxImportOffers    = 5000
	maxDescriptionSize = 200 << 10
)

var (
	sourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	roleCategories  = map[string]struct{}{
		"backend": {}, "frontend": {}, "mobile": {}, "platform": {}, "devops": {},
		"data": {}, "security": {}, "qa": {}, "architecture": {},
	}
	marketSkillAliases = map[string]string{
		"amazon web services": "aws", "aws": "aws", "ec2": "aws", "ecs": "aws", "eks": "aws", "lambda": "aws",
		"angular": "angular", "angularjs": "angular",
		"ansible": "ansible",
		"azure":   "azure", "azure devops": "azure", "microsoft azure": "azure",
		"c#": "c-sharp", "dotnet": "dotnet", ".net": "dotnet",
		"c++": "c-plus-plus", "cpp": "c-plus-plus",
		"cassandra": "cassandra",
		"ci cd":     "ci-cd", "ci/cd": "ci-cd", "cicd": "ci-cd",
		"docker": "docker", "docker compose": "docker", "dockerfile": "docker",
		"django":  "django",
		"fastapi": "fastapi",
		"gcp":     "gcp", "google cloud": "gcp", "google cloud platform": "gcp",
		"gin": "gin",
		"go":  "go", "golang": "go",
		"graphql":    "graphql",
		"grpc":       "grpc",
		"helm":       "helm",
		"java":       "java",
		"javascript": "javascript", "ecmascript": "javascript",
		"kotlin":     "kotlin",
		"kubernetes": "kubernetes", "k8s": "kubernetes",
		"linux":         "linux",
		"mariadb":       "mariadb",
		"microservices": "microservices",
		"mongodb":       "mongodb",
		"mysql":         "mysql",
		"node.js":       "node.js", "nodejs": "node.js", "node js": "node.js",
		"php":      "php",
		"postgres": "postgresql", "postgresql": "postgresql",
		"prometheus": "prometheus",
		"python":     "python",
		"react":      "react", "react.js": "react", "reactjs": "react",
		"redis": "redis",
		"rest":  "rest", "restful": "rest", "rest api": "rest",
		"ruby":   "ruby",
		"rust":   "rust",
		"scala":  "scala",
		"spring": "spring", "spring boot": "spring", "spring framework": "spring",
		"sql":        "sql",
		"terraform":  "terraform",
		"typescript": "typescript",
		"vue":        "vue", "vue.js": "vue", "vuejs": "vue",
	}
)

type ImportBatch struct {
	SourceID string        `json:"source_id"`
	Market   string        `json:"market"`
	Offers   []ImportOffer `json:"offers"`
}

type ImportOffer struct {
	ExternalID   string    `json:"external_id"`
	ObservedAt   time.Time `json:"observed_at"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	RoleCategory string    `json:"role_category"`
}

type Observation struct {
	OfferID            string    `json:"offer_id"`
	SourceID           string    `json:"source_id"`
	ObservedAt         time.Time `json:"observed_at"`
	RoleCategory       string    `json:"role_category"`
	SkillIDs           []string  `json:"skill_ids"`
	MethodologyVersion int       `json:"methodology_version"`
}

type storeSnapshot struct {
	Version      int           `json:"version"`
	Observations []Observation `json:"observations"`
}

type Store struct {
	mu           sync.RWMutex
	path         string
	observations []Observation
}

func NewStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("market data file path is empty")
	}
	store := &Store{path: path}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, fmt.Errorf("read market data file: %w", err)
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxStoreSize+1))
	if err != nil {
		return nil, fmt.Errorf("read market data file: %w", err)
	}
	if len(content) > maxStoreSize {
		return nil, fmt.Errorf("market data file exceeds %d bytes", maxStoreSize)
	}
	var snapshot storeSnapshot
	if err := decodeOneJSON(content, &snapshot); err != nil {
		return nil, fmt.Errorf("decode market data file: %w", err)
	}
	if snapshot.Version != storeVersion {
		return nil, fmt.Errorf("unsupported market data file version %d", snapshot.Version)
	}
	for index, observation := range snapshot.Observations {
		if err := validateObservation(observation); err != nil {
			return nil, fmt.Errorf("invalid market observation %d: %w", index, err)
		}
	}
	store.observations = snapshot.Observations
	return store, nil
}

func (store *Store) Import(ctx context.Context, batch ImportBatch) (int, error) {
	sourceID := strings.TrimSpace(batch.SourceID)
	if !sourceIDPattern.MatchString(sourceID) {
		return 0, fmt.Errorf("source_id must contain 1 to 64 letters, digits, dots, underscores, or hyphens")
	}
	if strings.TrimSpace(batch.Market) != "PL-IT" {
		return 0, fmt.Errorf("market must be PL-IT")
	}
	if len(batch.Offers) == 0 || len(batch.Offers) > maxImportOffers {
		return 0, fmt.Errorf("offers must contain between 1 and %d records", maxImportOffers)
	}

	incoming := make([]Observation, 0, len(batch.Offers))
	for index, offer := range batch.Offers {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		observation, err := normalizeOffer(sourceID, offer)
		if err != nil {
			return 0, fmt.Errorf("offer %d: %w", index, err)
		}
		incoming = append(incoming, observation)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	updated := make(map[string]Observation, len(store.observations)+len(incoming))
	for _, observation := range store.observations {
		updated[observationKey(observation)] = observation
	}
	for _, observation := range incoming {
		updated[observationKey(observation)] = observation
	}
	next := make([]Observation, 0, len(updated))
	for _, observation := range updated {
		next = append(next, observation)
	}
	sort.Slice(next, func(i, j int) bool {
		if next[i].ObservedAt.Equal(next[j].ObservedAt) {
			if next[i].SourceID == next[j].SourceID {
				return next[i].OfferID < next[j].OfferID
			}
			return next[i].SourceID < next[j].SourceID
		}
		return next[i].ObservedAt.Before(next[j].ObservedAt)
	})
	if err := store.writeSnapshot(next); err != nil {
		return 0, err
	}
	store.observations = next
	return len(incoming), nil
}

func (store *Store) Snapshot() []Observation {
	store.mu.RLock()
	defer store.mu.RUnlock()
	result := make([]Observation, len(store.observations))
	copy(result, store.observations)
	return result
}

func (store *Store) writeSnapshot(observations []Observation) error {
	content, err := encodeSnapshot(observations)
	if err != nil {
		return err
	}
	if len(content) > maxStoreSize {
		return fmt.Errorf("market data file would exceed %d bytes", maxStoreSize)
	}
	directory := filepath.Dir(store.path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return fmt.Errorf("create market data directory: %w", err)
	}
	file, err := os.CreateTemp(directory, ".market-data-*")
	if err != nil {
		return fmt.Errorf("create temporary market data file: %w", err)
	}
	temporaryPath := file.Name()
	defer os.Remove(temporaryPath)
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return fmt.Errorf("secure temporary market data file: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("write temporary market data file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync temporary market data file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary market data file: %w", err)
	}
	if err := os.Rename(temporaryPath, store.path); err != nil {
		return fmt.Errorf("replace market data file: %w", err)
	}
	return nil
}

func normalizeOffer(sourceID string, offer ImportOffer) (Observation, error) {
	externalID := strings.TrimSpace(offer.ExternalID)
	title := strings.TrimSpace(offer.Title)
	description := strings.TrimSpace(offer.Description)
	roleCategory := strings.TrimSpace(strings.ToLower(offer.RoleCategory))
	if externalID == "" || len(externalID) > 256 || !utf8.ValidString(externalID) {
		return Observation{}, fmt.Errorf("external_id must be valid UTF-8 and contain 1 to 256 bytes")
	}
	if title == "" || len(title) > 500 || !utf8.ValidString(title) {
		return Observation{}, fmt.Errorf("title must be valid UTF-8 and contain 1 to 500 bytes")
	}
	if description == "" || len(description) > maxDescriptionSize || !utf8.ValidString(description) {
		return Observation{}, fmt.Errorf("description must be valid UTF-8 and at most %d bytes", maxDescriptionSize)
	}
	if offer.ObservedAt.IsZero() {
		return Observation{}, fmt.Errorf("observed_at is required")
	}
	if offer.ObservedAt.After(time.Now().UTC().Add(5 * time.Minute)) {
		return Observation{}, fmt.Errorf("observed_at cannot be in the future")
	}
	if _, exists := roleCategories[roleCategory]; !exists {
		return Observation{}, fmt.Errorf("role_category is not a supported Poland IT category")
	}
	digest := sha256.Sum256([]byte(sourceID + "\x00" + externalID))
	skills := marketSkillsInText(title + "\n" + description)
	return Observation{
		OfferID:            hex.EncodeToString(digest[:]),
		SourceID:           sourceID,
		ObservedAt:         offer.ObservedAt.UTC(),
		RoleCategory:       roleCategory,
		SkillIDs:           skills,
		MethodologyVersion: methodologyVersion,
	}, nil
}

func marketSkillsInText(text string) []string {
	seen := make(map[string]struct{})
	skills := make([]string, 0)
	for _, pattern := range skillpatterns.PatternsInText(text) {
		skillID, exists := marketSkillAliases[pattern]
		if !exists {
			continue
		}
		if _, exists := seen[skillID]; exists {
			continue
		}
		seen[skillID] = struct{}{}
		skills = append(skills, skillID)
	}
	sort.Strings(skills)
	return skills
}

func validateObservation(observation Observation) error {
	if len(observation.OfferID) != 64 || !sourceIDPattern.MatchString(observation.SourceID) ||
		observation.ObservedAt.IsZero() || observation.MethodologyVersion != methodologyVersion {
		return fmt.Errorf("identity, timestamp, or methodology is invalid")
	}
	if _, exists := roleCategories[observation.RoleCategory]; !exists {
		return fmt.Errorf("unsupported role category %q", observation.RoleCategory)
	}
	seen := make(map[string]struct{}, len(observation.SkillIDs))
	for _, skillID := range observation.SkillIDs {
		if strings.TrimSpace(skillID) == "" {
			return fmt.Errorf("skill identifier is empty")
		}
		if _, exists := seen[skillID]; exists {
			return fmt.Errorf("duplicate skill identifier %q", skillID)
		}
		seen[skillID] = struct{}{}
	}
	return nil
}

func observationKey(observation Observation) string {
	return observation.OfferID + "\x00" + observation.ObservedAt.UTC().Format("2006-01-02")
}

func encodeSnapshot(observations []Observation) ([]byte, error) {
	content, err := json.Marshal(storeSnapshot{Version: storeVersion, Observations: observations})
	if err != nil {
		return nil, fmt.Errorf("encode market data file: %w", err)
	}
	return content, nil
}
