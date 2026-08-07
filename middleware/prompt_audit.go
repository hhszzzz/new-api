package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/helper"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/prompt_audit_setting"

	"github.com/gin-gonic/gin"
)

// inspectPromptBeforeDistribution runs after authentication and request-rate
// limiting, but before user routing and channel selection. It returns false
// after writing and aborting a rejected request.
func inspectPromptBeforeDistribution(c *gin.Context, modelRequest *ModelRequest) (func(), bool) {
	if c == nil || c.Request == nil || c.Request.Method != http.MethodPost {
		return nil, true
	}
	configured := prompt_audit_setting.GetSetting()
	if !setting.ShouldCheckPromptSensitive() && !configured.AppliesToGroup(common.GetContextKeyString(c, constant.ContextKeyUsingGroup)) {
		return nil, true
	}

	format, taskRequest, supported := promptAuditRequestKind(c.Request.URL.Path)
	if !supported {
		return nil, true
	}

	modelName := ""
	if modelRequest != nil {
		modelName = modelRequest.Model
	}
	var (
		snapshot      relaydto.PromptAuditSnapshot
		sensitiveText string
		isStream      bool
	)
	if taskRequest {
		extracted, requestModel, err := service.ExtractTaskPromptAuditSnapshot(c)
		if err != nil {
			abortPromptAuditInputError(c, err, true)
			return nil, false
		}
		snapshot = extracted
		sensitiveText = snapshot.Text()
		if requestModel != "" {
			modelName = requestModel
		}
		format = types.RelayFormatTask
	} else {
		request, err := helper.GetAndValidateRequest(c, format)
		if err != nil {
			abortPromptAuditInputError(c, err, false)
			return nil, false
		}
		common.SetContextKey(c, constant.ContextKeyValidatedRelayRequest, request)
		snapshot = relaydto.PromptAuditSnapshotOf(request)
		sensitiveText = request.GetSensitiveText()
		isStream = request.IsStream(c.Request)
	}

	cleanup, allowed := beginPromptAuditUserRateLimit(c, format, taskRequest, modelName, isStream)
	if !allowed {
		return cleanup, false
	}

	if setting.ShouldCheckPromptSensitive() {
		if contains, _ := service.CheckSensitiveText(sensitiveText); contains {
			logger.LogWarn(c, "user sensitive words detected")
			apiErr := types.NewError(
				errors.New("sensitive words detected"),
				types.ErrorCodeSensitiveWordsDetected,
				types.ErrOptionWithStatusCode(http.StatusBadRequest),
				types.ErrOptionWithSkipRetry(),
			)
			abortPromptAuditRequest(c, apiErr, taskRequest)
			return cleanup, false
		}
	}

	result, apiErr := service.CheckPromptAudit(c, service.PromptAuditRequest{
		Snapshot: snapshot,
		Protocol: string(format),
		Model:    modelName,
		Stage:    "pre_distribution",
		Stream:   isStream,
	})
	if apiErr != nil {
		service.RecordPromptAuditError(c, result, apiErr, modelName, isStream)
		abortPromptAuditRequest(c, apiErr, taskRequest)
		return cleanup, false
	}
	common.SetContextKey(c, constant.ContextKeyPromptAuditChecked, true)
	return cleanup, true
}

func beginPromptAuditUserRateLimit(c *gin.Context, format types.RelayFormat, taskRequest bool, modelName string, isStream bool) (func(), bool) {
	if taskRequest || !promptAuditUsesUserRateLimit(c.Request.URL.Path, format) {
		return nil, true
	}
	policy := service.UserRateLimitPolicyFromContext(c)
	waitOptions := service.UserConcurrencyWaitOptions{}
	if policy.HasConcurrencyLimit() && isStream {
		helper.EnsureStreamWriteMutex(c)
		waitOptions.Heartbeat = func() error {
			helper.SetEventStreamHeaders(c)
			return helper.PingData(c)
		}
	}
	guard, apiErr := service.BeginUserRequestRateLimit(c, policy, modelName, waitOptions)
	if apiErr != nil {
		abortPromptAuditRequest(c, apiErr, false)
		return nil, false
	}
	common.SetContextKey(c, constant.ContextKeyUserRateLimitApplied, true)
	if isStream && guard.Pacer != nil {
		service.InstallUserStreamPacer(c, guard.Pacer)
	}
	return func() {
		if isStream && guard.Pacer != nil {
			service.InstallUserStreamPacer(c, nil)
		}
		guard.Release()
	}, true
}

