package constant

var StreamingTimeout int

// StreamPingMaxDurationSeconds 限制心跳 goroutine 的最长存活时间，防止其无限运行。
// 超过后心跳停止，若上游此时仍在静默，中间的反向代理（如 Cloudflare 的 100s 空闲窗口）
// 会切断连接。长链路上游（Dify 多 Agent 工作流等）需要调大。
var StreamPingMaxDurationSeconds int
var DifyDebug bool
var MaxFileDownloadMB int
var StreamScannerMaxBufferMB int
var ForceStreamOption bool
var CountToken bool
var GetMediaToken bool
var GetMediaTokenNotStream bool
var UpdateTask bool
var MaxRequestBodyMB int
var AnonymousRequestBodyLimitKB int
var AzureDefaultAPIVersion string
var NotifyLimitCount int
var NotificationLimitDurationMinute int
var GenerateDefaultToken bool
var ErrorLogEnabled bool
var TaskQueryLimit int
var TaskTimeoutMinutes int

// temporary variable for sora patch, will be removed in future
var TaskPricePatches []string

// TrustedRedirectDomains is a list of trusted domains for redirect URL validation.
// Domains support subdomain matching (e.g., "example.com" matches "sub.example.com").
var TrustedRedirectDomains []string
