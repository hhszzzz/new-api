package gemini

import "strings"

func normalizeGeminiModelID(model string) string {
	model = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(model), "/"))
	return strings.TrimPrefix(model, "models/")
}

// resolveGeminiNativeURL follows CC Switch's Gemini URL normalization rules:
// resource-root bases such as /v1beta or /v1beta/models are reduced before the
// canonical endpoint is appended, while an already complete generateContent
// endpoint is kept verbatim and only receives the endpoint query parameters.
func resolveGeminiNativeURL(baseURL, endpoint string) string {
	baseURL = strings.TrimSpace(baseURL)
	baseWithoutFragment := baseURL
	if index := strings.IndexByte(baseWithoutFragment, '#'); index >= 0 {
		baseWithoutFragment = baseWithoutFragment[:index]
	}
	basePath, baseQuery := splitGeminiURLQuery(strings.TrimRight(baseWithoutFragment, "/"))
	endpointPath, endpointQuery := splitGeminiURLQuery(endpoint)

	if isCompleteGeminiMethodURL(basePath) {
		return joinGeminiURLQuery(basePath, baseQuery, endpointQuery)
	}

	origin, path := splitGeminiOriginAndPath(basePath)
	prefix := normalizeGeminiBasePath(path)
	targetPath := "/" + strings.TrimLeft(endpointPath, "/")
	return joinGeminiURLQuery(origin+prefix+targetPath, baseQuery, endpointQuery)
}

func splitGeminiURLQuery(value string) (string, string) {
	if index := strings.IndexByte(value, '?'); index >= 0 {
		return value[:index], value[index+1:]
	}
	return value, ""
}

func splitGeminiOriginAndPath(baseURL string) (string, string) {
	schemeIndex := strings.Index(baseURL, "://")
	if schemeIndex < 0 {
		return baseURL, ""
	}
	pathIndex := strings.IndexByte(baseURL[schemeIndex+3:], '/')
	if pathIndex < 0 {
		return baseURL, ""
	}
	pathIndex += schemeIndex + 3
	return baseURL[:pathIndex], baseURL[pathIndex:]
}

func normalizeGeminiBasePath(path string) string {
	path = strings.TrimRight(path, "/")
	if path == "" || path == "/" {
		return ""
	}

	for _, marker := range []string{"/v1beta/models/", "/v1/models/", "/models/"} {
		if index := strings.Index(path, marker); index >= 0 {
			return normalizeGeminiPrefix(path[:index])
		}
	}

	for _, suffix := range []string{
		"/v1beta/openai/chat/completions",
		"/v1/openai/chat/completions",
		"/openai/chat/completions",
		"/v1beta/openai/responses",
		"/v1/openai/responses",
		"/openai/responses",
		"/v1beta/openai",
		"/v1/openai",
		"/openai",
		"/v1beta/models",
		"/v1/models",
		"/models",
		"/v1beta",
		"/v1",
	} {
		if path == suffix {
			return ""
		}
		if strings.HasSuffix(path, suffix) {
			return normalizeGeminiPrefix(strings.TrimSuffix(path, suffix))
		}
	}

	return path
}

func normalizeGeminiPrefix(prefix string) string {
	prefix = strings.TrimRight(prefix, "/")
	if prefix == "" || prefix == "/" {
		return ""
	}
	return prefix
}

func isCompleteGeminiMethodURL(value string) bool {
	value = strings.TrimRight(value, "/")
	return strings.Contains(value, "/models/") &&
		(strings.HasSuffix(value, ":generateContent") || strings.HasSuffix(value, ":streamGenerateContent"))
}

func joinGeminiURLQuery(path string, queries ...string) string {
	parts := make([]string, 0, len(queries))
	for _, query := range queries {
		for _, part := range strings.Split(query, "&") {
			if part != "" {
				parts = append(parts, part)
			}
		}
	}
	if len(parts) == 0 {
		return path
	}
	return path + "?" + strings.Join(parts, "&")
}
