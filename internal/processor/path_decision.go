package processor

import (
	"github.com/wgomg/itzamna/internal/utils"
)

func ShouldReduceContent(estimatedTokens int, thresholdTokens int) bool {
	return estimatedTokens > thresholdTokens
}

func EstimateTokens(content string) int {
	cleanedUpContent := utils.CleanUp(content)

	wordCount := utils.CountWords(cleanedUpContent)
	estimatedTokens := utils.EstimateTokensFromWords(wordCount)

	return estimatedTokens
}
