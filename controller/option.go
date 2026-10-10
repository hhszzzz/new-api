package controller

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/QuantumNous/new-api/setting/group_rate_limit_setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

var completionRatioMetaOptionKeys = []string{
	"ModelPrice",
	"ModelRatio",
	"CompletionRatio",
	"CacheRatio",
	"CreateCacheRatio",
	"ImageRatio",
	"AudioRatio",
	"AudioCompletionRatio",
}

func isPaymentComplianceOptionKey(key string) bool {
	return strings.HasPrefix(key, "payment_setting.compliance_")
}

func isPositiveOptionValue(value string) bool {
	intValue, err := strconv.Atoi(strings.TrimSpace(value))
	if err == nil {
		return intValue > 0
	}
	floatValue, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return err == nil && floatValue > 0
}

func collectModelNamesFromOptionValue(raw string, modelNames map[string]struct{}) {
	if strings.TrimSpace(raw) == "" {
		return
	}

	var parsed map[string]any
	if err := common.UnmarshalJsonStr(raw, &parsed); err != nil {
		return
	}

	for modelName := range parsed {
		modelNames[modelName] = struct{}{}
	}
}

func buildCompletionRatioMetaValue(optionValues map[string]string) string {
	modelNames := make(map[string]struct{})
	for _, key := range completionRatioMetaOptionKeys {
		collectModelNamesFromOptionValue(optionValues[key], modelNames)
	}

	meta := make(map[string]ratio_setting.CompletionRatioInfo, len(modelNames))
	for modelName := range modelNames {
		meta[modelName] = ratio_setting.GetCompletionRatioInfo(modelName)
	}

	jsonBytes, err := common.Marshal(meta)
	if err != nil {
		return "{}"
	}
	return string(jsonBytes)
}

func GetOptions(c *gin.Context) {
	var options []*model.Option
	optionValues := make(map[string]string)
	common.OptionMapRWMutex.Lock()
	for k, v := range common.OptionMap {
		if k == "theme.frontend" || k == "billing_setting.billing_mode" || k == "billing_setting.billing_expr" {
			continue
		}
		value := common.Interface2String(v)
		isSensitiveKey := strings.HasSuffix(k, "Token") ||
			strings.HasSuffix(k, "Secret") ||
			strings.HasSuffix(k, "Key") ||
			strings.HasSuffix(k, "secret") ||
			strings.HasSuffix(k, "api_key")
		if isSensitiveKey {
			continue
		}
		options = append(options, &model.Option{
			Key:   k,
			Value: value,
		})
		if slices.Contains(completionRatioMetaOptionKeys, k) {
			optionValues[k] = value
		}
	}
	common.OptionMapRWMutex.Unlock()
	// Display the same effective expressions used by pricing and settlement,
	// including built-in defaults absent from persisted administrator options.
	for key, values := range map[string]map[string]string{
		"billing_setting.billing_mode": billing_setting.GetBillingModeCopy(),
		"billing_setting.billing_expr": billing_setting.GetBillingExprCopy(),
	} {
		encoded, err := common.Marshal(values)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}
		options = append(options, &model.Option{Key: key, Value: string(encoded)})
	}
	options = append(options, &model.Option{
		Key:   "CompletionRatioMeta",
		Value: buildCompletionRatioMetaValue(optionValues),
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    options,
	})
}

type OptionUpdateRequest struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type OptionBatchUpdateRequest struct {
	Options []OptionUpdateRequest `json:"options"`
}

type GroupRateLimitOptionsRequest struct {
	MemberEnabled              *bool                                           `json:"member_enabled"`
	SharedPoolEnabled          *bool                                           `json:"shared_pool_enabled"`
	ModelRequestRateLimitGroup map[string][2]int                               `json:"model_request_rate_limit_group"`
	Policies                   map[string]group_rate_limit_setting.GroupPolicy `json:"policies"`
}

func normalizeOptionValue(value any) string {
	switch value := value.(type) {
	case bool:
		return common.Interface2String(value)
	case float64:
		return common.Interface2String(value)
	case int:
		return common.Interface2String(value)
	default:
		return fmt.Sprintf("%v", value)
	}
}

func validateModelPricingOption(key string, value string) error {
	if !ratio_setting.IsPricingOptionKey(key) {
		return i18n.NewError(i18n.MsgOptionPricingKeyUnsupported, map[string]any{"Key": key})
	}
	if err := ratio_setting.ValidatePricingOptionsByJSONString(map[string]string{key: value}); err != nil {
		return i18n.NewError(i18n.MsgOptionPricingValueInvalid, map[string]any{"Key": key, "Error": err.Error()})
	}
	return nil
}

