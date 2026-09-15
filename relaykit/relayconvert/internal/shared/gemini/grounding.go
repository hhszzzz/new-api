package gemini

import (
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

// GroundingWebSearchQueries returns distinct queries in provider order, even
// when Gemini repeats the same grounding metadata across candidates.
func GroundingWebSearchQueries(response *dto.GeminiChatResponse) []string {
	if response == nil {
		return nil
	}
	queries := make([]string, 0)
	seen := make(map[string]struct{})
	for candidateIndex := range response.Candidates {
		metadata := response.Candidates[candidateIndex].GroundingMetadata
		if metadata == nil {
			continue
		}
		for _, query := range metadata.WebSearchQueries {
			query = strings.TrimSpace(query)
			if query == "" {
				continue
			}
			if _, exists := seen[query]; exists {
				continue
			}
			seen[query] = struct{}{}
			queries = append(queries, query)
		}
	}
	return queries
}
