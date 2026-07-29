package claude

import "strings"

const anthropicBillingHeaderPrefix = "x-anthropic-billing-header:"

// StripLeadingBillingHeader mirrors CC Switch's Claude Code normalization. A
// rotating billing attribution line at the very start of system text must not
// become an OpenAI system prompt or Responses instruction because it destroys
// prefix-cache reuse. Later occurrences remain untouched as user content.
func StripLeadingBillingHeader(text string) string {
	if !strings.HasPrefix(text, anthropicBillingHeaderPrefix) {
		return text
	}

	lineEnd := strings.IndexAny(text, "\r\n")
	if lineEnd < 0 {
		return ""
	}
	restStart := lineEnd + 1
	if text[lineEnd] == '\r' && restStart < len(text) && text[restStart] == '\n' {
		restStart++
	}
	rest := text[restStart:]
	for _, separator := range []string{"\r\n", "\n", "\r"} {
		if strings.HasPrefix(rest, separator) {
			return strings.TrimPrefix(rest, separator)
		}
	}
	return rest
}
