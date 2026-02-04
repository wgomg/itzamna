package utils

import (
	"math"
	"regexp"
	"strings"
)

func CountWords(text string) int {
	words := strings.Fields(text)
	wordCount := len(words)

	return wordCount
}

func EstimateTokensFromWords(wordCount int) int {
	return int(math.Round(float64(wordCount) * 1.3))
}

func CleanUp(text string) string {
	re := regexp.MustCompile(`[$€£¥¢%&*+=<>^|~@#\\_\[\]{}]`)
	return re.ReplaceAllString(text, "")
}

func Truncate(s *string, maxLenght int) *string {
	defaultString := "Unknown"

	if s == nil || strings.ReplaceAll(*s, " ", "") == "" {
		return &defaultString
	}

	if len(*s) <= maxLenght {
		return s
	}

	trunc := (*s)[:maxLenght]
	return &trunc
}

func CleanCodeBlock(s string) string {
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```JSON")
	s = strings.TrimPrefix(s, "```")

	s = strings.TrimSuffix(s, "```")

	return strings.TrimSpace(s)
}
