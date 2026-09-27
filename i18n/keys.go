package i18n

const MsgTaskPluginUnknownMetaField = "task_plugin.unknown_meta_field"
const MsgProbeRequestBlocked = "prompt_audit.probe_request_blocked"
const MsgSensitiveWordsDetected = "prompt_audit.sensitive_words_detected"
const MsgPromptAuditServiceUnavailable = "prompt_audit.service_unavailable"
const MsgOutputAuditSensitiveWordsDetected = "output_audit.sensitive_words_detected"
const MsgOutputAuditServiceUnavailable = "output_audit.service_unavailable"

// Message keys for i18n translations
// Use these constants instead of hardcoded strings

// Common error messages
const (
	MsgInvalidParams     = "common.invalid_params"
	MsgDatabaseError     = "common.database_error"
	MsgRetryLater        = "common.retry_later"
	MsgGenerateFailed    = "common.generate_failed"
	MsgNotFound          = "common.not_found"
	MsgUnauthorized      = "common.unauthorized"
	MsgForbidden         = "common.forbidden"
	MsgInvalidId         = "common.invalid_id"
	MsgIdEmpty           = "common.id_empty"
	MsgFeatureDisabled   = "common.feature_disabled"
	MsgOperationSuccess  = "common.operation_success"
	MsgOperationFailed   = "common.operation_failed"
	MsgUpdateSuccess     = "common.update_success"
	MsgUpdateFailed      = "common.update_failed"
	MsgCreateSuccess     = "common.create_success"
	MsgCreateFailed      = "common.create_failed"
	MsgDeleteSuccess     = "common.delete_success"
	MsgDeleteFailed      = "common.delete_failed"
	MsgAlreadyExists     = "common.already_exists"
	MsgNameCannotBeEmpty = "common.name_cannot_be_empty"
	MsgBatchTooMany      = "common.batch_too_many"

	MsgServerRunning              = "common.server_running"
	MsgInvalidParamsDetail        = "common.invalid_params_detail"
	MsgDatabaseConnectionFailed   = "common.database_connection_failed"
	MsgSearchConsecutiveWildcards = "common.search_consecutive_wildcards"
	MsgSearchTooManyWildcards     = "common.search_too_many_wildcards"
	MsgSearchKeywordTooShort      = "common.search_keyword_too_short"
	MsgSMTPNotConfigured          = "common.smtp_not_configured"
)

// Auth middleware messages
const (
	MsgAuthNotLoggedIn           = "auth.not_logged_in"
	MsgAuthAccessTokenInvalid    = "auth.access_token_invalid"
	MsgAuthUserInfoInvalid       = "auth.user_info_invalid"
	MsgAuthUserIdNotProvided     = "auth.user_id_not_provided"
	MsgAuthUserIdFormatError     = "auth.user_id_format_error"
	MsgAuthUserIdMismatch        = "auth.user_id_mismatch"
	MsgAuthUserBanned            = "auth.user_banned"
	MsgAuthInsufficientPrivilege = "auth.insufficient_privilege"

	MsgAuthTokenNoValidGroup     = "auth.token_no_valid_group"
	MsgAuthClientIPUnparseable   = "auth.client_ip_unparseable"
	MsgAuthIPNotAllowed          = "auth.ip_not_allowed"
	MsgAuthGroupAccessDenied     = "auth.group_access_denied"
	MsgAuthGroupDeprecated       = "auth.group_deprecated"
	MsgAuthSpecificChannelDenied = "auth.specific_channel_denied"
)

// Security proof (verification) messages
const (
	MsgSecurityProofInvalid         = "security_proof.invalid"
	MsgSecurityProofRequired        = "security_proof.required"
	MsgSecurityProofExpired         = "security_proof.expired"
	MsgSecurityProofScopeMismatch   = "security_proof.scope_mismatch"
	MsgSecurityProofMethodMismatch  = "security_proof.method_mismatch"
	MsgSecurityProofConsumed        = "security_proof.consumed"
	MsgSecurityProofContextMismatch = "security_proof.context_mismatch"

	MsgSecurityProofAuthMethodUnsupported = "security_proof.auth_method_unsupported"
	MsgSecurityProofFlowExpired           = "security_proof.flow_expired"
)

// Turnstile verification messages
const (
	MsgTurnstileTokenEmpty      = "turnstile.token_empty"
	MsgTurnstileVerifyFailed    = "turnstile.verify_failed"
	MsgTurnstileRequestFailed   = "turnstile.request_failed"
	MsgTurnstileResponseInvalid = "turnstile.response_invalid"
)

// Token related messages
const (
	MsgTokenNameTooLong          = "token.name_too_long"
	MsgTokenQuotaNegative        = "token.quota_negative"
	MsgTokenQuotaExceedMax       = "token.quota_exceed_max"
	MsgTokenGenerateFailed       = "token.generate_failed"
	MsgTokenGetInfoFailed        = "token.get_info_failed"
	MsgTokenExpiredCannotEnable  = "token.expired_cannot_enable"
	MsgTokenExhaustedCannotEable = "token.exhausted_cannot_enable"
	MsgTokenInvalid              = "token.invalid"
	MsgTokenNotProvided          = "token.not_provided"
	MsgTokenExpired              = "token.expired"
	MsgTokenExhausted            = "token.exhausted"
	MsgTokenStatusUnavailable    = "token.status_unavailable"
	MsgTokenDbError              = "token.db_error"
	MsgTokenAutoGroupsTooMany    = "token.auto_groups_too_many"
	MsgTokenAutoGroupsDuplicate  = "token.auto_groups_duplicate"
	MsgTokenAutoGroupsInvalid    = "token.auto_groups_invalid"

	MsgTokenStatusInvalid        = "token.status_invalid"
	MsgTokenAuthorizationMissing = "token.authorization_missing"
	MsgTokenMaxCountReached      = "token.max_count_reached"
	MsgTokenGroupRequired        = "token.group_required"
	MsgTokenBearerRequired       = "token.bearer_required"
	MsgTokenCountFailed          = "token.count_failed"
	MsgTokenSearchExactOnly      = "token.search_exact_only"
	MsgTokenSearchFailed         = "token.search_failed"
)

