package main

import (
	"time"

	"brodaty.dev/cv-platform/common/skillpatterns"
)

type CategoryScore = skillpatterns.CategoryScore
type MatchResult = skillpatterns.MatchResult

func scoreText(jobID, text string, scoredAt time.Time) MatchResult {
	return skillpatterns.ScoreText(jobID, text, scoredAt)
}
