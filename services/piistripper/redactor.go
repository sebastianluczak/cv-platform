package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/jdkato/prose/v2"
)

type match struct {
	start int
	end   int
	label string
}

var directIdentifierPatterns = []struct {
	pattern *regexp.Regexp
	label   string
}{
	{regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`), "EMAIL"},
	{regexp.MustCompile(`(?i)\b(?:https?://|www\.)[^\s<>()]+`), "URL"},
	{regexp.MustCompile(`(?i)\b\d{1,6}\s+(?:[[:alnum:].'-]+\s+){0,5}(?:street|st|road|rd|avenue|ave|lane|ln|boulevard|blvd|drive|dr|court|ct|way|place|pl)\b(?:,?\s*(?:apt|apartment|suite|unit|#)\s*[\w-]+)?`), "ADDRESS"},
	{regexp.MustCompile(`(?i)(?:\+?\d{1,3}[-.\s]?)?(?:\(?\d{2,4}\)?[-.\s]?)\d{3,4}[-.\s]?\d{3,4}`), "PHONE"},
}

func redactText(text string) (string, error) {
	matches := make([]match, 0)
	for _, rule := range directIdentifierPatterns {
		for _, bounds := range rule.pattern.FindAllStringIndex(text, -1) {
			matches = append(matches, match{start: bounds[0], end: bounds[1], label: rule.label})
		}
	}

	doc, err := prose.NewDocument(text)
	if err != nil {
		return "", fmt.Errorf("recognize text entities: %w", err)
	}
	for _, entity := range doc.Entities() {
		label := entityPlaceholder(entity.Label)
		if label == "" || entity.Text == "" {
			continue
		}
		for offset := 0; offset < len(text); {
			start := strings.Index(text[offset:], entity.Text)
			if start < 0 {
				break
			}
			start += offset
			end := start + len(entity.Text)
			matches = append(matches, match{start: start, end: end, label: label})
			offset = end
		}
	}

	if len(matches) == 0 {
		return text, nil
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].start == matches[j].start {
			return matches[i].end > matches[j].end
		}
		return matches[i].start < matches[j].start
	})

	var output strings.Builder
	output.Grow(len(text))
	cursor := 0
	for index := 0; index < len(matches); {
		current := matches[index]
		if current.start < cursor {
			index++
			continue
		}
		output.WriteString(text[cursor:current.start])
		output.WriteByte('[')
		output.WriteString(current.label)
		output.WriteByte(']')
		cursor = current.end
		index++
		for index < len(matches) && matches[index].start < cursor {
			index++
		}
	}
	output.WriteString(text[cursor:])
	return output.String(), nil
}

func entityPlaceholder(label string) string {
	switch label {
	case "PERSON":
		return "PERSON"
	case "ORG":
		return "ORGANIZATION"
	default:
		return ""
	}
}