// Redemption related messages
const (
	MsgRedemptionNameLength        = "redemption.name_length"
	MsgRedemptionCountPositive     = "redemption.count_positive"
	MsgRedemptionCountMax          = "redemption.count_max"
	MsgRedemptionCreateFailed      = "redemption.create_failed"
	MsgRedemptionInvalid           = "redemption.invalid"
	MsgRedemptionUsed              = "redemption.used"
	MsgRedemptionExpired           = "redemption.expired"
	MsgRedemptionFailed            = "redemption.failed"
	MsgRedemptionNotProvided       = "redemption.not_provided"
	MsgRedemptionExpireTimeInvalid = "redemption.expire_time_invalid"
)

// User related messages
const (
	MsgUserPasswordLoginDisabled     = "user.password_login_disabled"
	MsgUserRegisterDisabled          = "user.register_disabled"
	MsgUserPasswordRegisterDisabled  = "user.password_register_disabled"
	MsgUserUsernameOrPasswordEmpty   = "user.username_or_password_empty"
	MsgUserUsernameOrPasswordError   = "user.username_or_password_error"
	MsgUserEmailOrPasswordEmpty      = "user.email_or_password_empty"
	MsgUserExists                    = "user.exists"
	MsgUserNotExists                 = "user.not_exists"
	MsgUserDisabled                  = "user.disabled"
	MsgUserSessionSaveFailed         = "user.session_save_failed"
	MsgUserRequire2FA                = "user.require_2fa"
	MsgUserEmailVerificationRequired = "user.email_verification_required"
	MsgUserVerificationCodeError     = "user.verification_code_error"
	MsgUserEmailAlreadyTaken         = "user.email_already_taken"
	MsgUserPasswordUnset             = "user.password_unset"
	MsgUserPasswordResetLinkInvalid  = "user.password_reset_link_invalid"
	MsgUserInputInvalid              = "user.input_invalid"
	MsgUserNoPermissionSameLevel     = "user.no_permission_same_level"
	MsgUserNoPermissionHigherLevel   = "user.no_permission_higher_level"
	MsgUserCannotCreateHigherLevel   = "user.cannot_create_higher_level"
	MsgUserCannotDeleteRootUser      = "user.cannot_delete_root_user"
	MsgUserCannotDisableRootUser     = "user.cannot_disable_root_user"
	MsgUserCannotDemoteRootUser      = "user.cannot_demote_root_user"
	MsgUserAlreadyAdmin              = "user.already_admin"
	MsgUserAlreadyCommon             = "user.already_common"
	MsgUserAdminCannotPromote        = "user.admin_cannot_promote"
	MsgUserOriginalPasswordError     = "user.original_password_error"
	MsgUserInviteQuotaInsufficient   = "user.invite_quota_insufficient"
	MsgUserTransferQuotaMinimum      = "user.transfer_quota_minimum"
	MsgUserTransferSuccess           = "user.transfer_success"
	MsgUserTransferFailed            = "user.transfer_failed"
	MsgUserTopUpProcessing           = "user.topup_processing"
	MsgUserRegisterFailed            = "user.register_failed"
	MsgUserDefaultTokenFailed        = "user.default_token_failed"
	MsgUserAffCodeEmpty              = "user.aff_code_empty"
	MsgUserEmailEmpty                = "user.email_empty"
	MsgUserGitHubIdEmpty             = "user.github_id_empty"
	MsgUserDiscordIdEmpty            = "user.discord_id_empty"
	MsgUserOidcIdEmpty               = "user.oidc_id_empty"
	MsgUserWeChatIdEmpty             = "user.wechat_id_empty"
	MsgUserTelegramIdEmpty           = "user.telegram_id_empty"
	MsgUserTelegramNotBound          = "user.telegram_not_bound"
	MsgUserLinuxDOIdEmpty            = "user.linux_do_id_empty"
	MsgUserQuotaChangeZero           = "user.quota_change_zero"

	MsgUserPolicyGroupInvalid                = "user.policy_group_invalid"
	MsgUserPolicyGroupRequired               = "user.policy_group_required"
	MsgUserPolicyPrimaryGroupInvalid         = "user.policy_primary_group_invalid"
	MsgUserPolicyTopupGroupInvalid           = "user.policy_topup_group_invalid"
	MsgUserRateLimitRange                    = "user.rate_limit_range"
	MsgUserQuotaCapNegative                  = "user.quota_cap_negative"
	MsgUserQuotaCapTooLarge                  = "user.quota_cap_too_large"
	MsgUserCheckinQuotaPairRequired          = "user.checkin_quota_pair_required"
	MsgUserCheckinQuotaNegative              = "user.checkin_quota_negative"
	MsgUserCheckinQuotaMaxBelowMin           = "user.checkin_quota_max_below_min"
	MsgUserCheckinQuotaTooLarge              = "user.checkin_quota_too_large"
	MsgUserModelNotPublic                    = "user.model_not_public"
	MsgUserAdminPermissionRootOnly           = "user.admin_permission_root_only"
	MsgUserRedeemQuotaCapExceeded            = "user.redeem_quota_cap_exceeded"
	MsgUserBatchNoUsersSelected              = "user.batch_no_users_selected"
	MsgUserBatchNoChangesSelected            = "user.batch_no_changes_selected"
	MsgUserBatchInvalidListMode              = "user.batch_invalid_list_mode"
	MsgUserBatchEmptyModelList               = "user.batch_empty_model_list"
	MsgUserBatchInvalidCheckinMode           = "user.batch_invalid_checkin_mode"
	MsgUserBatchInvalidCheckinQuotaMode      = "user.batch_invalid_checkin_quota_mode"
	MsgUserBatchEmptyCheckin                 = "user.batch_empty_checkin"
	MsgUserBatchQuotaCapValueRequired        = "user.batch_quota_cap_value_required"
	MsgUserBatchInvalidQuotaCapMode          = "user.batch_invalid_quota_cap_mode"
	MsgUserBatchRouteRequired                = "user.batch_route_required"
	MsgUserRateLimitKeepNoValue              = "user.rate_limit_keep_no_value"
	MsgUserRateLimitClearNoValue             = "user.rate_limit_clear_no_value"
	MsgUserRateLimitCustomValueRequired      = "user.rate_limit_custom_value_required"
	MsgUserRateLimitInvalidMode              = "user.rate_limit_invalid_mode"
	MsgUserExecutionGroupNotExists           = "user.execution_group_not_exists"
	MsgUserModelRouteOverlap                 = "user.model_route_overlap"
	MsgUserModelRouteIncomplete              = "user.model_route_incomplete"
	MsgUserModelRouteTargetConcrete          = "user.model_route_target_concrete"
	MsgUserModelRouteInjectPromptTooLong     = "user.model_route_inject_prompt_too_long"
	MsgUserModelRouteGroupRequired           = "user.model_route_group_required"
	MsgUserModelRouteGroupNotAuthorized      = "user.model_route_group_not_authorized"
	MsgUserModelRouteChannelUnavailable      = "user.model_route_channel_unavailable"
	MsgUserModelRouteChannelModelUnsupported = "user.model_route_channel_model_unsupported"
	MsgUserBatchNoPermission                 = "user.batch_no_permission"
	MsgUserLimitNameRPM                      = "user.limit_name_rpm"
	MsgUserLimitNameConcurrency              = "user.limit_name_concurrency"
	MsgUserLimitNameStreamTPS                = "user.limit_name_stream_tps"
	MsgUserLimitNameFirstTokenDelay          = "user.limit_name_first_token_delay"
	MsgUserBatchCheckinCustomQuotaRequired   = "user.batch_checkin_custom_quota_required"
	MsgUserModelRouteSourceNotPublic         = "user.model_route_source_not_public"
	MsgUserTransferExceedsQuotaCap           = "user.transfer_exceeds_quota_cap"
	MsgUserPasswordLength                    = "user.password_length"
	MsgUserDisplayNameTooLong                = "user.display_name_too_long"
	MsgUserEmailTooLong                      = "user.email_too_long"
)

