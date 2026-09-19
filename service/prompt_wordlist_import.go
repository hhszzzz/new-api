package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

const (
	wordlistMaxFileBytes   = 10 * 1024 * 1024
	wordlistMaxSourceBytes = 30 * 1024 * 1024
	wordlistMaxWords       = 500000
	wordlistMaxFiles       = 100
)

var wordlistProtection = &common.SSRFProtection{AllowedPorts: []int{443}, ApplyIPFilterForDomain: true}

func NormalizePromptWordlistURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || len(raw) > 2048 {
		return "", errors.New("invalid_source_url")
	}
	if u.Port() != "" && u.Port() != "443" {
		return "", errors.New("invalid_source_url")
	}
	if err := wordlistProtection.ValidateNetworkTarget(u.Hostname(), 443); err != nil {
		return "", errors.New("private_source_not_allowed")
	}
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	if u.Hostname() == "github.com" {
		u.RawQuery = ""
		u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), ".git")
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" || (len(parts) > 2 && (len(parts) < 4 || (parts[2] != "tree" && parts[2] != "blob"))) {
			return "", errors.New("invalid_github_source")
		}
	}
	return u.String(), nil
}

func newPromptWordlistHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	protected := &protectedFetchDialer{
		resolver: net.DefaultResolver, dialContext: dialer.DialContext,
		getProtection: func() (*common.SSRFProtection, bool, error) { return wordlistProtection, true, nil },
	}
	return &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			DialContext: protected.DialContext, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second,
			MaxIdleConns: 8, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second, ForceAttemptHTTP2: true,
		},
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too_many_redirects")
			}
			_, err := NormalizePromptWordlistURL(request.URL.String())
			return err
		},
	}
}

type wordlistFetcher struct {
	ctx       context.Context
	client    *http.Client
	bytesRead int
}

func (f *wordlistFetcher) get(source, etag string) ([]byte, string, bool, error) {
	if _, err := NormalizePromptWordlistURL(source); err != nil {
		return nil, "", false, err
	}
	request, err := http.NewRequestWithContext(f.ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, "", false, errors.New("invalid_source_url")
	}
	request.Header.Set("User-Agent", "new-api-wordlist-import")
	if request.URL.Hostname() == "api.github.com" {
		request.Header.Set("Accept", "application/vnd.github+json")
	}
	if etag != "" {
		request.Header.Set("If-None-Match", etag)
	}
	response, err := f.client.Do(request)
	if err != nil {
		return nil, "", false, errors.New("download_failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		return nil, etag, true, nil
	}
	if response.StatusCode == 429 || (response.StatusCode == 403 && response.Header.Get("X-RateLimit-Remaining") == "0") {
		return nil, "", false, errors.New("source_rate_limited")
	}
	if response.StatusCode != http.StatusOK {
		return nil, "", false, fmt.Errorf("download_http_%d", response.StatusCode)
	}
	if response.ContentLength > wordlistMaxFileBytes {
		return nil, "", false, errors.New("file_too_large")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, wordlistMaxFileBytes+1))
	if err != nil {
		return nil, "", false, errors.New("download_failed")
	}
	if len(body) > wordlistMaxFileBytes {
		return nil, "", false, errors.New("file_too_large")
	}
	f.bytesRead += len(body)
	if f.bytesRead > wordlistMaxSourceBytes {
		return nil, "", false, errors.New("source_too_large")
	}
	newETag := response.Header.Get("ETag")
	if len(newETag) > 512 {
		newETag = ""
	}
	return body, newETag, false, nil
}

func (f *wordlistFetcher) getJSON(source string, target any) error {
	body, _, _, err := f.get(source, "")
	if err != nil {
		return err
	}
	if common.Unmarshal(body, target) != nil {
		return errors.New("invalid_github_response")
	}
	return nil
}

