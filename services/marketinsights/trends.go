package main

import (
	"fmt"
	"math"
	"sort"
	"time"
)

const (
	defaultWindowDays = 30
	minimumSampleSize = 30
)

type Period struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type SourceCoverage struct {
	SourceID   string `json:"source_id"`
	OfferCount int    `json:"offer_count"`
}

type DailyPoint struct {
	Date          string  `json:"date"`
	SampleSize    int     `json:"sample_size"`
	MatchingCount int     `json:"matching_count"`
	Prevalence    float64 `json:"prevalence"`
}

type SkillTrend struct {
	SkillID            string       `json:"skill_id"`
	CurrentCount       int          `json:"current_count"`
	CurrentPrevalence  float64      `json:"current_prevalence"`
	PreviousCount      int          `json:"previous_count"`
	PreviousPrevalence float64      `json:"previous_prevalence"`
	ChangePP           *float64     `json:"change_percentage_points,omitempty"`
	RelativeChange     *float64     `json:"relative_change_percent,omitempty"`
	Daily              []DailyPoint `json:"daily"`
}

type MarketSummary struct {
	Market             string           `json:"market"`
	RoleCategory       string           `json:"role_category,omitempty"`
	MethodologyVersion int              `json:"methodology_version"`
	GeneratedAt        time.Time        `json:"generated_at"`
	RecentPeriod       Period           `json:"recent_period"`
	PreviousPeriod     Period           `json:"previous_period"`
	RecentSampleSize   int              `json:"recent_sample_size"`
	PreviousSampleSize int              `json:"previous_sample_size"`
	RecentSources      []SourceCoverage `json:"recent_sources"`
	PreviousSources    []SourceCoverage `json:"previous_sources"`
	LastObservedAt     *time.Time       `json:"last_observed_at,omitempty"`
	Warnings           []string         `json:"warnings"`
	Skills             []SkillTrend     `json:"skills"`
}

type offerAggregate struct {
	observation Observation
}

type dailyAggregate struct {
	offers map[string]struct{}
	skills map[string]map[string]struct{}
}

func buildMarketSummary(observations []Observation, days int, roleCategory string, now time.Time) MarketSummary {
	today := utcDate(now)
	recentFrom := today.AddDate(0, 0, -(days - 1))
	previousTo := recentFrom.AddDate(0, 0, -1)
	previousFrom := previousTo.AddDate(0, 0, -(days - 1))
	summary := MarketSummary{
		Market:             "PL-IT",
		RoleCategory:       roleCategory,
		MethodologyVersion: methodologyVersion,
		GeneratedAt:        now.UTC(),
		RecentPeriod:       Period{From: recentFrom.Format("2006-01-02"), To: today.Format("2006-01-02")},
		PreviousPeriod:     Period{From: previousFrom.Format("2006-01-02"), To: previousTo.Format("2006-01-02")},
		RecentSources:      []SourceCoverage{},
		PreviousSources:    []SourceCoverage{},
		Warnings:           []string{},
		Skills:             []SkillTrend{},
	}

	recentOffers := periodOffers(observations, recentFrom, today, roleCategory)
	previousOffers := periodOffers(observations, previousFrom, previousTo, roleCategory)
	recentSources := sourceCoverage(recentOffers)
	previousSources := sourceCoverage(previousOffers)
	summary.RecentSampleSize = len(recentOffers)
	summary.PreviousSampleSize = len(previousOffers)
	summary.RecentSources = recentSources
	summary.PreviousSources = previousSources
	if summary.RecentSampleSize > 0 && summary.RecentSampleSize < minimumSampleSize {
		summary.Warnings = append(summary.Warnings,
			fmt.Sprintf("Low sample: %d distinct advertisements is below the %d-ad interpretation threshold.",
				summary.RecentSampleSize, minimumSampleSize))
	}
	if summary.RecentSampleSize > 0 && summary.PreviousSampleSize == 0 {
		summary.Warnings = append(summary.Warnings, "No previous-period sample is available for comparison.")
	} else if sourceCoverageChanged(summary.RecentSources, summary.PreviousSources) &&
		(summary.RecentSampleSize > 0 || summary.PreviousSampleSize > 0) {
		summary.Warnings = append(summary.Warnings,
			"Source coverage or source mix changed between periods; observed changes may reflect collection changes as well as demand.")
	}

	currentCounts := skillCounts(recentOffers)
	previousCounts := skillCounts(previousOffers)
	allSkills := make(map[string]struct{}, len(currentCounts)+len(previousCounts))
	for skillID := range currentCounts {
		allSkills[skillID] = struct{}{}
	}
	for skillID := range previousCounts {
		allSkills[skillID] = struct{}{}
	}
	daily := dailySkillCounts(observations, recentFrom, today, roleCategory)
	skillIDs := make([]string, 0, len(allSkills))
	for skillID := range allSkills {
		skillIDs = append(skillIDs, skillID)
	}
	sort.Slice(skillIDs, func(i, j int) bool {
		if currentCounts[skillIDs[i]] == currentCounts[skillIDs[j]] {
			if previousCounts[skillIDs[i]] == previousCounts[skillIDs[j]] {
				return skillIDs[i] < skillIDs[j]
			}
			return previousCounts[skillIDs[i]] > previousCounts[skillIDs[j]]
		}
		return currentCounts[skillIDs[i]] > currentCounts[skillIDs[j]]
	})
	if len(skillIDs) > 30 {
		skillIDs = skillIDs[:30]
	}

	summary.Skills = make([]SkillTrend, 0, len(skillIDs))
	for _, skillID := range skillIDs {
		currentCount := currentCounts[skillID]
		previousCount := previousCounts[skillID]
		trend := SkillTrend{
			SkillID:            skillID,
			CurrentCount:       currentCount,
			CurrentPrevalence:  percentage(currentCount, summary.RecentSampleSize),
			PreviousCount:      previousCount,
			PreviousPrevalence: percentage(previousCount, summary.PreviousSampleSize),
			Daily:              make([]DailyPoint, 0, days),
		}
		if summary.PreviousSampleSize > 0 {
			change := roundOne(trend.CurrentPrevalence - trend.PreviousPrevalence)
			trend.ChangePP = &change
			if trend.PreviousPrevalence > 0 {
				relative := roundOne((trend.CurrentPrevalence - trend.PreviousPrevalence) / trend.PreviousPrevalence * 100)
				trend.RelativeChange = &relative
			}
		}
		for date := recentFrom; !date.After(today); date = date.AddDate(0, 0, 1) {
			dateKey := date.Format("2006-01-02")
			aggregate := daily[dateKey]
			matchingCount := 0
			sampleSize := 0
			if aggregate != nil {
				sampleSize = len(aggregate.offers)
				matchingCount = len(aggregate.skills[skillID])
			}
			trend.Daily = append(trend.Daily, DailyPoint{
				Date: dateKey, SampleSize: sampleSize, MatchingCount: matchingCount,
				Prevalence: percentage(matchingCount, sampleSize),
			})
		}
		summary.Skills = append(summary.Skills, trend)
	}

	for index := range observations {
		observation := observations[index]
		if roleCategory != "" && observation.RoleCategory != roleCategory {
			continue
		}
		if summary.LastObservedAt == nil || observation.ObservedAt.After(*summary.LastObservedAt) {
			lastObservedAt := observation.ObservedAt
			summary.LastObservedAt = &lastObservedAt
		}
	}
	return summary
}