// Quota related messages
const (
	MsgQuotaNegative        = "quota.negative"
	MsgQuotaExceedMax       = "quota.exceed_max"
	MsgQuotaInsufficient    = "quota.insufficient"
	MsgQuotaWarningInvalid  = "quota.warning_invalid"
	MsgQuotaThresholdGtZero = "quota.threshold_gt_zero"
)

// Subscription related messages
const (
	MsgSubscriptionNotEnabled       = "subscription.not_enabled"
	MsgSubscriptionTitleEmpty       = "subscription.title_empty"
	MsgSubscriptionPriceNegative    = "subscription.price_negative"
	MsgSubscriptionPriceMax         = "subscription.price_max"
	MsgSubscriptionPurchaseLimitNeg = "subscription.purchase_limit_negative"
	MsgSubscriptionQuotaNegative    = "subscription.quota_negative"
	MsgSubscriptionGroupNotExists   = "subscription.group_not_exists"
	MsgSubscriptionResetCycleGtZero = "subscription.reset_cycle_gt_zero"
	MsgSubscriptionPurchaseMax      = "subscription.purchase_max"
	MsgSubscriptionInvalidId        = "subscription.invalid_id"
	MsgSubscriptionInvalidUserId    = "subscription.invalid_user_id"

	MsgSubscriptionPeriodQuotaNegative        = "subscription.period_quota_negative"
	MsgSubscriptionDowngradeGroupUnsupported  = "subscription.downgrade_group_unsupported"
	MsgSubscriptionLegacyDowngradeGroupLocked = "subscription.legacy_downgrade_group_locked"
	MsgSubscriptionNoPermissionManage         = "subscription.no_permission_manage"
	MsgSubscriptionPlanDisabled               = "subscription.plan_disabled"
	MsgSubscriptionInvalidBatchAction         = "subscription.invalid_batch_action"
	MsgSubscriptionNoActiveSubscription       = "subscription.no_active_subscription"
	MsgSubscriptionPlanNotFound               = "subscription.plan_not_found"
	MsgSubscriptionAdminAssignOnly            = "subscription.admin_assign_only"
	MsgSubscriptionBalancePayNotAllowed       = "subscription.balance_pay_not_allowed"
	MsgSubscriptionBalanceInsufficient        = "subscription.balance_insufficient"
	MsgSubscriptionExpiredCannotResume        = "subscription.expired_cannot_resume"
)

// Payment related messages
const (
	MsgPaymentNotConfigured      = "payment.not_configured"
	MsgPaymentMethodNotExists    = "payment.method_not_exists"
	MsgPaymentCallbackError      = "payment.callback_error"
	MsgPaymentCreateFailed       = "payment.create_failed"
	MsgPaymentStartFailed        = "payment.start_failed"
	MsgPaymentAmountTooLow       = "payment.amount_too_low"
	MsgPaymentStripeNotConfig    = "payment.stripe_not_configured"
	MsgPaymentWebhookNotConfig   = "payment.webhook_not_configured"
	MsgPaymentPriceIdNotConfig   = "payment.price_id_not_configured"
	MsgPaymentCreemNotConfig     = "payment.creem_not_configured"
	MsgPaymentComplianceRequired = "payment.compliance_required"

	MsgPaymentWaffoPancakeProductNotConfigured = "payment.waffo_pancake_product_not_configured"
	MsgPaymentWaffoPancakeNotConfigured        = "payment.waffo_pancake_not_configured"
	MsgPaymentSessionAuthRequired              = "payment.session_auth_required"
	MsgPaymentComplianceConfirmRequired        = "payment.compliance_confirm_required"
)

