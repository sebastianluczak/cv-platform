package main

import (
	"regexp"
	"time"
)

type CategoryScore struct {
	Category string   `json:"category"`
	Score    int      `json:"score"`
	Evidence []string `json:"evidence"`
}

type MatchResult struct {
	Version    int             `json:"version"`
	JobID      string          `json:"job_id"`
	ScoredAt   time.Time       `json:"scored_at"`
	Categories []CategoryScore `json:"categories"`
}

type profile struct {
	name     string
	patterns []string
}

var profiles = []profile{
	{
		name: "backend",
		patterns: []string{
			"backend", "api", "rest", "grpc", "microservices", "java", "go", "golang", "python",
			"node.js", "spring boot", "django", "ruby on rails", "postgresql", "kafka",
			"server-side development", "distributed systems",
		},
	},
	{
		name: "devops",
		patterns: []string{
			"devops", "ci/cd", "continuous integration", "continuous delivery", "docker",
			"kubernetes", "terraform", "ansible", "infrastructure as code", "aws", "gcp",
			"azure", "prometheus", "grafana", "helm", "linux", "deployment pipeline",
		},
	},
	{
		name: "management",
		patterns: []string{
			"engineering manager", "team lead", "people management", "roadmap",
			"stakeholder management", "hiring", "mentoring", "performance reviews",
			"budget ownership", "agile", "scrum", "delivery management",
			"strategic planning", "cross-functional leadership",
		},
	},
	{
		name: "architecture",
		patterns: []string{
			"software architecture", "solution architecture", "system design",
			"architectural patterns", "domain-driven design", "event-driven architecture",
			"distributed systems", "scalability", "high availability", "cloud architecture",
			"technical strategy", "system integration", "architecture review", "migration",
		},
	},
	{
		name: "qa",
		patterns: []string{
			"quality assurance", "test automation", "automated testing", "integration testing",
			"unit testing", "end-to-end testing", "selenium", "cypress", "playwright",
			"junit", "test plan", "regression testing", "performance testing", "test strategy",
		},
	},
	{
		name: "frontend",
		patterns: []string{
			"frontend", "front-end", "javascript", "typescript", "react", "angular", "vue",
			"html", "css", "web accessibility", "responsive design", "next.js", "user interface",
		},
	},
	{
		name: "data",
		patterns: []string{
			"data engineering", "data science", "machine learning", "data pipeline", "etl",
			"spark", "pandas", "data warehouse", "dbt", "airflow", "business intelligence",
			"data modeling", "analytics",
		},
	},
	{
		name: "security",
		patterns: []string{
			"cybersecurity", "application security", "security engineering", "threat modeling",
			"penetration testing", "vulnerability management", "iam", "oauth", "encryption",
			"incident response", "zero trust", "compliance", "owasp",
		},
	},
}

func scoreText(jobID, text string, scoredAt time.Time) MatchResult {
	result := MatchResult{
		Version:    1,
		JobID:      jobID,
		ScoredAt:   scoredAt.UTC(),
		Categories: make([]CategoryScore, 0, len(profiles)),
	}
	for _, item := range profiles {
		evidence := make([]string, 0)
		for _, pattern := range item.patterns {
			if patternMatches(pattern, text) {
				evidence = append(evidence, pattern)
			}
		}
		result.Categories = append(result.Categories, CategoryScore{
			Category: item.name,
			Score:    scoreEvidenceCount(len(evidence)),
			Evidence: evidence,
		})
	}
	return result
}

func scoreEvidenceCount(count int) int {
	switch {
	case count <= 1:
		return 1
	case count <= 3:
		return 2
	case count <= 5:
		return 3
	case count <= 7:
		return 4
	default:
		return 5
	}
}

func patternMatches(pattern, text string) bool {
	expression := regexp.MustCompile(`(?i)(^|[^[:alnum:]])` + regexp.QuoteMeta(pattern) + `([^[:alnum:]]|$)`)
	return expression.MatchString(text)
}