func periodOffers(observations []Observation, from, to time.Time, roleCategory string) map[string]offerAggregate {
	offers := make(map[string]offerAggregate)
	for _, observation := range observations {
		date := utcDate(observation.ObservedAt)
		if date.Before(from) || date.After(to) {
			continue
		}
		existing, found := offers[observation.OfferID]
		if !found || observation.ObservedAt.After(existing.observation.ObservedAt) {
			offers[observation.OfferID] = offerAggregate{observation: observation}
		}
	}
	if roleCategory != "" {
		for offerID, offer := range offers {
			if offer.observation.RoleCategory != roleCategory {
				delete(offers, offerID)
			}
		}
	}
	return offers
}

func sourceCoverage(offers map[string]offerAggregate) []SourceCoverage {
	counts := make(map[string]int)
	for _, offer := range offers {
		counts[offer.observation.SourceID]++
	}
	sourceIDs := make([]string, 0, len(counts))
	for sourceID := range counts {
		sourceIDs = append(sourceIDs, sourceID)
	}
	sort.Strings(sourceIDs)
	coverage := make([]SourceCoverage, 0, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		coverage = append(coverage, SourceCoverage{SourceID: sourceID, OfferCount: counts[sourceID]})
	}
	return coverage
}

func sourceCoverageChanged(recent, previous []SourceCoverage) bool {
	recentCounts := make(map[string]int, len(recent))
	previousCounts := make(map[string]int, len(previous))
	sourceIDs := make(map[string]struct{}, len(recent)+len(previous))
	totalRecent, totalPrevious := 0, 0
	for _, source := range recent {
		recentCounts[source.SourceID] = source.OfferCount
		sourceIDs[source.SourceID] = struct{}{}
		totalRecent += source.OfferCount
	}
	for _, source := range previous {
		previousCounts[source.SourceID] = source.OfferCount
		sourceIDs[source.SourceID] = struct{}{}
		totalPrevious += source.OfferCount
	}
	for sourceID := range sourceIDs {
		recentCount, previousCount := recentCounts[sourceID], previousCounts[sourceID]
		if recentCount == 0 || previousCount == 0 {
			return true
		}
		recentShare := float64(recentCount) / float64(totalRecent)
		previousShare := float64(previousCount) / float64(totalPrevious)
		if math.Abs(recentShare-previousShare) >= 0.2 {
			return true
		}
	}
	return false
}

func skillCounts(offers map[string]offerAggregate) map[string]int {
	counts := make(map[string]int)
	for _, offer := range offers {
		for _, skillID := range offer.observation.SkillIDs {
			counts[skillID]++
		}
	}
	return counts
}

func dailySkillCounts(observations []Observation, from, to time.Time, roleCategory string) map[string]*dailyAggregate {
	daily := make(map[string]*dailyAggregate)
	for _, observation := range observations {
		date := utcDate(observation.ObservedAt)
		if date.Before(from) || date.After(to) || (roleCategory != "" && observation.RoleCategory != roleCategory) {
			continue
		}
		dateKey := date.Format("2006-01-02")
		aggregate := daily[dateKey]
		if aggregate == nil {
			aggregate = &dailyAggregate{
				offers: make(map[string]struct{}),
				skills: make(map[string]map[string]struct{}),
			}
			daily[dateKey] = aggregate
		}
		aggregate.offers[observation.OfferID] = struct{}{}
		for _, skillID := range observation.SkillIDs {
			if aggregate.skills[skillID] == nil {
				aggregate.skills[skillID] = make(map[string]struct{})
			}
			aggregate.skills[skillID][observation.OfferID] = struct{}{}
		}
	}
	return daily
}

func percentage(count, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return roundOne(float64(count) * 100 / float64(denominator))
}

func roundOne(value float64) float64 {
	return math.Round(value*10) / 10
}

func utcDate(value time.Time) time.Time {
	year, month, day := value.UTC().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func validateWindowDays(value int) error {
	if value < 7 || value > 90 {
		return fmt.Errorf("days must be between 7 and 90")
	}
	return nil
}