// Topup related messages
const (
	MsgTopupNotProvided    = "topup.not_provided"
	MsgTopupOrderNotExists = "topup.order_not_exists"
	MsgTopupOrderStatus    = "topup.order_status"
	MsgTopupFailed         = "topup.failed"
	MsgTopupInvalidQuota   = "topup.invalid_quota"

	MsgTopupAmountBelowMin            = "topup.amount_below_min"
	MsgTopupAmountTooLow              = "topup.amount_too_low"
	MsgTopupUnsupportedChannel        = "topup.unsupported_channel"
	MsgTopupProductRequired           = "topup.product_required"
	MsgTopupProductConfigError        = "topup.product_config_error"
	MsgTopupProductNotExists          = "topup.product_not_exists"
	MsgTopupCreemPriceMismatch        = "topup.creem_price_mismatch"
	MsgTopupReadRequestFailed         = "topup.read_request_failed"
	MsgTopupAmountAboveMax            = "topup.amount_above_max"
	MsgTopupSuccessUrlUntrusted       = "topup.success_url_untrusted"
	MsgTopupCancelUrlUntrusted        = "topup.cancel_url_untrusted"
	MsgTopupWaffoNotEnabled           = "topup.waffo_not_enabled"
	MsgTopupUnsupportedPayMethod      = "topup.unsupported_pay_method"
	MsgTopupConfigError               = "topup.config_error"
	MsgTopupSaveConfigFailed          = "topup.save_config_failed"
	MsgTopupPancakeCredentialsMissing = "topup.pancake_credentials_missing"
	MsgTopupCatalogFetchFailed        = "topup.catalog_fetch_failed"
	MsgTopupPlanAmountEmpty           = "topup.plan_amount_empty"
	MsgTopupPancakeSetupIncomplete    = "topup.pancake_setup_incomplete"
	MsgTopupPlanProductCreateFailed   = "topup.plan_product_create_failed"
	MsgTopupProductListFetchFailed    = "topup.product_list_fetch_failed"
	MsgTopupPancakeConfigIncomplete   = "topup.pancake_config_incomplete"
	MsgTopupAmountMustBePositive      = "topup.amount_must_be_positive"
	MsgTopupPriceConfigInvalid        = "topup.price_config_invalid"
	MsgTopupDiscountConfigInvalid     = "topup.discount_config_invalid"
	MsgTopupAmountConfigInvalid       = "topup.amount_config_invalid"
	MsgTopupAmountOutOfRange          = "topup.amount_out_of_range"
	MsgTopupQuotaOutOfRange           = "topup.quota_out_of_range"
	MsgTopupQuotaMustBePositive       = "topup.quota_must_be_positive"
	MsgTopupQuotaLimitExceeded        = "topup.quota_limit_exceeded"
	MsgTopupSearchFailed              = "topup.search_failed"
)

// Channel related messages
const (
	MsgChannelNotExists          = "channel.not_exists"
	MsgChannelIdFormatError      = "channel.id_format_error"
	MsgChannelNoAvailableKey     = "channel.no_available_key"
	MsgChannelGetListFailed      = "channel.get_list_failed"
	MsgChannelGetTagsFailed      = "channel.get_tags_failed"
	MsgChannelGetKeyFailed       = "channel.get_key_failed"
	MsgChannelGetOllamaFailed    = "channel.get_ollama_failed"
	MsgChannelQueryFailed        = "channel.query_failed"
	MsgChannelNoValidUpstream    = "channel.no_valid_upstream"
	MsgChannelUpstreamSaturated  = "channel.upstream_saturated"
	MsgChannelGetAvailableFailed = "channel.get_available_failed"

	MsgChannelTypeStatsFailed                  = "channel.type_stats_failed"
	MsgChannelRefreshCredentialFailed          = "channel.refresh_credential_failed"
	MsgChannelCredentialRefreshed              = "channel.credential_refreshed"
	MsgChannelVertexBatchKeysInvalid           = "channel.vertex_batch_keys_invalid"
	MsgChannelVertexKeyEncodeFailed            = "channel.vertex_key_encode_failed"
	MsgChannelVertexKeysEmpty                  = "channel.vertex_keys_empty"
	MsgChannelTaskPluginBindPermissionRequired = "channel.task_plugin_bind_permission_required"
	MsgChannelUnsupportedAddMode               = "channel.unsupported_add_mode"
	MsgChannelTagRequired                      = "channel.tag_required"
	MsgChannelParamOverrideInvalidJson         = "channel.param_override_invalid_json"
	MsgChannelHeaderOverrideInvalidJson        = "channel.header_override_invalid_json"
	MsgChannelGetInfoFailed                    = "channel.get_info_failed"
	MsgChannelCopyInvalidSettings              = "channel.copy_invalid_settings"
	MsgChannelCopyFailed                       = "channel.copy_failed"
	MsgChannelNotMultiKey                      = "channel.not_multi_key"
	MsgChannelKeyIndexRequired                 = "channel.key_index_required"
	MsgChannelKeyIndexOutOfRange               = "channel.key_index_out_of_range"
	MsgChannelKeyDisabled                      = "channel.key_disabled"
	MsgChannelKeyEnabled                       = "channel.key_enabled"
	MsgChannelKeysEnabledCount                 = "channel.keys_enabled_count"
	MsgChannelNoKeyToDisable                   = "channel.no_key_to_disable"
	MsgChannelKeysDisabledCount                = "channel.keys_disabled_count"
	MsgChannelCannotDeleteLastKey              = "channel.cannot_delete_last_key"
	MsgChannelKeyDeleted                       = "channel.key_deleted"
	MsgChannelNoAutoDisabledKey                = "channel.no_auto_disabled_key"
	MsgChannelKeysDeletedAutoDisabled          = "channel.keys_deleted_auto_disabled"
	MsgChannelUnsupportedAction                = "channel.unsupported_action"
	MsgChannelIdAndModelRequired               = "channel.id_and_model_required"
	MsgChannelOllamaOnly                       = "channel.ollama_only"
	MsgChannelOllamaPullFailed                 = "channel.ollama_pull_failed"
	MsgChannelOllamaPullSuccess                = "channel.ollama_pull_success"
	MsgChannelOllamaDeleteFailed               = "channel.ollama_delete_failed"
	MsgChannelOllamaDeleteSuccess              = "channel.ollama_delete_success"
	MsgChannelOllamaVersionFailed              = "channel.ollama_version_failed"
	MsgChannelTestTaskAlreadyRunning           = "channel.test_task_already_running"
	MsgChannelBalanceUnsupported               = "channel.balance_unsupported"
	MsgChannelTaskPluginBalanceUnsupported     = "channel.task_plugin_balance_unsupported"
	MsgChannelMultiKeyBalanceUnsupported       = "channel.multi_key_balance_unsupported"
	MsgChannelModelUpdateTaskAlreadyRunning    = "channel.model_update_task_already_running"
	MsgChannelBatchNoFieldsSelected            = "channel.batch_no_fields_selected"
	MsgChannelBatchNoSelectedChannels          = "channel.batch_no_selected_channels"
	MsgChannelBatchFingerprintRequired         = "channel.batch_fingerprint_required"
	MsgChannelBatchPreviewStale                = "channel.batch_preview_stale"
	MsgChannelBatchFilteredEmpty               = "channel.batch_filtered_empty"
	MsgChannelBatchUnsupportedMode             = "channel.batch_unsupported_mode"
	MsgChannelTypeMismatch                     = "channel.type_mismatch"
	MsgChannelAffinityRuleNameOrAllRequired    = "channel.affinity_rule_name_or_all_required"
	MsgChannelAffinityParamRequired            = "channel.affinity_param_required"
	MsgChannelRequired                         = "channel.required"
	MsgChannelKeyRequired                      = "channel.key_required"
	MsgChannelSettingsInvalid                  = "channel.settings_invalid"
	MsgChannelScheduleInvalid                  = "channel.schedule_invalid"
	MsgChannelLimitOutOfRange                  = "channel.limit_out_of_range"
	MsgChannelTaskPluginKeyRequired            = "channel.task_plugin_key_required"
	MsgChannelTaskPluginKeyTooLong             = "channel.task_plugin_key_too_long"
	MsgChannelTaskPluginNotRegistered          = "channel.task_plugin_not_registered"
	MsgChannelTaskPluginBaseURLRequired        = "channel.task_plugin_base_url_required"
	MsgChannelTaskExtendNewAPIOnly             = "channel.task_extend_new_api_only"
	MsgChannelTaskExtendTooMany                = "channel.task_extend_too_many"
	MsgChannelTaskPluginKeyInvalid             = "channel.task_plugin_key_invalid"
	MsgChannelTaskPluginBoundTwice             = "channel.task_plugin_bound_twice"
	MsgChannelTaskPluginNewAPIUnsupported      = "channel.task_plugin_new_api_unsupported"
	MsgChannelBaseURLRequired                  = "channel.base_url_required"
	MsgChannelModelNameTooLong                 = "channel.model_name_too_long"
	MsgChannelVertexRegionRequired             = "channel.vertex_region_required"
	MsgChannelVertexRegionInvalidJSON          = "channel.vertex_region_invalid_json"
	MsgChannelVertexRegionDefaultRequired      = "channel.vertex_region_default_required"
	MsgChannelCodexKeyInvalidJSON              = "channel.codex_key_invalid_json"
)