func promptAuditUsesUserRateLimit(path string, format types.RelayFormat) bool {
	path = strings.ToLower(path)
	switch format {
	case types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponsesCompaction:
		return true
	case types.RelayFormatClaude:
		return strings.HasSuffix(path, "/messages")
	case types.RelayFormatGemini:
		return strings.HasSuffix(path, ":generatecontent") || strings.HasSuffix(path, ":streamgeneratecontent")
	case types.RelayFormatOpenAI:
		return strings.HasPrefix(path, "/v1/chat/completions") || strings.HasPrefix(path, "/v1/completions")
	default:
		return false
	}
}

func promptAuditRequestKind(path string) (types.RelayFormat, bool, bool) {
	path = strings.TrimSpace(path)
	switch {
	case strings.HasPrefix(path, "/suno/submit/"),
		strings.HasPrefix(path, "/v1/video/generations"),
		strings.HasPrefix(path, "/v1/videos"),
		strings.HasPrefix(path, "/kling/v1/videos/"),
		strings.HasPrefix(path, "/jimeng"):
		return types.RelayFormatTask, true, true
	case strings.Contains(path, "/mj/submit/") || strings.HasPrefix(path, "/mj/submit/"):
		return types.RelayFormatTask, true, true
	case strings.HasPrefix(path, "/pg/chat/completions"):
		return types.RelayFormatOpenAI, false, true
	case strings.HasPrefix(path, "/v1/messages"):
		return types.RelayFormatClaude, false, true
	case strings.HasPrefix(path, "/v1/responses/compact"):
		return types.RelayFormatOpenAIResponsesCompaction, false, true
	case strings.HasPrefix(path, "/v1/responses"):
		return types.RelayFormatOpenAIResponses, false, true
	case strings.HasPrefix(path, "/v1/alpha/search"):
		return types.RelayFormatOpenAIAlphaSearch, false, true
	case strings.HasPrefix(path, "/v1/images/"), strings.HasPrefix(path, "/v1/edits"):
		return types.RelayFormatOpenAIImage, false, true
	case strings.HasPrefix(path, "/v1/audio/"):
		return types.RelayFormatOpenAIAudio, false, true
	case strings.HasPrefix(path, "/v1/rerank"):
		return types.RelayFormatRerank, false, true
	case strings.HasPrefix(path, "/v1/embeddings"):
		return types.RelayFormatEmbedding, false, true
	case strings.HasPrefix(path, "/v1beta/models/"),
		strings.HasPrefix(path, "/v1/models/"),
		strings.HasPrefix(path, "/v1/engines/"):
		return types.RelayFormatGemini, false, true
	case strings.HasPrefix(path, "/v1/chat/completions"),
		strings.HasPrefix(path, "/v1/completions"),
		strings.HasPrefix(path, "/v1/moderations"):
		return types.RelayFormatOpenAI, false, true
	default:
		return types.RelayFormat(""), false, false
	}
}

func abortPromptAuditInputError(c *gin.Context, err error, taskRequest bool) {
	statusCode := http.StatusBadRequest
	if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
		statusCode = http.StatusRequestEntityTooLarge
	}
	apiErr := types.NewErrorWithStatusCode(
		errors.New("invalid request"),
		types.ErrorCodeInvalidRequest,
		statusCode,
		types.ErrOptionWithSkipRetry(),
	)
	abortPromptAuditRequest(c, apiErr, taskRequest)
}

func abortPromptAuditRequest(c *gin.Context, apiErr *types.NewAPIError, taskRequest bool) {
	if apiErr == nil {
		return
	}
	if taskRequest {
		taskErr := service.TaskErrorFromAPIError(apiErr)
		c.AbortWithStatusJSON(taskErr.StatusCode, taskErr)
		return
	}
	abortWithProtocolMessage(c, apiErr.StatusCode, apiErr.Err.Error(), apiErr.GetErrorCode())
}
