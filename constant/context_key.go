package constant

type ContextKey string

const (
	ContextKeyTokenCountMeta  ContextKey = "token_count_meta"
	ContextKeyPromptTokens    ContextKey = "prompt_tokens"
	ContextKeyEstimatedTokens ContextKey = "estimated_tokens"

	ContextKeyOriginalModel               ContextKey = "original_model"
	ContextKeyRequestStartTime            ContextKey = "request_start_time"
	ContextKeyRequestProtocol             ContextKey = "request_protocol"
	ContextKeyUpstreamProtocol            ContextKey = "upstream_protocol"
	ContextKeyProtocolConverter           ContextKey = "protocol_converter"
	ContextKeyProtocolStateMode           ContextKey = "protocol_state_mode"
	ContextKeyProtocolPlan                ContextKey = "protocol_plan"
	ContextKeyRequestFeatureSet           ContextKey = "request_feature_set"
	ContextKeyProtocolStateParent         ContextKey = "protocol_state_parent"
	ContextKeyProtocolStateSession        ContextKey = "protocol_state_session"
	ContextKeyProtocolStateBinding        ContextKey = "protocol_state_binding"
	ContextKeyProtocolStatePending        ContextKey = "protocol_state_pending"
	ContextKeyProtocolStatePublicID       ContextKey = "protocol_state_public_id"
	ContextKeyProtocolStateForceReplay    ContextKey = "protocol_state_force_replay"
	ContextKeyProtocolRequestNormalized   ContextKey = "protocol_request_normalized"
	ContextKeyProtocolResponseStreamState ContextKey = "protocol_response_stream_state"
	ContextKeyProtocolStreamCompleted     ContextKey = "protocol_stream_completed"
	ContextKeyProtocolAutoAttempt         ContextKey = "protocol_auto_attempt"
	ContextKeyProtocolIncompatibleReason  ContextKey = "protocol_incompatible_reason"
	ContextKeyClientName                  ContextKey = "client_name"
	ContextKeyUpstreamRequestSize         ContextKey = "upstream_request_size"

	/* token related keys */
	ContextKeyTokenUnlimited         ContextKey = "token_unlimited_quota"
	ContextKeyTokenKey               ContextKey = "token_key"
	ContextKeyTokenId                ContextKey = "token_id"
	ContextKeyTokenGroup             ContextKey = "token_group"
	ContextKeyTokenSpecificChannelId ContextKey = "specific_channel_id"
	ContextKeyTokenModelLimitEnabled ContextKey = "token_model_limit_enabled"
	ContextKeyTokenModelLimit        ContextKey = "token_model_limit"
	ContextKeyTokenCrossGroupRetry   ContextKey = "token_cross_group_retry"

	/* channel related keys */
	ContextKeyChannelId                ContextKey = "channel_id"
	ContextKeyChannelName              ContextKey = "channel_name"
	ContextKeyChannelCreateTime        ContextKey = "channel_create_time"
	ContextKeyChannelBaseUrl           ContextKey = "base_url"
	ContextKeyChannelType              ContextKey = "channel_type"
	ContextKeyChannelSetting           ContextKey = "channel_setting"
	ContextKeyChannelOtherSetting      ContextKey = "channel_other_setting"
	ContextKeyChannelParamOverride     ContextKey = "param_override"
	ContextKeyChannelHeaderOverride    ContextKey = "header_override"
	ContextKeyChannelOrganization      ContextKey = "channel_organization"
	ContextKeyChannelAutoBan           ContextKey = "auto_ban"
	ContextKeyChannelModelMapping      ContextKey = "model_mapping"
	ContextKeyChannelStatusCodeMapping ContextKey = "status_code_mapping"
	ContextKeyChannelIsMultiKey        ContextKey = "channel_is_multi_key"
	ContextKeyChannelMultiKeyIndex     ContextKey = "channel_multi_key_index"
	ContextKeyChannelKey               ContextKey = "channel_key"

	ContextKeyAutoGroup           ContextKey = "auto_group"
	ContextKeyAutoGroupIndex      ContextKey = "auto_group_index"
	ContextKeyAutoGroupRetryIndex ContextKey = "auto_group_retry_index"

	/* user related keys */
	ContextKeyUserId                    ContextKey = "id"
	ContextKeyUserSetting               ContextKey = "user_setting"
	ContextKeyUserQuota                 ContextKey = "user_quota"
	ContextKeyUserStatus                ContextKey = "user_status"
	ContextKeyUserEmail                 ContextKey = "user_email"
	ContextKeyUserGroup                 ContextKey = "user_group"
	ContextKeyUserGroups                ContextKey = "user_groups"
	ContextKeyUserModelLimitEnabled     ContextKey = "user_model_limit_enabled"
	ContextKeyUserModelLimit            ContextKey = "user_model_limit"
	ContextKeyUserModelBlocklistEnabled ContextKey = "user_model_blocklist_enabled"
	ContextKeyUserModelBlocklist        ContextKey = "user_model_blocklist"
	ContextKeyUserModelRouteId          ContextKey = "user_model_route_id"
	ContextKeyUserModelRouteTarget      ContextKey = "user_model_route_target"
	ContextKeyUserModelRouteGroup       ContextKey = "user_model_route_group"
	ContextKeyUserModelRouteChannel     ContextKey = "user_model_route_channels"
	ContextKeyUserModelRoutePool        ContextKey = "user_model_route_pool"
	ContextKeyUserModelRoutePrompt      ContextKey = "user_model_route_prompt"
	ContextKeyUsingGroup                ContextKey = "group"
	ContextKeyUserName                  ContextKey = "username"

	ContextKeyLocalCountTokens ContextKey = "local_count_tokens"

	ContextKeySystemPromptOverride ContextKey = "system_prompt_override"

	// ContextKeyFileSourcesToCleanup stores file sources that need cleanup when request ends
	ContextKeyFileSourcesToCleanup ContextKey = "file_sources_to_cleanup"

	// ContextKeyAdminRejectReason stores an admin-only reject/block reason extracted from upstream responses.
	// It is not returned to end users, but can be persisted into consume/error logs for debugging.
	ContextKeyAdminRejectReason ContextKey = "admin_reject_reason"

	// ContextKeyLanguage stores the user's language preference for i18n
	ContextKeyLanguage ContextKey = "language"
	ContextKeyIsStream ContextKey = "is_stream"

	// ContextKeyAuditLogged marks that the current request has already recorded
	// a manage/operation audit log inside the handler. When set, the admin-audit
	// fallback in authHelper (finishAdminAudit) skips its record to avoid
	// duplicate entries.
	ContextKeyAuditLogged ContextKey = "audit_logged"
)