// Model related messages
const (
	MsgModelNameEmpty     = "model.name_empty"
	MsgModelNameExists    = "model.name_exists"
	MsgModelIdMissing     = "model.id_missing"
	MsgModelGetListFailed = "model.get_list_failed"
	MsgModelGetFailed     = "model.get_failed"
	MsgModelResetSuccess  = "model.reset_success"

	MsgModelInvalidSquareState         = "model.invalid_square_state"
	MsgModelStatusOnlyPricingImmutable = "model.status_only_pricing_immutable"
	MsgModelInvalidCatalogVisibility   = "model.invalid_catalog_visibility"
	MsgModelPricingRootOnly            = "model.pricing_root_only"
	MsgModelGetUserGroupFailed         = "model.get_user_group_failed"
	MsgModelDoesNotExist               = "model.does_not_exist"
	MsgModelFetchUpstreamListFailed    = "model.fetch_upstream_list_failed"
)

// Vendor related messages
const (
	MsgVendorNameEmpty  = "vendor.name_empty"
	MsgVendorNameExists = "vendor.name_exists"
	MsgVendorIdMissing  = "vendor.id_missing"
)

// Group related messages
const (
	MsgGroupNameTypeEmpty = "group.name_type_empty"
	MsgGroupNameExists    = "group.name_exists"
	MsgGroupIdMissing     = "group.id_missing"
)

// Checkin related messages
const (
	MsgCheckinDisabled     = "checkin.disabled"
	MsgCheckinAlreadyToday = "checkin.already_today"
	MsgCheckinFailed       = "checkin.failed"
	MsgCheckinQuotaFailed  = "checkin.quota_failed"

	MsgCheckinSuccess         = "checkin.success"
	MsgCheckinUserRestricted  = "checkin.user_restricted"
	MsgCheckinQuotaCapReached = "checkin.quota_cap_reached"
)

// Passkey related messages
const (
	MsgPasskeyCreateFailed            = "passkey.create_failed"
	MsgPasskeyLoginAbnormal           = "passkey.login_abnormal"
	MsgPasskeyUpdateFailed            = "passkey.update_failed"
	MsgPasskeyInvalidUserId           = "passkey.invalid_user_id"
	MsgPasskeyVerifyFailed            = "passkey.verify_failed"
	MsgPasskeyRPIDInvalid             = "passkey.rp_id_invalid"
	MsgPasskeyRPIDUnavailable         = "passkey.rp_id_unavailable"
	MsgPasskeyRPIDRemovalConfirmation = "passkey.rp_id_removal_confirmation"

	MsgPasskeyNotEnabled      = "passkey.not_enabled"
	MsgPasskeyInvalidRequest  = "passkey.invalid_request"
	MsgPasskeyRegisterSuccess = "passkey.register_success"
	MsgPasskeyUnbindSuccess   = "passkey.unbind_success"
	MsgPasskeyNotBound        = "passkey.not_bound"
	MsgPasskeyResetSuccess    = "passkey.reset_success"
	MsgPasskeyNotFound        = "passkey.not_found"
)

// 2FA related messages
const (
	MsgTwoFANotEnabled    = "twofa.not_enabled"
	MsgTwoFAUserIdEmpty   = "twofa.user_id_empty"
	MsgTwoFAAlreadyExists = "twofa.already_exists"
	MsgTwoFARecordIdEmpty = "twofa.record_id_empty"
	MsgTwoFACodeInvalid   = "twofa.code_invalid"

	MsgTwoFADisabledSuccess           = "twofa.disabled_success"
	MsgTwoFABackupCodesGenerateFailed = "twofa.backup_codes_generate_failed"
	MsgTwoFABackupCodesSaveFailed     = "twofa.backup_codes_save_failed"
	MsgTwoFABackupCodesRegenerated    = "twofa.backup_codes_regenerated"
	MsgTwoFAAdminDisabled             = "twofa.admin_disabled"
)