func normalizeModelPricingOptions(options []OptionUpdateRequest) (map[string]string, error) {
	values := make(map[string]string, len(options))
	for _, option := range options {
		if _, duplicate := values[option.Key]; duplicate {
			return nil, i18n.NewError(i18n.MsgOptionPricingKeyDuplicate, map[string]any{"Key": option.Key})
		}
		value := normalizeOptionValue(option.Value)
		if err := validateModelPricingOption(option.Key, value); err != nil {
			return nil, err
		}
		values[option.Key] = value
	}
	return values, nil
}

func UpdatePasskeyDomains(c *gin.Context) {
	var request struct {
		RPID                *string `json:"rp_id"`
		LegacyRPIDs         *string `json:"legacy_rp_ids"`
		Origins             *string `json:"origins"`
		Preview             bool    `json:"preview"`
		RemovalConfirmation string  `json:"removal_confirmation"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.RPID == nil || request.LegacyRPIDs == nil || request.Origins == nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	change, err := model.UpdatePasskeyDomainOptions(map[string]string{
		"passkey.rp_id": *request.RPID, "passkey.legacy_rp_ids": *request.LegacyRPIDs, "passkey.origins": *request.Origins,
	}, request.Preview, request.RemovalConfirmation)
	if err != nil {
		writePasskeyDomainSettingsError(c, err)
		if !request.Preview {
			recordPasskeyDomainAudit(c, change, request.RemovalConfirmation != "", err)
		}
		return
	}
	if !request.Preview {
		recordPasskeyDomainAudit(c, change, request.RemovalConfirmation != "", nil)
	}
	common.ApiSuccess(c, change)
}

func writePasskeyDomainSettingsError(c *gin.Context, err error) {
	var removal *model.PasskeyDomainRemovalError
	if errors.As(err, &removal) {
		c.JSON(http.StatusConflict, gin.H{
			"success": false, "code": "PASSKEY_RP_ID_REMOVAL_CONFIRMATION_REQUIRED",
			"message": i18n.T(c, i18n.MsgPasskeyRPIDRemovalConfirmation), "data": removal.Change,
		})
		return
	}
	if errors.Is(err, system_setting.ErrPasskeyRPIDInvalid) {
		writeSecurityOperationError(c, err)
		return
	}
	common.ApiError(c, err)
}

func UpdateOption(c *gin.Context) {
	var option OptionUpdateRequest
	err := common.DecodeJson(c.Request.Body, &option)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": common.TranslateMessage(c, i18n.MsgInvalidParams),
		})
		return
	}
	if option.Key == "prompt_audit.shared_content_enabled" {
		common.ApiErrorMsg(c, "shared audit storage must be changed through prompt audit settings after migration")
		return
	}
	option.Value = normalizeOptionValue(option.Value)
	if ratio_setting.IsPricingOptionKey(option.Key) {
		if err := validateModelPricingOption(option.Key, option.Value.(string)); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	switch option.Key {
	case "QuotaForInviter", "QuotaForInvitee":
		if isPositiveOptionValue(option.Value.(string)) && !operation_setting.IsPaymentComplianceConfirmed() {
			common.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
			return
		}
	default:
		if isPaymentComplianceOptionKey(option.Key) {
			common.ApiErrorI18n(c, i18n.MsgOptionComplianceFieldReadonly)
			return
		}
	}
	if option.Key == "TaskPublicAddress" && option.Value.(string) != "" {
		if err := service.ValidateTaskArtifactBaseURL(option.Value.(string)); err != nil {
			common.ApiErrorMsg(c, err.Error())
			return
		}
	}
	switch option.Key {
	case "GitHubOAuthEnabled":
		if option.Value == "true" && common.GitHubClientId == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionOAuthNotConfigured, map[string]any{"Provider": "GitHub OAuth"}),
			})
			return
		}
	case "discord.enabled":
		if option.Value == "true" && system_setting.GetDiscordSettings().ClientId == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionOAuthNotConfigured, map[string]any{"Provider": "Discord OAuth"}),
			})
			return
		}
	case "oidc.enabled":
		if option.Value == "true" && system_setting.GetOIDCSettings().ClientId == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionOAuthNotConfigured, map[string]any{"Provider": "OIDC"}),
			})
			return
		}
	case "LinuxDOOAuthEnabled":
		if option.Value == "true" && common.LinuxDOClientId == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionOAuthNotConfigured, map[string]any{"Provider": "LinuxDO OAuth"}),
			})
			return
		}
	case "EmailDomainRestrictionEnabled":
		if option.Value == "true" && len(common.EmailDomainWhitelist) == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionEmailDomainRestrictionNotConfigured),
			})
			return
		}
	case "WeChatAuthEnabled":
		if option.Value == "true" && common.WeChatServerAddress == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionWeChatNotConfigured),
			})
			return
		}
	case "TurnstileCheckEnabled":
		if option.Value == "true" && common.TurnstileSiteKey == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionTurnstileNotConfigured),
			})

			return
		}
	case "TelegramOAuthEnabled":
		if option.Value == "true" && !system_setting.GetTelegramSettings().IsConfigured() {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"code":    "TELEGRAM_OAUTH_NOT_CONFIGURED",
				"message": common.TranslateMessage(c, i18n.MsgOptionTelegramNotConfigured),
			})
			return
		}
	case "theme.frontend":
		if option.Value != "default" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionClassicThemeRemoved),
			})
			return
		}
	case "GroupRatio":
		err = ratio_setting.CheckGroupRatio(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "gemini.safety_settings":
		err = model_setting.ValidateGeminiSafetySettings(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "claude.default_max_tokens":
		err = model_setting.ValidateClaudeDefaultMaxTokens(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case operation_setting.ToolPriceOptionKey:
		err = operation_setting.ValidateToolPricesJSON(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "ModelRequestRateLimitGroup":
		err = setting.CheckModelRequestRateLimitGroup(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "AutomaticDisableStatusCodes":
		_, err = operation_setting.ParseHTTPStatusCodeRanges(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "AutomaticRetryStatusCodes":
		_, err = operation_setting.ParseHTTPStatusCodeRanges(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "billing_setting.billing_expr":
		expressions := make(map[string]string)
		if err = common.UnmarshalJsonStr(option.Value.(string), &expressions); err != nil {
			common.ApiErrorI18n(c, i18n.MsgOptionBillingExprInvalid, map[string]any{"Error": err.Error()})
			return
		}
		models := make([]string, 0, len(expressions))
		for modelName := range expressions {
			models = append(models, modelName)
		}
		sort.Strings(models)
		storedVariants := billing_setting.GetPluginBillingExprCopy()
		for _, modelName := range models {
			variants := make(map[string]any)
			for key, expression := range storedVariants {
				if plugin, name, ok := billing_setting.SplitPluginBillingExprKey(key); ok && name == modelName {
					variants[plugin] = expression
				}
			}
			err = model.ValidateModelPricing(modelName, model.PricingValues{
				"billing_setting.billing_expr":          expressions[modelName],
				billing_setting.PluginBillingExprOption: variants,
			})
			if err != nil {
				common.ApiErrorI18n(c, i18n.MsgOptionBillingExprModelInvalid, map[string]any{"Model": modelName, "Error": err.Error()})
				return
			}
		}
	case billing_setting.PluginBillingExprOption:
		var expressions map[string]string
		if err = common.UnmarshalJsonStr(option.Value.(string), &expressions); err != nil || expressions == nil {
			common.ApiErrorI18n(c, i18n.MsgOptionPluginBillingExprInvalid)
			return
		}
		for key, expression := range expressions {
			plugin, name, valid := billing_setting.SplitPluginBillingExprKey(key)
			if !valid {
				common.ApiErrorI18n(c, i18n.MsgOptionPluginBillingExprKeyInvalid, map[string]any{"Key": key})
				return
			}
			if err = model.ValidateModelPricing(name, model.PricingValues{
				billing_setting.PluginBillingExprOption: map[string]any{plugin: expression},
			}); err != nil {
				common.ApiErrorMsg(c, err.Error())
				return
			}
		}
	case "console_setting.api_info":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "ApiInfo")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "console_setting.announcements":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "Announcements")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "console_setting.faq":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "FAQ")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "console_setting.uptime_kuma_groups":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "UptimeKumaGroups")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	}
	if strings.HasPrefix(option.Key, "log_diagnostic_setting.") {
		switch option.Key {
		case "log_diagnostic_setting.record_ip", "log_diagnostic_setting.record_headers":
			if option.Value != "true" && option.Value != "false" {
				common.ApiErrorI18n(c, i18n.MsgOptionDiagnosticSwitchInvalid)
				return
			}
		case "log_diagnostic_setting.extra_headers":
			var headers []string
			if err := common.UnmarshalJsonStr(option.Value.(string), &headers); err != nil {
				common.ApiErrorI18n(c, i18n.MsgOptionExtraHeadersInvalid)
				return
			}
			if err := operation_setting.ValidateLogDiagnosticHeaders(headers); err != nil {
				common.ApiErrorMsg(c, err.Error())
				return
			}
		default:
			common.ApiErrorI18n(c, i18n.MsgOptionUnsupportedLogDiagnostic)
			return
		}
	}
	if strings.HasPrefix(option.Key, "client_policy_setting.") {
		if option.Key != operation_setting.ClientPolicyRulesOptionKey && option.Key != operation_setting.ClientPolicyGroupsOptionKey {
			common.ApiErrorI18n(c, i18n.MsgOptionUnsupportedClientPolicy)
			return
		}
		candidate := *operation_setting.GetClientPolicySettingSnapshot()
		if option.Key == operation_setting.ClientPolicyRulesOptionKey {
			var rules []operation_setting.ClientIdentificationRule
			if err := common.UnmarshalJsonStr(option.Value.(string), &rules); err != nil {
				common.ApiErrorI18n(c, i18n.MsgOptionClientRulesInvalid)
				return
			}
			candidate.Rules = rules
		} else {
			var groupPolicies map[string]operation_setting.ClientAccessPolicy
			if err := common.UnmarshalJsonStr(option.Value.(string), &groupPolicies); err != nil {
				common.ApiErrorI18n(c, i18n.MsgOptionGroupClientPolicyInvalid)
				return
			}
			candidate.GroupPolicies = groupPolicies
		}
		if err := operation_setting.ValidateClientPolicySetting(candidate); err != nil {
			common.ApiErrorMsg(c, err.Error())
			return
		}
	}
	if model.IsPasskeyDomainOption(option.Key) {
		change, updateErr := model.UpdatePasskeyDomainOptions(map[string]string{option.Key: option.Value.(string)}, false, "")
		if updateErr != nil {
			writePasskeyDomainSettingsError(c, updateErr)
			recordPasskeyDomainAudit(c, change, false, updateErr)
			return
		}
		recordPasskeyDomainAudit(c, change, false, nil)
		common.ApiSuccess(c, change)
		return
	}
	err = model.UpdateOption(option.Key, option.Value.(string))
	if err != nil {
		if errors.Is(err, system_setting.ErrPasskeyRPIDInvalid) {
			writeSecurityOperationError(c, err)
		} else {
			common.ApiError(c, err)
		}
		return
	}
	// 出于安全考虑只记录被修改的配置项名称，不记录配置值（可能含密钥等敏感信息）。
	recordManageAudit(c, "option.update", map[string]any{
		"key": option.Key,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func UpdateModelPricingOptions(c *gin.Context) {
	var request OptionBatchUpdateRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if len(request.Options) == 0 || len(request.Options) > ratio_setting.PricingOptionKeyCount() {
		common.ApiErrorI18n(c, i18n.MsgOptionPricingCountInvalid)
		return
	}

	values, err := normalizeModelPricingOptions(request.Options)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if err := model.UpdateOptionsBulk(values); err != nil {
		common.ApiError(c, err)
		return
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	recordManageAudit(c, "option.model_pricing.update", map[string]interface{}{
		"keys": keys,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func UpdateClientPolicyOptions(c *gin.Context) {
	var request operation_setting.ClientPolicySetting
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := model.UpdateClientPolicySetting(request); err != nil {
		common.ApiError(c, err)
		return
	}

	recordManageAudit(c, "option.client_policy.update", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func UpdateGroupRateLimitOptions(c *gin.Context) {
	var request GroupRateLimitOptionsRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if request.MemberEnabled == nil || request.SharedPoolEnabled == nil || request.ModelRequestRateLimitGroup == nil || request.Policies == nil {
		common.ApiErrorI18n(c, i18n.MsgOptionGroupRateLimitIncomplete)
		return
	}
	if err := model.UpdateGroupRateLimitOptions(
		*request.MemberEnabled,
		*request.SharedPoolEnabled,
		request.ModelRequestRateLimitGroup,
		request.Policies,
	); err != nil {
		common.ApiError(c, err)
		return
	}

	groups := make(map[string]struct{}, len(request.ModelRequestRateLimitGroup)+len(request.Policies))
	for group := range request.ModelRequestRateLimitGroup {
		groups[group] = struct{}{}
	}
	for group := range request.Policies {
		groups[group] = struct{}{}
	}
	groupNames := make([]string, 0, len(groups))
	for group := range groups {
		groupNames = append(groupNames, group)
	}
	sort.Strings(groupNames)
	recordManageAudit(c, "option.group_rate_limits.update", map[string]interface{}{
		"member_enabled":                 *request.MemberEnabled,
		"shared_pool_enabled":            *request.SharedPoolEnabled,
		"groups":                         groupNames,
		"model_request_rate_limit_group": request.ModelRequestRateLimitGroup,
		"policies":                       request.Policies,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}
