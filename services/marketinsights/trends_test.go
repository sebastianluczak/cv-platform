package main

import (
	"fmt"
	"testing"
	"time"
)

func TestMarketSummaryUsesDistinctOffersAndEqualPeriods(t *testing.T) {
	observations := make([]Observation, 0, 20)
	previousDay := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	recentDay := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	for index := range 10 {
		skills := []string{"java"}
		if index < 5 {
			skills = append(skills, "go")
		}
		observations = append(observations, Observation{
			OfferID: fmt.Sprintf("previous-%02d", index), SourceID: "source-a",
			ObservedAt: previousDay, RoleCategory: "backend", SkillIDs: skills,
		})
		skills = []string{"java"}
		if index < 8 {
			skills = append(skills, "go")
		}
		observations = append(observations, Observation{
			OfferID: fmt.Sprintf("recent-%02d", index), SourceID: "source-a",
			ObservedAt: recentDay, RoleCategory: "backend", SkillIDs: skills,
		})
	}
	observations = append(observations, Observation{
		OfferID: "recent-00", SourceID: "source-a", ObservedAt: recentDay.Add(2 * time.Hour),
		RoleCategory: "backend", SkillIDs: []string{"go", "java"},
	})

	summary := buildMarketSummary(observations, 7, "backend", recentDay)
	if summary.RecentPeriod.From != "2026-10-11" || summary.RecentPeriod.To != "2026-10-17" ||
		summary.PreviousPeriod.From != "2026-10-04" || summary.PreviousPeriod.To != "2026-10-10" {
		t.Fatalf("unexpected equal periods: recent=%+v previous=%+v", summary.RecentPeriod, summary.PreviousPeriod)
	}
	if summary.RecentSampleSize != 10 || summary.PreviousSampleSize != 10 {
		t.Fatalf("expected distinct offer denominators of 10; recent=%d previous=%d",
			summary.RecentSampleSize, summary.PreviousSampleSize)
	}
	goTrend := findSkill(t, summary.Skills, "go")
	if goTrend.CurrentCount != 8 || goTrend.PreviousCount != 5 ||
		goTrend.CurrentPrevalence != 80 || goTrend.PreviousPrevalence != 50 {
		t.Fatalf("unexpected Go counts or prevalence: %+v", goTrend)
	}
	if goTrend.ChangePP == nil || *goTrend.ChangePP != 30 {
		t.Fatalf("expected +30 percentage points, got %v", goTrend.ChangePP)
	}
	if goTrend.RelativeChange == nil || *goTrend.RelativeChange != 60 {
		t.Fatalf("expected +60 percent relative change, got %v", goTrend.RelativeChange)
	}
	if len(goTrend.Daily) != 7 || goTrend.Daily[6].SampleSize != 10 {
		t.Fatalf("unexpected daily sample series: %+v", goTrend.Daily)
	}
	if summary.MethodologyVersion != methodologyVersion || len(summary.RecentSources) != 1 {
		t.Fatalf("missing methodology or source coverage: %+v", summary)
	}
}

func TestMarketSummaryDoesNotClaimChangeWithoutPreviousSample(t *testing.T) {
	observations := []Observation{{
		OfferID: "offer-1", SourceID: "source-a",
		ObservedAt:   time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC),
		RoleCategory: "backend", SkillIDs: []string{"go"},
	}}
	summary := buildMarketSummary(observations, 7, "", time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC))
	trend := findSkill(t, summary.Skills, "go")
	if trend.ChangePP != nil || trend.RelativeChange != nil {
		t.Fatalf("expected no comparison without a previous sample: %+v", trend)
	}
	if summary.PreviousSampleSize != 0 {
		t.Fatalf("expected no previous sample, got %d", summary.PreviousSampleSize)
	}
}

func TestMarketSummaryFlagsSmallSampleAndSourceCoverageChange(t *testing.T) {
	observations := []Observation{
		{
			OfferID: "previous-1", SourceID: "source-old",
			ObservedAt:   time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC),
			RoleCategory: "backend", SkillIDs: []string{"go"},
		},
		{
			OfferID: "recent-1", SourceID: "source-new",
			ObservedAt:   time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC),
			RoleCategory: "backend", SkillIDs: []string{"go"},
		},
	}
	summary := buildMarketSummary(observations, 7, "", time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC))
	if len(summary.Warnings) != 2 {
		t.Fatalf("expected small-sample and coverage warnings, got %v", summary.Warnings)
	}
}

func TestMarketSummaryFlagsSourceMixShift(t *testing.T) {
	recent := []SourceCoverage{{SourceID: "a", OfferCount: 80}, {SourceID: "b", OfferCount: 20}}
	previous := []SourceCoverage{{SourceID: "a", OfferCount: 50}, {SourceID: "b", OfferCount: 50}}
	if !sourceCoverageChanged(recent, previous) {
		t.Fatal("expected a meaningful source-mix change to be flagged")
	}
	if sourceCoverageChanged(previous, previous) {
		t.Fatal("unchanged source coverage should not be flagged")
	}
}

func findSkill(t *testing.T, skills []SkillTrend, skillID string) SkillTrend {
	t.Helper()
	for _, skill := range skills {
		if skill.SkillID == skillID {
			return skill
		}
	}
	t.Fatalf("skill %q not found in %+v", skillID, skills)
	return SkillTrend{}
}