// Rate limit related messages
const (
	MsgRateLimitReached      = "rate_limit.reached"
	MsgRateLimitTotalReached = "rate_limit.total_reached"

	MsgRateLimitCheckFailed                  = "rate_limit.check_failed"
	MsgRateLimitEmailVerificationWait        = "rate_limit.email_verification_wait"
	MsgRateLimitEmailVerificationTooFrequent = "rate_limit.email_verification_too_frequent"
	MsgRateLimitUserExceeded                 = "rate_limit.user_exceeded"
)

// Setting related messages
const (
	MsgSettingInvalidType      = "setting.invalid_type"
	MsgSettingWebhookEmpty     = "setting.webhook_empty"
	MsgSettingWebhookInvalid   = "setting.webhook_invalid"
	MsgSettingEmailInvalid     = "setting.email_invalid"
	MsgSettingBarkUrlEmpty     = "setting.bark_url_empty"
	MsgSettingBarkUrlInvalid   = "setting.bark_url_invalid"
	MsgSettingGotifyUrlEmpty   = "setting.gotify_url_empty"
	MsgSettingGotifyTokenEmpty = "setting.gotify_token_empty"
	MsgSettingGotifyUrlInvalid = "setting.gotify_url_invalid"
	MsgSettingUrlMustHttp      = "setting.url_must_http"
	MsgSettingSaved            = "setting.saved"
)

// Deployment related messages (io.net)
const (
	MsgDeploymentNotEnabled     = "deployment.not_enabled"
	MsgDeploymentIdRequired     = "deployment.id_required"
	MsgDeploymentContainerIdReq = "deployment.container_id_required"
	MsgDeploymentNameEmpty      = "deployment.name_empty"
	MsgDeploymentNameTaken      = "deployment.name_taken"
	MsgDeploymentHardwareIdReq  = "deployment.hardware_id_required"
	MsgDeploymentHardwareInvId  = "deployment.hardware_invalid_id"
	MsgDeploymentApiKeyRequired = "deployment.api_key_required"
	MsgDeploymentInvalidPayload = "deployment.invalid_payload"
	MsgDeploymentNotFound       = "deployment.not_found"

	MsgDeploymentValidateApiKeyFailed = "deployment.validate_api_key_failed"
	MsgDeploymentTerminationRequested = "deployment.termination_requested"
)

// Performance related messages
const (
	MsgPerfDiskCacheCleared = "performance.disk_cache_cleared"
	MsgPerfStatsReset       = "performance.stats_reset"
	MsgPerfGcExecuted       = "performance.gc_executed"

	MsgPerfInvalidCleanupMode  = "performance.invalid_mode"
	MsgPerfInvalidCleanupValue = "performance.invalid_value"
	MsgPerfLogDirNotConfigured = "performance.log_dir_not_configured"
	MsgPerfPartialDeleteFailed = "performance.partial_delete_failed"
	MsgPerfMetricsUnavailable  = "performance.metrics_unavailable"
	MsgPerfModelRequired       = "performance.model_required"
	MsgPerfModelNotAvailable   = "performance.model_not_available"
)

// Ability related messages
const (
	MsgAbilityDbCorrupted   = "ability.db_corrupted"
	MsgAbilityRepairRunning = "ability.repair_running"
)

// OAuth related messages
const (
	MsgOAuthInvalidCode     = "oauth.invalid_code"
	MsgOAuthGetUserErr      = "oauth.get_user_error"
	MsgOAuthAccountUsed     = "oauth.account_used"
	MsgOAuthUnknownProvider = "oauth.unknown_provider"
	MsgOAuthStateInvalid    = "oauth.state_invalid"
	MsgOAuthNotEnabled      = "oauth.not_enabled"
	MsgOAuthUserDeleted     = "oauth.user_deleted"
	MsgOAuthUserBanned      = "oauth.user_banned"
	MsgOAuthBindSuccess     = "oauth.bind_success"
	MsgOAuthAlreadyBound    = "oauth.already_bound"
	MsgOAuthNotAutoLinked   = "oauth.not_auto_linked"
	MsgOAuthConnectFailed   = "oauth.connect_failed"
	MsgOAuthTokenFailed     = "oauth.token_failed"
	MsgOAuthUserInfoEmpty   = "oauth.user_info_empty"
	MsgOAuthTrustLevelLow   = "oauth.trust_level_low"

	MsgOAuthBindRequiresLogin = "oauth.bind_requires_login"
	MsgOAuthProviderWeChat    = "oauth.provider_wechat"
	MsgOAuthAccessDenied      = "oauth.access_denied"
)

// Model layer error messages (for translation in controller)
const (
	MsgRedeemFailed          = "redeem.failed"
	MsgCreateDefaultTokenErr = "user.create_default_token_error"
	MsgUuidDuplicate         = "common.uuid_duplicate"
	MsgInvalidInput          = "common.invalid_input"
)

// Distributor related messages
const (
	MsgDistributorInvalidRequest               = "distributor.invalid_request"
	MsgDistributorInvalidChannelId             = "distributor.invalid_channel_id"
	MsgDistributorChannelDisabled              = "distributor.channel_disabled"
	MsgDistributorAffinityChannelDisabled      = "distributor.affinity_channel_disabled"
	MsgDistributorTokenNoModelAccess           = "distributor.token_no_model_access"
	MsgDistributorTokenModelForbidden          = "distributor.token_model_forbidden"
	MsgDistributorModelNameRequired            = "distributor.model_name_required"
	MsgDistributorInvalidPlayground            = "distributor.invalid_playground_request"
	MsgDistributorGroupAccessDenied            = "distributor.group_access_denied"
	MsgDistributorGetChannelFailed             = "distributor.get_channel_failed"
	MsgDistributorNoAvailableChannel           = "distributor.no_available_channel"
	MsgDistributorNoAvailableChannelTaskPlugin = "distributor.no_available_channel_task_plugin"
	MsgDistributorInvalidMidjourney            = "distributor.invalid_midjourney_request"
	MsgDistributorInvalidParseModel            = "distributor.invalid_request_parse_model"
	MsgDistributorUserModelRouteUnavailable    = "distributor.user_model_route_unavailable"
	MsgDistributorChannelOutsideUserRoute      = "distributor.channel_outside_user_route"
	MsgDistributorModelNotAccessible           = "distributor.model_not_accessible"
)

