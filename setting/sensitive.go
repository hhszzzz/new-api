package setting

import (
	"strings"
	"sync"
)

var CheckSensitiveEnabled = true
var CheckSensitiveOnPromptEnabled = true

//var CheckSensitiveOnCompletionEnabled = true

// StopOnSensitiveEnabled 如果检测到敏感词，是否立刻停止生成，否则替换敏感词
var StopOnSensitiveEnabled = true

// StreamCacheQueueLength 流模式缓存队列长度，0表示无缓存
var StreamCacheQueueLength = 0

// SensitiveWords 敏感词
// var SensitiveWords []string
var SensitiveWords = []string{
	"test_sensitive",
}

var sensitiveWordsMu sync.RWMutex
var sensitiveWordsVersion uint64

func SensitiveWordsVersion() uint64 {
	sensitiveWordsMu.RLock()
	defer sensitiveWordsMu.RUnlock()
	return sensitiveWordsVersion
}

func SensitiveWordsToString() string {
	return strings.Join(SensitiveWordsSnapshot(), "\n")
}

func SensitiveWordsFromString(s string) {
	words := make([]string, 0)
	for w := range strings.SplitSeq(s, "\n") {
		w = strings.TrimSpace(w)
		if w != "" {
			words = append(words, w)
		}
	}

	sensitiveWordsMu.Lock()
	if strings.Join(SensitiveWords, "\n") == strings.Join(words, "\n") {
		sensitiveWordsMu.Unlock()
		return
	}
	SensitiveWords = words
	sensitiveWordsVersion++
	sensitiveWordsMu.Unlock()
}

// SensitiveWordsSnapshot returns a stable copy for request processing. The
// settings page can update the list while requests are being served.
func SensitiveWordsSnapshot() []string {
	sensitiveWordsMu.RLock()
	words := append([]string(nil), SensitiveWords...)
	sensitiveWordsMu.RUnlock()
	return words
}

func SetCheckSensitiveEnabled(enabled bool) {
	sensitiveWordsMu.Lock()
	CheckSensitiveEnabled = enabled
	sensitiveWordsMu.Unlock()
}

func SetCheckSensitiveOnPromptEnabled(enabled bool) {
	sensitiveWordsMu.Lock()
	CheckSensitiveOnPromptEnabled = enabled
	sensitiveWordsMu.Unlock()
}

func ShouldCheckPromptSensitive() bool {
	sensitiveWordsMu.RLock()
	shouldCheck := CheckSensitiveEnabled && CheckSensitiveOnPromptEnabled
	sensitiveWordsMu.RUnlock()
	return shouldCheck
}

//func ShouldCheckCompletionSensitive() bool {
//	return CheckSensitiveEnabled && CheckSensitiveOnCompletionEnabled
//}