type githubWordlistTree struct {
	Truncated bool `json:"truncated"`
	Tree      []struct {
		Path string `json:"path"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
		Size int64  `json:"size"`
	} `json:"tree"`
}

type wordlistSourceFile struct{ Name, URL string }

func (f *wordlistFetcher) githubFiles(source *url.URL, previousRevision string) ([]wordlistSourceFile, string, bool, error) {
	parts := strings.Split(strings.Trim(source.Path, "/"), "/")
	repo := url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1])
	api := "https://api.github.com/repos/" + repo
	ref, filePath, isFile := "", "", false
	if len(parts) == 2 {
		var metadata struct {
			DefaultBranch string `json:"default_branch"`
		}
		if err := f.getJSON(api, &metadata); err != nil {
			return nil, "", false, err
		}
		ref = metadata.DefaultBranch
	} else {
		isFile = parts[2] == "blob"
		remainder := strings.Join(parts[3:], "/")
		// GitHub ref names may themselves contain '/'. Resolve the longest actual
		// ref before treating the remainder as a directory or filename.
		for _, kind := range []string{"heads", "tags"} {
			var refs []struct {
				Ref string `json:"ref"`
			}
			if err := f.getJSON(api+"/git/matching-refs/"+kind+"/"+url.PathEscape(parts[3]), &refs); err != nil {
				return nil, "", false, err
			}
			for _, candidate := range refs {
				name := strings.TrimPrefix(candidate.Ref, "refs/"+kind+"/")
				if (remainder == name || strings.HasPrefix(remainder, name+"/")) && len(name) > len(ref) {
					ref = name
				}
			}
			if ref != "" {
				break
			}
		}
		if ref == "" {
			ref = parts[3]
		}
		filePath = strings.TrimPrefix(strings.TrimPrefix(remainder, ref), "/")
	}
	if ref == "" {
		return nil, "", false, errors.New("invalid_github_source")
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if err := f.getJSON(api+"/commits/"+url.PathEscape(ref), &commit); err != nil {
		return nil, "", false, err
	}
	if len(commit.SHA) != 40 {
		return nil, "", false, errors.New("invalid_github_response")
	}
	if previousRevision == commit.SHA {
		return nil, commit.SHA, true, nil
	}
	rawBase := "https://raw.githubusercontent.com/" + repo + "/" + commit.SHA + "/"
	if isFile {
		if filePath == "" {
			return nil, "", false, errors.New("invalid_github_source")
		}
		return []wordlistSourceFile{{Name: filePath, URL: rawBase + (&url.URL{Path: filePath}).EscapedPath()}}, commit.SHA, false, nil
	}
	treeID := commit.SHA
	if filePath != "" {
		for _, directory := range strings.Split(filePath, "/") {
			var tree githubWordlistTree
			if err := f.getJSON(api+"/git/trees/"+treeID, &tree); err != nil {
				return nil, "", false, err
			}
			next := ""
			for _, entry := range tree.Tree {
				if entry.Path == directory && entry.Type == "tree" {
					next = entry.SHA
					break
				}
			}
			if next == "" {
				return nil, "", false, errors.New("github_directory_not_found")
			}
			treeID = next
		}
	}
	var tree githubWordlistTree
	if err := f.getJSON(api+"/git/trees/"+treeID+"?recursive=1", &tree); err != nil {
		return nil, "", false, err
	}
	if tree.Truncated {
		return nil, "", false, errors.New("github_tree_too_large")
	}
	files := make([]wordlistSourceFile, 0)
	for _, entry := range tree.Tree {
		if entry.Type != "blob" || !wordlistCandidate(entry.Path, filePath != "") {
			continue
		}
		if entry.Size > wordlistMaxFileBytes {
			return nil, "", false, errors.New("file_too_large")
		}
		name := path.Join(filePath, entry.Path)
		files = append(files, wordlistSourceFile{Name: name, URL: rawBase + (&url.URL{Path: name}).EscapedPath()})
		if len(files) > wordlistMaxFiles {
			return nil, "", false, errors.New("too_many_files")
		}
	}
	slices.SortFunc(files, func(a, b wordlistSourceFile) int { return strings.Compare(a.Name, b.Name) })
	if len(files) == 0 {
		return nil, "", false, errors.New("no_wordlist_files")
	}
	return files, commit.SHA, false, nil
}

func wordlistCandidate(name string, explicitDirectory bool) bool {
	lower := strings.ToLower(name)
	if !slices.Contains([]string{".txt", ".dic", ".json"}, path.Ext(lower)) {
		return false
	}
	for _, segment := range strings.Split(lower, "/") {
		if strings.HasPrefix(segment, ".") || slices.Contains([]string{"docs", "doc", "examples", "example", "tests", "test", "scripts", "dist", "build", "node_modules"}, segment) {
			return false
		}
	}
	base := path.Base(lower)
	if strings.HasPrefix(base, "readme") || strings.HasPrefix(base, "license") || strings.HasPrefix(base, "changelog") || base == "package.json" {
		return false
	}
	if explicitDirectory || path.Ext(lower) == ".dic" {
		return true
	}
	for _, marker := range []string{"word", "vocabulary", "lexicon", "sensitive", "dict", "敏感", "词"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func parsePromptWordlist(name string, body []byte, words map[string]struct{}) error {
	body = bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(body) {
		return errors.New("invalid_encoding")
	}
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(strings.ToLower(trimmed), "<!doctype") || strings.HasPrefix(strings.ToLower(trimmed), "<html") {
		return errors.New("unsupported_format")
	}
	var values []string
	if strings.EqualFold(path.Ext(name), ".json") {
		if strings.HasPrefix(trimmed, "[") {
			if common.Unmarshal(body, &values) != nil {
				return errors.New("unsupported_format")
			}
		} else {
			var document struct {
				Words *[]string `json:"words"`
			}
			if common.Unmarshal(body, &document) != nil || document.Words == nil {
				return errors.New("unsupported_format")
			}
			values = *document.Words
		}
	} else {
		if ext := strings.ToLower(path.Ext(name)); ext != "" && ext != ".txt" && ext != ".dic" {
			return errors.New("unsupported_format")
		}
		values = strings.Split(trimmed, "\n")
	}
	for _, value := range values {
		word := strings.ToLower(strings.TrimSpace(value))
		if word == "" || strings.HasPrefix(word, "#") || strings.HasPrefix(word, "//") {
			continue
		}
		if utf8.RuneCountInString(word) > 256 || strings.ContainsAny(word, "\x00\r\n") {
			return errors.New("invalid_word")
		}
		words[word] = struct{}{}
		if len(words) > wordlistMaxWords {
			return errors.New("too_many_words")
		}
	}
	return nil
}

type promptWordlistImport struct {
	Content   []byte
	Hash      string
	Revision  string
	ETag      string
	Files     []string
	WordCount int
	Unchanged bool
}

func downloadPromptWordlist(ctx context.Context, client *http.Client, row *model.PromptWordlist) (promptWordlistImport, error) {
	result := promptWordlistImport{}
	normalized, err := NormalizePromptWordlistURL(row.SourceURL)
	if err != nil {
		return result, err
	}
	u, _ := url.Parse(normalized)
	fetcher := wordlistFetcher{ctx: ctx, client: client}
	files := []wordlistSourceFile{{Name: path.Base(u.Path), URL: normalized}}
	if u.Hostname() == "github.com" {
		files, result.Revision, result.Unchanged, err = fetcher.githubFiles(u, row.SourceRevision)
		if err != nil || result.Unchanged {
			return result, err
		}
	}
	words := make(map[string]struct{})
	for _, file := range files {
		etag := ""
		if u.Hostname() != "github.com" {
			etag = row.ETag
		}
		body, newETag, unchanged, err := fetcher.get(file.URL, etag)
		if err != nil {
			return result, err
		}
		if unchanged {
			if row.ContentHash == "" {
				return result, errors.New("empty_wordlist")
			}
			result.Unchanged = true
			return result, nil
		}
		if err := parsePromptWordlist(file.Name, body, words); err != nil {
			return result, err
		}
		result.Files = append(result.Files, file.Name)
		result.ETag = newETag
	}
	if len(words) == 0 {
		return result, errors.New("empty_wordlist")
	}
	ordered := make([]string, 0, len(words))
	for word := range words {
		ordered = append(ordered, word)
	}
	slices.Sort(ordered)
	result.Content = []byte(strings.Join(ordered, "\n"))
	digest := sha256.Sum256(result.Content)
	result.Hash, result.WordCount = hex.EncodeToString(digest[:]), len(ordered)
	return result, nil
}

func runPromptWordlistImports() {
	if !common.IsMasterNode {
		return
	}
	owner := common.GetUUID()
	client := newPromptWordlistHTTPClient()
	defer client.CloseIdleConnections()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		row, err := model.ClaimPromptWordlist(owner, common.GetTimestamp())
		if err != nil {
			logger.LogWarn(context.Background(), "wordlist import claim failed")
			continue
		}
		if row == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		result, downloadErr := downloadPromptWordlist(ctx, client, row)
		cancel()
		values := map[string]any{"status": "ready", "last_error": "", "last_success_at": common.GetTimestamp(), "next_sync_at": time.Now().Add(24 * time.Hour).Unix()}
		if downloadErr == nil && !result.Unchanged {
			if InitAc(strings.Split(string(result.Content), "\n")) == nil {
				downloadErr = errors.New("wordlist_compilation_failed")
			}
			manifest, encodeErr := common.Marshal(result.Files)
			if encodeErr != nil {
				downloadErr = errors.New("wordlist_encoding_failed")
			}
			values["content"], values["content_hash"], values["word_count"] = result.Content, result.Hash, result.WordCount
			values["source_revision"], values["etag"], values["source_files"], values["file_count"] = result.Revision, result.ETag, manifest, len(result.Files)
		}
		if downloadErr != nil {
			values = map[string]any{"status": "failed", "last_error": downloadErr.Error(), "next_sync_at": time.Now().Add(time.Hour).Unix()}
		}
		if _, err := model.FinishPromptWordlist(row, values); err != nil {
			logger.LogWarn(context.Background(), "wordlist import completion failed")
		}
		if err := RefreshPromptWordlists(); err != nil {
			logger.LogWarn(context.Background(), "wordlist import snapshot refresh failed")
		}
	}
}