// Custom OAuth provider related messages
const (
	MsgCustomOAuthNotFound          = "custom_oauth.not_found"
	MsgCustomOAuthSlugEmpty         = "custom_oauth.slug_empty"
	MsgCustomOAuthSlugExists        = "custom_oauth.slug_exists"
	MsgCustomOAuthNameEmpty         = "custom_oauth.name_empty"
	MsgCustomOAuthHasBindings       = "custom_oauth.has_bindings"
	MsgCustomOAuthBindingNotFound   = "custom_oauth.binding_not_found"
	MsgCustomOAuthProviderIdInvalid = "custom_oauth.provider_id_field_invalid"

	MsgCustomOAuthDiscoveryURLEmpty   = "custom_oauth.discovery_url_empty"
	MsgCustomOAuthDiscoveryURLInvalid = "custom_oauth.discovery_url_invalid"
	MsgCustomOAuthDiscoveryFetch      = "custom_oauth.discovery_fetch_failed"
	MsgCustomOAuthDiscoveryParse      = "custom_oauth.discovery_parse_failed"
	MsgCustomOAuthSlugBuiltinConflict = "custom_oauth.slug_builtin_conflict"
	MsgCustomOAuthUnbindSuccess       = "custom_oauth.unbind_success"
	MsgCustomOAuthBindingCheckFailed  = "custom_oauth.binding_check_failed"
)

// Audit related messages
const (
	MsgAuditInvalidPagination = "audit.invalid_pagination"
	MsgAuditInvalidFilters    = "audit.invalid_filters"
	MsgAuditInvalidTimeRange  = "audit.invalid_time_range"
	MsgAuditInvalidResult     = "audit.invalid_result"
)

// Auth session related messages
const (
	MsgAuthSessionIdRequired = "auth_session.session_id_required"
	MsgAuthSessionNotFound   = "auth_session.not_found"
	MsgAuthSessionRequired   = "auth_session.required"
)

// Codex related messages
const (
	MsgCodexUsageFetchFailed        = "codex.usage_fetch_failed"
	MsgCodexResetCreditsFetchFailed = "codex.reset_credits_fetch_failed"
	MsgCodexResetUsageFailed        = "codex.reset_usage_failed"
	MsgCodexChannelTypeInvalid      = "codex.channel_type_invalid"
	MsgCodexMultiKeyUnsupported     = "codex.multi_key_unsupported"
	MsgCodexCredentialParseFailed   = "codex.credential_parse_failed"
	MsgCodexAccessTokenRequired     = "codex.access_token_required"
	MsgCodexAccountIdRequired       = "codex.account_id_required"
	MsgCodexUpstreamStatus          = "codex.upstream_status"
)

// Log related messages
const (
	MsgLogFieldUnavailable         = "log.field_unavailable"
	MsgLogModelFilterUnavailable   = "log.model_filter_unavailable"
	MsgLogChannelFilterUnavailable = "log.channel_filter_unavailable"
	MsgLogEndpointDeprecated       = "log.endpoint_deprecated"
	MsgLogQueryFailed              = "log.query_failed"
	MsgLogStatQueryFailed          = "log.stat_query_failed"
)

// Model radar related messages
const (
	MsgModelRadarDataUnavailable            = "model_radar.data_unavailable"
	MsgModelRadarDataTemporarilyUnavailable = "model_radar.data_temporarily_unavailable"
	MsgModelRadarSyncInProgress             = "model_radar.sync_in_progress"
)

// Model sync related messages
const (
	MsgModelSyncUnsupportedLocale       = "model_sync.unsupported_locale"
	MsgModelSyncUpstreamReportedFailure = "model_sync.upstream_reported_failure"
	MsgModelSyncPreviewRequired         = "model_sync.preview_required"
	MsgModelSyncUpstreamChanged         = "model_sync.upstream_changed"
	MsgModelSyncSelectionUnavailable    = "model_sync.selection_unavailable"
)

// Option related messages
const (
	MsgOptionOAuthNotConfigured                  = "option.oauth_not_configured"
	MsgOptionEmailDomainRestrictionNotConfigured = "option.email_domain_restriction_not_configured"
	MsgOptionWeChatNotConfigured                 = "option.wechat_not_configured"
	MsgOptionTurnstileNotConfigured              = "option.turnstile_not_configured"
	MsgOptionTelegramNotConfigured               = "option.telegram_not_configured"
	MsgOptionClassicThemeRemoved                 = "option.classic_theme_removed"
	MsgOptionComplianceFieldReadonly             = "option.compliance_field_readonly"
	MsgOptionBillingExprInvalid                  = "option.billing_expr_invalid"
	MsgOptionBillingExprModelInvalid             = "option.billing_expr_model_invalid"
	MsgOptionPluginBillingExprInvalid            = "option.plugin_billing_expr_invalid"
	MsgOptionPluginBillingExprKeyInvalid         = "option.plugin_billing_expr_key_invalid"
	MsgOptionDiagnosticSwitchInvalid             = "option.diagnostic_switch_invalid"
	MsgOptionExtraHeadersInvalid                 = "option.extra_headers_invalid"
	MsgOptionUnsupportedLogDiagnostic            = "option.unsupported_log_diagnostic"
	MsgOptionUnsupportedClientPolicy             = "option.unsupported_client_policy"
	MsgOptionClientRulesInvalid                  = "option.client_rules_invalid"
	MsgOptionGroupClientPolicyInvalid            = "option.group_client_policy_invalid"
	MsgOptionPricingCountInvalid                 = "option.pricing_count_invalid"
	MsgOptionGroupRateLimitIncomplete            = "option.group_rate_limit_incomplete"
	MsgOptionPricingKeyUnsupported               = "option.pricing_key_unsupported"
	MsgOptionPricingValueInvalid                 = "option.pricing_value_invalid"
	MsgOptionPricingKeyDuplicate                 = "option.pricing_key_duplicate"
)

// Ratio sync related messages
const (
	MsgRatioSyncOpenRouterKeyRequired = "ratio_sync.openrouter_key_required"
	MsgRatioSyncUnrecognizedData      = "ratio_sync.unrecognized_data"
)

// Relay related messages
const (
	MsgRelayGenInfoFailed                      = "relay.gen_relay_info_failed"
	MsgRelayNotImplemented                     = "relay.not_implemented"
	MsgRelayInvalidUrl                         = "relay.invalid_url"
	MsgRelayTaskProtocolError                  = "relay.task_protocol_error"
	MsgRelayTaskSubmitNoResult                 = "relay.task_submit_no_result"
	MsgRelayTaskPersistFailed                  = "relay.task_persist_failed"
	MsgRelayTaskBillingSettleFailed            = "relay.task_billing_settle_failed"
	MsgRelayUserModelRouteNoAvailable          = "relay.user_model_route_no_available"
	MsgRelayNoAvailableChannel                 = "relay.no_available_channel"
	MsgRelayChannelUnavailableForProtocolRetry = "relay.channel_unavailable_for_protocol_retry"
	MsgRelayGetChannelFailed                   = "relay.get_channel_failed"
	MsgRelayPlaygroundAccessTokenUnsupported   = "relay.playground_access_token_unsupported"
	MsgRelayNoCompatibleChannel                = "relay.no_compatible_channel"
	MsgRelayNoCompatibleChannelReason          = "relay.no_compatible_channel_reason"
	MsgRelayRequestBodyTooLarge                = "relay.request_body_too_large"
)

// Setup related messages
const (
	MsgSetupAlreadyInitialized     = "setup.already_initialized"
	MsgSetupUsernameTooLong        = "setup.username_too_long"
	MsgSetupPasswordMismatch       = "setup.password_mismatch"
	MsgSetupInternalError          = "setup.internal_error"
	MsgSetupCreateAdminFailed      = "setup.create_admin_failed"
	MsgSetupSaveSelfUseModeFailed  = "setup.save_self_use_mode_failed"
	MsgSetupSaveDemoSiteModeFailed = "setup.save_demo_site_mode_failed"
	MsgSetupInitializeFailed       = "setup.initialize_failed"
	MsgSetupInitializeSuccess      = "setup.initialize_success"
)

// System related messages
const (
	MsgSystemNodeNameRequired = "system.node_name_required"
	MsgSystemInstanceNotStale = "system.instance_not_stale"
)

// System task related messages
const (
	MsgSystemTaskTargetTimestampRequired = "system_task.target_timestamp_required"
	MsgSystemTaskTypeRequired            = "system_task.type_required"
	MsgSystemTaskInvalidFilters          = "system_task.invalid_filters"
	MsgSystemTaskIdRequired              = "system_task.task_id_required"
	MsgSystemTaskNotFound                = "system_task.not_found"
)

// System update related messages
const (
	MsgSystemUpdateCheckFailed   = "system_update.check_failed"
	MsgSystemUpdateNotConfigured = "system_update.not_configured"
	MsgSystemUpdateInProgress    = "system_update.in_progress"
	MsgSystemUpdateTriggerFailed = "system_update.trigger_failed"
)

// Task plugin related messages
const (
	MsgTaskPluginNotFound                      = "task_plugin.not_found"
	MsgTaskPluginSourceTooLarge                = "task_plugin.source_too_large"
	MsgTaskPluginSourceHashMismatch            = "task_plugin.source_hash_mismatch"
	MsgTaskPluginOverrideVersionNotFound       = "task_plugin.override_version_not_found"
	MsgTaskPluginVersionNotFound               = "task_plugin.version_not_found"
	MsgTaskPluginInUse                         = "task_plugin.in_use"
	MsgTaskPluginEnabledRequired               = "task_plugin.enabled_required"
	MsgTaskPluginMarketplaceSourceNameRequired = "task_plugin.marketplace_source_name_required"
	MsgTaskPluginMarketplaceSourceUrlInvalid   = "task_plugin.marketplace_source_url_invalid"
	MsgTaskPluginDatabaseSnapshotUnavailable   = "task_plugin.database_snapshot_unavailable"
)

// Telegram related messages
const (
	MsgTelegramLegacyAuthRemoved = "telegram.legacy_auth_removed"
)

// Usage related messages
const (
	MsgUsageInvalidStartTimestamp = "usage.invalid_start_timestamp"
	MsgUsageInvalidEndTimestamp   = "usage.invalid_end_timestamp"
	MsgUsageInvalidTimeRange      = "usage.invalid_time_range"
	MsgUsageTimeRangeTooLong      = "usage.time_range_too_long"
)

// Wordlist related messages
const (
	MsgWordlistUnavailable              = "wordlist.unavailable"
	MsgWordlistInvalidRequest           = "wordlist.invalid_request"
	MsgWordlistNameLength               = "wordlist.name_length"
	MsgWordlistNameInvalid              = "wordlist.name_invalid"
	MsgWordlistInvalidSource            = "wordlist.invalid_source"
	MsgWordlistActionInvalid            = "wordlist.action_invalid"
	MsgWordlistReviewRequiresModelAudit = "wordlist.review_requires_model_audit"
	MsgWordlistCreateFailed             = "wordlist.create_failed"
	MsgWordlistUpdateFailed             = "wordlist.update_failed"
	MsgWordlistDeleteFailed             = "wordlist.delete_failed"
	MsgWordlistSyncFailed               = "wordlist.sync_failed"
	MsgWordlistRefreshFailed            = "wordlist.refresh_failed"
	MsgWordlistNoFields                 = "wordlist.no_fields"
	MsgWordlistNotFound                 = "wordlist.not_found"
	MsgWordlistEnabledOrActionRequired  = "wordlist.enabled_or_action_required"
	MsgWordlistCustomInvalid            = "wordlist.custom_invalid"
	MsgWordlistInvalidTest              = "wordlist.invalid_test"
)
