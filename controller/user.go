package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

var (
	errUserPasswordUnset    = errors.New("user password is not set")
	errOriginalPasswordFail = errors.New("original password is incorrect")
)

func Login(c *gin.Context) {
	if !common.PasswordLoginEnabled {
		common.ApiErrorI18n(c, i18n.MsgUserPasswordLoginDisabled)
		return
	}
	var loginRequest LoginRequest
	err := common.DecodeJson(c.Request.Body, &loginRequest)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	username := loginRequest.Username
	password := loginRequest.Password
	if username == "" || password == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	user := model.User{
		Username: username,
		Password: password,
	}
	err = user.ValidateAndFill()
	if err != nil {
		switch {
		case errors.Is(err, model.ErrDatabase):
			common.SysLog(fmt.Sprintf("Login database error for user %s: %v", username, err))
			common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		case errors.Is(err, model.ErrUserEmptyCredentials):
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		default:
			common.ApiErrorI18n(c, i18n.MsgUserUsernameOrPasswordError)
		}
		return
	}

	// 检查是否启用2FA
	twoFAEnabled, err := model.IsTwoFAEnabled(user.Id)
	if err != nil {
		common.SysLog(fmt.Sprintf("Login failed to load 2FA status for user %d: %v", user.Id, err))
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		return
	}
	if twoFAEnabled {
		expiresAt := time.Now().Add(5 * time.Minute)
		payload, err := common.Marshal(twoFALoginFlowPayload{AuthVersion: user.AuthVersion})
		if err != nil {
			common.ApiError(c, err)
			return
		}
		flowToken, _, err := model.CreateAuthFlow(model.AuthFlowCreate{
			Purpose:   model.AuthFlowPurposeTwoFALogin,
			UserId:    user.Id,
			Payload:   string(payload),
			ExpiresAt: expiresAt,
		})
		if err != nil {
			common.ApiError(c, err)
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": i18n.T(c, i18n.MsgUserRequire2FA),
			"success": true,
			"data": map[string]interface{}{
				"require_2fa": true,
				"flow_token":  flowToken,
				"expires_at":  expiresAt.Unix(),
			},
		})
		return
	}

	setupLogin(&user, c)
}

// loginMethodFromContext 根据请求路径推导登录方式，用于登录审计日志。
func loginMethodFromContext(c *gin.Context) string {
	switch c.FullPath() {
	case "/api/user/login":
		return "password"
	case "/api/user/login/2fa":
		return "2fa"
	case "/api/user/passkey/login/finish":
		return "passkey"
	case "/api/oauth/wechat":
		return "wechat"
	case "/api/oauth/telegram/login":
		return "telegram"
	case "/api/oauth/:provider":
		if provider := c.Param("provider"); provider != "" {
			return "oauth:" + provider
		}
		return "oauth"
	default:
		return "unknown"
	}
}

// recordLoginAudit 记录登录成功审计日志（对所有用户启用，仅记录成功，不记录失败）。
func recordLoginAudit(user *model.User, c *gin.Context) {
	method := loginMethodFromContext(c)
	ip := c.ClientIP()
	extra := map[string]interface{}{
		"login_method": method,
		"user_agent":   c.Request.UserAgent(),
	}
	content := fmt.Sprintf("Logged in successfully via %s", method)
	model.RecordLoginLog(user.Id, user.Username, content, ip, "login", map[string]interface{}{
		"method": method,
	}, extra)
}

// setupLogin creates a server-controlled login Session and returns the shared
// authentication bundle used by every login method.
func setupLogin(user *model.User, c *gin.Context) {
	setupLoginAtAuthVersion(user, 0, c)
}

func setupLoginAtAuthVersion(user *model.User, expectedAuthVersion int64, c *gin.Context) {
	if user == nil || user.Id <= 0 || user.Status != common.UserStatusEnabled {
		common.ApiErrorI18n(c, i18n.MsgAuthUserBanned)
		return
	}
	currentUser, err := model.GetUserById(user.Id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var bundle *service.AuthBundle
	if expectedAuthVersion > 0 {
		bundle, err = service.CreateLoginSessionAtAuthVersion(
			user.Id,
			expectedAuthVersion,
			loginMethodFromContext(c),
			c.ClientIP(),
			c.Request.UserAgent(),
		)
	} else {
		bundle, err = service.CreateLoginSession(
			user.Id,
			loginMethodFromContext(c),
			c.ClientIP(),
			c.Request.UserAgent(),
		)
	}
	if err != nil {
		writeAuthSessionError(c, err)
		return
	}
	model.UpdateUserLastLoginAt(user.Id)
	service.WriteRefreshCookie(c, bundle.RefreshToken)
	setAuthNoStore(c)
	recordLoginAudit(user, c)
	c.JSON(http.StatusOK, gin.H{
		"message": "",
		"success": true,
		"data": gin.H{
			"access_token":      bundle.AccessToken,
			"token_type":        bundle.TokenType,
			"access_expires_at": bundle.AccessExpiresAt,
			"session":           bundle.Session,
			"user":              buildSelfUserData(currentUser),
		},
	})
}

func Register(c *gin.Context) {
	if !common.RegisterEnabled {
		common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
		return
	}
	if !common.PasswordRegisterEnabled {
		common.ApiErrorI18n(c, i18n.MsgUserPasswordRegisterDisabled)
		return
	}
	var user model.User
	err := common.DecodeJson(c.Request.Body, &user)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	user.Username = strings.TrimSpace(user.Username)
	user.Email = model.NormalizeEmail(user.Email)
	if user.Username == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := common.Validate.Struct(&user); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserInputInvalid, map[string]any{"Error": err.Error()})
		return
	}
	if common.EmailVerificationEnabled {
		if user.Email == "" || user.VerificationCode == "" {
			common.ApiErrorI18n(c, i18n.MsgUserEmailVerificationRequired)
			return
		}
		if !common.VerifyCodeWithKey(user.Email, user.VerificationCode, common.EmailVerificationPurpose) {
			common.ApiErrorI18n(c, i18n.MsgUserVerificationCodeError)
			return
		}
		if err := model.EnsureEmailAvailable(user.Email, 0); err != nil {
			if errors.Is(err, model.ErrEmailAlreadyTaken) {
				common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
				return
			}
			common.ApiErrorI18n(c, i18n.MsgDatabaseError)
			return
		}
	}
	emailForExistCheck := ""
	if common.EmailVerificationEnabled {
		emailForExistCheck = user.Email
	}
	exist, err := model.CheckUserExistOrDeleted(user.Username, emailForExistCheck)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		common.SysLog(fmt.Sprintf("CheckUserExistOrDeleted error: %v", err))
		return
	}
	if exist {
		common.ApiErrorI18n(c, i18n.MsgUserExists)
		return
	}
	affCode := user.AffCode // this code is the inviter's code, not the user's own code
	inviterId, _ := model.GetUserIdByAffCode(affCode)
	cleanUser := model.User{
		Username:    user.Username,
		Password:    user.Password,
		DisplayName: user.Username,
		InviterId:   inviterId,
		Role:        common.RoleCommonUser, // 明确设置角色为普通用户
	}
	if common.EmailVerificationEnabled {
		cleanUser.Email = user.Email
	}
	if err := cleanUser.Insert(inviterId); err != nil {
		if errors.Is(err, model.ErrEmailAlreadyTaken) {
			common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
			return
		}
		common.ApiError(c, err)
		return
	}

	// 获取插入后的用户ID
	var insertedUser model.User
	if err := model.DB.Where("username = ?", cleanUser.Username).First(&insertedUser).Error; err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserRegisterFailed)
		return
	}
	// 生成默认令牌
	if constant.GenerateDefaultToken {
		key, err := common.GenerateKey()
		if err != nil {
			common.ApiErrorI18n(c, i18n.MsgUserDefaultTokenFailed)
			common.SysLog("failed to generate token key: " + err.Error())
			return
		}
		// 生成默认令牌
		token := model.Token{
			UserId:             insertedUser.Id, // 使用插入后的用户ID
			Name:               cleanUser.Username + "的初始令牌",
			Key:                key,
			CreatedTime:        common.GetTimestamp(),
			AccessedTime:       common.GetTimestamp(),
			ExpiredTime:        -1,     // 永不过期
			RemainQuota:        500000, // 示例额度
			UnlimitedQuota:     true,
			ModelLimitsEnabled: false,
			Group:              "default",
		}
		if setting.DefaultUseAutoGroup {
			token.Group = "auto"
		} else if len(cleanUser.Groups) > 0 {
			token.Group = cleanUser.Groups[0]
		}
		if err := token.Insert(); err != nil {
			common.ApiErrorI18n(c, i18n.MsgCreateDefaultTokenErr)
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func GetAllUsers(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	sortOptions := model.NewUserSortOptions(c.Query("sort_by"), c.Query("sort_order"))
	users, total, err := model.GetAllUsers(pageInfo, sortOptions)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(users)

	common.ApiSuccess(c, pageInfo)
	return
}

func SearchUsers(c *gin.Context) {
	keyword := c.Query("keyword")
	group := c.Query("group")
	var role *int
	if roleStr := c.Query("role"); roleStr != "" {
		if parsed, err := strconv.Atoi(roleStr); err == nil {
			role = &parsed
		}
	}
	var status *int
	if statusStr := c.Query("status"); statusStr != "" {
		if parsed, err := strconv.Atoi(statusStr); err == nil {
			status = &parsed
		}
	}
	pageInfo := common.GetPageQuery(c)
	sortOptions := model.NewUserSortOptions(c.Query("sort_by"), c.Query("sort_order"))
	users, total, err := model.SearchUsers(keyword, group, role, status, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), sortOptions)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(users)
	common.ApiSuccess(c, pageInfo)
	return
}

func canManageTargetRole(myRole int, targetRole int) bool {
	return myRole == common.RoleRootUser || myRole > targetRole
}

func GetUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	myRole := c.GetInt("role")
	if !canManageTargetRole(myRole, user.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionSameLevel)
		return
	}
	user.AdminPermissions = authz.Capabilities(user.Id, user.Role)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    user,
	})
	return
}

func GenerateAccessToken(c *gin.Context) {
	id := c.GetInt("id")
	// get rand int 28-32
	randI := common.GetRandomInt(4)
	key, err := common.GenerateRandomKey(29 + randI)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgGenerateFailed)
		common.SysLog("failed to generate key: " + err.Error())
		return
	}
	if model.DB.Where("access_token = ?", key).First(&model.User{}).RowsAffected != 0 {
		common.ApiErrorI18n(c, i18n.MsgUuidDuplicate)
		return
	}

	if err := model.UpdateUserAccessToken(id, key); err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    key,
	})
	return
}

type TransferAffQuotaRequest struct {
	Quota int `json:"quota" binding:"required"`
}

func TransferAffQuota(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	id := c.GetInt("id")
	user, err := model.GetUserById(id, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	tran := TransferAffQuotaRequest{}
	if err := c.ShouldBindJSON(&tran); err != nil {
		common.ApiError(c, err)
		return
	}
	err = user.TransferAffQuotaToQuota(tran.Quota)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserTransferFailed, map[string]any{"Error": err.Error()})
		return
	}
	common.ApiSuccessI18n(c, i18n.MsgUserTransferSuccess, nil)
}

func GetAffCode(c *gin.Context) {
	id := c.GetInt("id")
	user, err := model.GetUserById(id, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if user.AffCode == "" {
		user.AffCode = common.GetRandomString(4)
		if err := user.Update(false); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    user.AffCode,
	})
	return
}

func GetSelf(c *gin.Context) {
	id := c.GetInt("id")
	userRole := c.GetInt("role")
	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	responseData := buildSelfUserData(user)
	// The authenticated role is loaded from GetUserCache. It should equal the
	// row role, but use it for capabilities so GetSelf and login/refresh remain
	// consistent with the authorization decision made for this request.
	permissions := calculateUserPermissions(userRole)
	permissions["admin_permissions"] = authz.Capabilities(id, userRole)
	responseData["permissions"] = permissions

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    responseData,
	})
	return
}

// buildSelfUserData is the single safe dashboard-user DTO used by GetSelf,
// login and refresh. It intentionally excludes password, management PAT and
// administrator-only remarks.
func buildSelfUserData(user *model.User) map[string]interface{} {
	userSetting := user.GetSetting()
	permissions := calculateUserPermissions(user.Role)
	permissions["admin_permissions"] = authz.Capabilities(user.Id, user.Role)
	return map[string]interface{}{
		"id":                      user.Id,
		"username":                user.Username,
		"display_name":            user.DisplayName,
		"role":                    user.Role,
		"status":                  user.Status,
		"email":                   user.Email,
		"github_id":               user.GitHubId,
		"discord_id":              user.DiscordId,
		"oidc_id":                 user.OidcId,
		"wechat_id":               user.WeChatId,
		"telegram_id":             user.TelegramId,
		"group":                   user.Group,
		"groups":                  user.Groups,
		"topup_group":             user.TopupGroup,
		"model_limits_enabled":    user.ModelLimitsEnabled,
		"model_limits":            user.ModelLimits,
		"model_blocklist_enabled": user.ModelBlocklistEnabled,
		"model_blocklist":         user.ModelBlocklist,
		"quota":                   user.Quota,
		"used_quota":              user.UsedQuota,
		"request_count":           user.RequestCount,
		"aff_code":                user.AffCode,
		"aff_count":               user.AffCount,
		"aff_quota":               user.AffQuota,
		"aff_history_quota":       user.AffHistoryQuota,
		"inviter_id":              user.InviterId,
		"linux_do_id":             user.LinuxDOId,
		"setting":                 user.Setting,
		"stripe_customer":         user.StripeCustomer,
		"sidebar_modules":         userSetting.SidebarModules, // 正确提取sidebar_modules字段
		"permissions":             permissions,
	}
}

// 计算用户权限的辅助函数
func calculateUserPermissions(userRole int) map[string]interface{} {
	permissions := map[string]interface{}{}

	// 根据用户角色计算权限
	if userRole == common.RoleRootUser {
		// 超级管理员不需要边栏设置功能
		permissions["sidebar_settings"] = false
		permissions["sidebar_modules"] = map[string]interface{}{}
	} else if userRole == common.RoleAdminUser {
		// 管理员可以设置边栏，但不包含系统设置功能
		permissions["sidebar_settings"] = true
		permissions["sidebar_modules"] = map[string]interface{}{
			"admin": map[string]interface{}{
				"setting": false, // 管理员不能访问系统设置
			},
		}
	} else {
		// 普通用户只能设置个人功能，不包含管理员区域
		permissions["sidebar_settings"] = true
		permissions["sidebar_modules"] = map[string]interface{}{
			"admin": false, // 普通用户不能访问管理员区域
		}
	}

	return permissions
}

// 根据用户角色生成默认的边栏配置
func generateDefaultSidebarConfig(userRole int) string {
	defaultConfig := map[string]interface{}{}

	// 聊天区域 - 所有用户都可以访问
	defaultConfig["chat"] = map[string]interface{}{
		"enabled":    true,
		"playground": true,
		"chat":       true,
	}

	// 控制台区域 - 所有用户都可以访问
	defaultConfig["console"] = map[string]interface{}{
		"enabled":    true,
		"detail":     true,
		"token":      true,
		"log":        true,
		"midjourney": true,
		"task":       true,
	}

	// 个人中心区域 - 所有用户都可以访问
	defaultConfig["personal"] = map[string]interface{}{
		"enabled":  true,
		"topup":    true,
		"personal": true,
	}

	// 管理员区域 - 根据角色决定
	if userRole == common.RoleAdminUser {
		// 管理员可以访问管理员区域，但不能访问系统设置
		defaultConfig["admin"] = map[string]interface{}{
			"enabled":    true,
			"channel":    true,
			"models":     true,
			"redemption": true,
			"user":       true,
			"setting":    false, // 管理员不能访问系统设置
		}
	} else if userRole == common.RoleRootUser {
		// 超级管理员可以访问所有功能
		defaultConfig["admin"] = map[string]interface{}{
			"enabled":    true,
			"channel":    true,
			"models":     true,
			"redemption": true,
			"user":       true,
			"setting":    true,
		}
	}
	// 普通用户不包含admin区域

	// 转换为JSON字符串
	configBytes, err := common.Marshal(defaultConfig)
	if err != nil {
		common.SysLog("生成默认边栏配置失败: " + err.Error())
		return ""
	}

	return string(configBytes)
}

func GetUserModels(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		id = c.GetInt("id")
	}
	user, err := model.GetUserCache(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	groups := service.GetAuthorizedUserGroups(user.Groups)
	group := c.Query("group")
	var groupsToQuery []string
	switch {
	case group == "":
		// Start with the administrator-defined membership order, then include
		// every additional group granted by the usable-group rules. Maps do not
		// provide stable iteration order, so the remaining names are sorted.
		seenGroups := make(map[string]struct{}, len(groups))
		for _, userGroup := range user.Groups {
			if _, ok := groups[userGroup]; ok {
				groupsToQuery = append(groupsToQuery, userGroup)
				seenGroups[userGroup] = struct{}{}
			}
		}
		for _, autoGroup := range setting.GetAutoGroups() {
			if _, ok := groups[autoGroup]; ok {
				if _, seen := seenGroups[autoGroup]; !seen {
					groupsToQuery = append(groupsToQuery, autoGroup)
					seenGroups[autoGroup] = struct{}{}
				}
			}
		}
		remainingGroups := make([]string, 0, len(groups)-len(groupsToQuery))
		for authorizedGroup := range groups {
			if _, seen := seenGroups[authorizedGroup]; !seen {
				remainingGroups = append(remainingGroups, authorizedGroup)
			}
		}
		sort.Strings(remainingGroups)
		groupsToQuery = append(groupsToQuery, remainingGroups...)
	case group == "auto":
		if _, ok := groups[group]; ok {
			groupsToQuery = service.GetUserAutoGroups(user.Groups)
		}
	default:
		if _, ok := groups[group]; ok {
			groupsToQuery = []string{group}
		}
	}
	models := service.GetGroupsEnabledModels(groupsToQuery)
	if user.Role != common.RoleRootUser {
		routeSources, routeErr := model.GetEnabledUserModelRouteSources(user.Id, groupsToQuery)
		if routeErr == nil && len(routeSources) > 0 {
			seen := make(map[string]struct{}, len(models)+len(routeSources))
			for _, modelName := range models {
				seen[modelName] = struct{}{}
			}
			for _, modelName := range routeSources {
				if _, exists := seen[modelName]; !exists {
					models = append(models, modelName)
					seen[modelName] = struct{}{}
				}
			}
		}
	}
	if user.Role != common.RoleRootUser && user.ModelLimitsEnabled {
		allowed := make(map[string]struct{}, len(user.ModelLimits))
		for _, modelName := range user.ModelLimits {
			modelName = strings.TrimSpace(modelName)
			if modelName == "" {
				continue
			}
			allowed[modelName] = struct{}{}
			allowed[ratio_setting.FormatMatchingModelName(modelName)] = struct{}{}
		}
		filtered := make([]string, 0, len(models))
		for _, modelName := range models {
			matchName := ratio_setting.FormatMatchingModelName(modelName)
			if _, ok := allowed[modelName]; ok {
				filtered = append(filtered, modelName)
			} else if _, ok := allowed[matchName]; ok {
				filtered = append(filtered, modelName)
			}
		}
		models = filtered
	}
	if user.Role != common.RoleRootUser && user.ModelBlocklistEnabled {
		blocked := make(map[string]struct{}, len(user.ModelBlocklist))
		for _, modelName := range user.ModelBlocklist {
			modelName = strings.TrimSpace(modelName)
			if modelName == "" {
				continue
			}
			blocked[modelName] = struct{}{}
			blocked[ratio_setting.FormatMatchingModelName(modelName)] = struct{}{}
		}
		filtered := make([]string, 0, len(models))
		for _, modelName := range models {
			matchName := ratio_setting.FormatMatchingModelName(modelName)
			if _, denied := blocked[modelName]; denied {
				continue
			}
			if _, denied := blocked[matchName]; denied {
				continue
			}
			filtered = append(filtered, modelName)
		}
		models = filtered
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    models,
	})
}

func GetUserPolicy(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
		return
	}
	common.ApiSuccess(c, model.UserPolicyUpdate{
		Groups:                user.Groups,
		PrimaryGroup:          user.Group,
		TopupGroup:            user.TopupGroup,
		ModelLimitsEnabled:    user.ModelLimitsEnabled,
		ModelLimits:           user.ModelLimits,
		ModelBlocklistEnabled: user.ModelBlocklistEnabled,
		ModelBlocklist:        user.ModelBlocklist,
		CheckinEnabled:        user.CheckinEnabled,
		CheckinMinQuota:       user.CheckinMinQuota,
		CheckinMaxQuota:       user.CheckinMaxQuota,
		QuotaCap:              user.QuotaCap,
		RpmLimit:              user.RpmLimit,
		ConcurrencyLimit:      user.ConcurrencyLimit,
		StreamTpsLimit:        user.StreamTpsLimit,
		FirstTokenDelayMs:     user.FirstTokenDelayMs,
	})
}

func decodeManagedUserMutation(c *gin.Context) (model.User, map[string]json.RawMessage, error) {
	var raw map[string]json.RawMessage
	if err := common.DecodeJson(c.Request.Body, &raw); err != nil {
		return model.User{}, nil, err
	}
	payload, err := common.Marshal(raw)
	if err != nil {
		return model.User{}, nil, err
	}
	var user model.User
	if err := common.Unmarshal(payload, &user); err != nil {
		return model.User{}, nil, err
	}
	return user, raw, nil
}

func userPolicyFromMutation(user model.User, raw map[string]json.RawMessage, fallback *model.UserPolicyUpdate) (*model.UserPolicyUpdate, error) {
	_, hasGroups := raw["groups"]
	_, hasPrimaryGroup := raw["primary_group"]
	_, hasLegacyGroup := raw["group"]
	_, hasTopupGroup := raw["topup_group"]
	_, hasModelLimitsEnabled := raw["model_limits_enabled"]
	_, hasModelLimits := raw["model_limits"]
	_, hasModelBlocklistEnabled := raw["model_blocklist_enabled"]
	_, hasModelBlocklist := raw["model_blocklist"]
	_, hasCheckinEnabled := raw["checkin_enabled"]
	_, hasCheckinMinQuota := raw["checkin_min_quota"]
	_, hasCheckinMaxQuota := raw["checkin_max_quota"]
	_, hasQuotaCap := raw["quota_cap"]
	_, hasRpmLimit := raw["rpm_limit"]
	_, hasConcurrencyLimit := raw["concurrency_limit"]
	_, hasStreamTpsLimit := raw["stream_tps_limit"]
	_, hasFirstTokenDelayMs := raw["first_token_delay_ms"]
	if !hasGroups && !hasPrimaryGroup && !hasLegacyGroup && !hasTopupGroup && !hasModelLimitsEnabled && !hasModelLimits && !hasModelBlocklistEnabled && !hasModelBlocklist &&
		!hasCheckinEnabled && !hasCheckinMinQuota && !hasCheckinMaxQuota && !hasQuotaCap && !hasRpmLimit && !hasConcurrencyLimit && !hasStreamTpsLimit && !hasFirstTokenDelayMs {
		return nil, nil
	}

	update := model.UserPolicyUpdate{
		Groups:                append([]string(nil), user.Groups...),
		PrimaryGroup:          user.Group,
		TopupGroup:            user.TopupGroup,
		ModelLimitsEnabled:    user.ModelLimitsEnabled,
		ModelLimits:           append([]string(nil), user.ModelLimits...),
		ModelBlocklistEnabled: user.ModelBlocklistEnabled,
		ModelBlocklist:        append([]string(nil), user.ModelBlocklist...),
		CheckinEnabled:        user.CheckinEnabled,
		CheckinMinQuota:       user.CheckinMinQuota,
		CheckinMaxQuota:       user.CheckinMaxQuota,
		QuotaCap:              user.QuotaCap,
		RpmLimit:              user.RpmLimit,
		ConcurrencyLimit:      user.ConcurrencyLimit,
		StreamTpsLimit:        user.StreamTpsLimit,
		FirstTokenDelayMs:     user.FirstTokenDelayMs,
	}
	if hasRpmLimit {
		if err := common.Unmarshal(raw["rpm_limit"], &update.RpmLimit); err != nil {
			return nil, err
		}
	}
	if hasConcurrencyLimit {
		if err := common.Unmarshal(raw["concurrency_limit"], &update.ConcurrencyLimit); err != nil {
			return nil, err
		}
	}
	if hasStreamTpsLimit {
		if err := common.Unmarshal(raw["stream_tps_limit"], &update.StreamTpsLimit); err != nil {
			return nil, err
		}
	}
	if hasFirstTokenDelayMs {
		if err := common.Unmarshal(raw["first_token_delay_ms"], &update.FirstTokenDelayMs); err != nil {
			return nil, err
		}
	}
	if !hasGroups {
		switch {
		case hasLegacyGroup:
			update.Groups = []string{user.Group}
		case fallback != nil:
			update.Groups = append([]string(nil), fallback.Groups...)
		default:
			group := strings.TrimSpace(user.Group)
			if group == "" || group == "auto" {
				group = "default"
			}
			update.Groups = []string{group}
		}
	}
	if hasPrimaryGroup {
		if err := common.Unmarshal(raw["primary_group"], &update.PrimaryGroup); err != nil {
			return nil, err
		}
	} else if hasGroups && hasLegacyGroup {
		// New clients may keep sending the legacy `group` field alongside the
		// multi-group list. In that shape it identifies the explicit primary
		// group; when it is the only policy field it remains the old single-group
		// replacement handled above.
		if err := common.Unmarshal(raw["group"], &update.PrimaryGroup); err != nil {
			return nil, err
		}
	} else if !hasPrimaryGroup && fallback != nil {
		update.PrimaryGroup = fallback.PrimaryGroup
	}
	if !hasTopupGroup && fallback != nil {
		update.TopupGroup = fallback.TopupGroup
	}
	if !hasModelLimitsEnabled && fallback != nil {
		update.ModelLimitsEnabled = fallback.ModelLimitsEnabled
	}
	if !hasModelLimits && fallback != nil {
		update.ModelLimits = append([]string(nil), fallback.ModelLimits...)
	}
	if !hasModelBlocklistEnabled && fallback != nil {
		update.ModelBlocklistEnabled = fallback.ModelBlocklistEnabled
	}
	if !hasModelBlocklist && fallback != nil {
		update.ModelBlocklist = append([]string(nil), fallback.ModelBlocklist...)
	}
	if !hasCheckinEnabled && fallback != nil {
		update.CheckinEnabled = fallback.CheckinEnabled
	}
	if !hasCheckinMinQuota && fallback != nil {
		update.CheckinMinQuota = fallback.CheckinMinQuota
	}
	if !hasCheckinMaxQuota && fallback != nil {
		update.CheckinMaxQuota = fallback.CheckinMaxQuota
	}
	if !hasQuotaCap && fallback != nil {
		update.QuotaCap = fallback.QuotaCap
	}
	if !hasRpmLimit && fallback != nil {
		update.RpmLimit = fallback.RpmLimit
	}
	if !hasConcurrencyLimit && fallback != nil {
		update.ConcurrencyLimit = fallback.ConcurrencyLimit
	}
	if !hasStreamTpsLimit && fallback != nil {
		update.StreamTpsLimit = fallback.StreamTpsLimit
	}
	if !hasFirstTokenDelayMs && fallback != nil {
		update.FirstTokenDelayMs = fallback.FirstTokenDelayMs
	}

	normalized, err := normalizeUserPolicyUpdate(update)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

func normalizeUserPolicyUpdate(update model.UserPolicyUpdate) (model.UserPolicyUpdate, error) {
	seenGroups := make(map[string]struct{}, len(update.Groups))
	groups := make([]string, 0, len(update.Groups))
	for _, group := range update.Groups {
		group = strings.TrimSpace(group)
		if group == "" || group == "auto" || !ratio_setting.ContainsGroupRatio(group) {
			return model.UserPolicyUpdate{}, fmt.Errorf("无效分组：%s", group)
		}
		if _, exists := seenGroups[group]; exists {
			continue
		}
		seenGroups[group] = struct{}{}
		groups = append(groups, group)
	}
	if len(groups) == 0 {
		return model.UserPolicyUpdate{}, errors.New("至少需要选择一个用户分组")
	}
	update.PrimaryGroup = strings.TrimSpace(update.PrimaryGroup)
	if update.PrimaryGroup == "" {
		update.PrimaryGroup = groups[0]
	}
	if !service.UserHasGroup(groups, update.PrimaryGroup) {
		return model.UserPolicyUpdate{}, errors.New("默认分组无效")
	}
	// Keep the primary membership first so legacy reads of users.group and
	// older token fallback behavior remain deterministic.
	orderedGroups := make([]string, 0, len(groups))
	orderedGroups = append(orderedGroups, update.PrimaryGroup)
	for _, group := range groups {
		if group != update.PrimaryGroup {
			orderedGroups = append(orderedGroups, group)
		}
	}
	update.Groups = orderedGroups
	update.TopupGroup = strings.TrimSpace(update.TopupGroup)
	if update.TopupGroup == "" {
		update.TopupGroup = groups[0]
	}
	if !ratio_setting.ContainsGroupRatio(update.TopupGroup) || !service.UserHasGroup(groups, update.TopupGroup) {
		return model.UserPolicyUpdate{}, errors.New("充值分组无效")
	}

	publicModels := make(map[string]struct{})
	for _, pricing := range model.GetPricing() {
		publicModels[pricing.ModelName] = struct{}{}
	}
	models, err := normalizeUserPolicyModels(update.ModelLimits, publicModels)
	if err != nil {
		return model.UserPolicyUpdate{}, err
	}
	blockedModels, err := normalizeUserPolicyModels(update.ModelBlocklist, publicModels)
	if err != nil {
		return model.UserPolicyUpdate{}, err
	}
	update.ModelLimits = models
	update.ModelBlocklist = blockedModels
	if err := validateUserCheckinOverride(update.CheckinMinQuota, update.CheckinMaxQuota); err != nil {
		return model.UserPolicyUpdate{}, err
	}
	if err := validateUserQuotaCap(update.QuotaCap); err != nil {
		return model.UserPolicyUpdate{}, err
	}
	if err := validateUserRateLimit("RPM", update.RpmLimit); err != nil {
		return model.UserPolicyUpdate{}, err
	}
	if err := validateUserRateLimit("并发", update.ConcurrencyLimit); err != nil {
		return model.UserPolicyUpdate{}, err
	}
	if err := validateUserRateLimit("流式 TPS", update.StreamTpsLimit); err != nil {
		return model.UserPolicyUpdate{}, err
	}
	if err := validateUserRateLimit("首个文本延迟", update.FirstTokenDelayMs); err != nil {
		return model.UserPolicyUpdate{}, err
	}
	return update, nil
}

// maxUserCheckinQuotaOverride bounds admin-set per-user check-in rewards so a
// typo cannot approach the 32-bit quota columns.
const maxUserCheckinQuotaOverride = 1_000_000_000

// maxUserQuotaCap keeps the balance cap inside the 32-bit quota column range.
const maxUserQuotaCap = 2_000_000_000

const maxUserRateLimit = 2_147_483_647

func validateUserRateLimit(name string, limit *int) error {
	if limit == nil {
		return nil
	}
	if *limit < 1 || *limit > maxUserRateLimit {
		return fmt.Errorf("%s 限制必须在 1 到 %d 之间", name, maxUserRateLimit)
	}
	return nil
}

func userRateLimitAudit(raw map[string]json.RawMessage, update *model.UserPolicyUpdate) map[string]interface{} {
	if update == nil {
		return nil
	}
	entries := make(map[string]interface{})
	add := func(key string, value *int) {
		if _, exists := raw[key]; !exists {
			return
		}
		entry := map[string]interface{}{"mode": "clear"}
		if value != nil {
			entry["mode"] = "custom"
			entry["value"] = *value
		}
		entries[key] = entry
	}
	add("rpm_limit", update.RpmLimit)
	add("concurrency_limit", update.ConcurrencyLimit)
	add("stream_tps_limit", update.StreamTpsLimit)
	add("first_token_delay_ms", update.FirstTokenDelayMs)
	if len(entries) == 0 {
		return nil
	}
	return entries
}

func validateUserQuotaCap(cap *int) error {
	if cap == nil {
		return nil
	}
	if *cap < 0 {
		return errors.New("额度上限不能为负数")
	}
	if *cap > maxUserQuotaCap {
		return fmt.Errorf("额度上限不能超过 %d", maxUserQuotaCap)
	}
	return nil
}

func validateUserCheckinOverride(minQuota, maxQuota *int) error {
	if (minQuota == nil) != (maxQuota == nil) {
		return errors.New("签到额度的最小值和最大值必须同时设置或同时留空")
	}
	if minQuota == nil {
		return nil
	}
	if *minQuota < 0 || *maxQuota < 0 {
		return errors.New("签到额度不能为负数")
	}
	if *maxQuota < *minQuota {
		return errors.New("签到最大额度不能小于最小额度")
	}
	if *maxQuota > maxUserCheckinQuotaOverride {
		return fmt.Errorf("签到额度不能超过 %d", maxUserCheckinQuotaOverride)
	}
	return nil
}

func normalizeUserPolicyModels(models []string, publicModels map[string]struct{}) ([]string, error) {
	seenModels := make(map[string]struct{}, len(models))
	normalized := make([]string, 0, len(models))
	for _, modelName := range models {
		modelName = strings.TrimSpace(modelName)
		if _, exists := publicModels[modelName]; !exists {
			return nil, fmt.Errorf("模型不是公开可用模型：%s", modelName)
		}
		if _, exists := seenModels[modelName]; exists {
			continue
		}
		seenModels[modelName] = struct{}{}
		normalized = append(normalized, modelName)
	}
	return normalized, nil
}

func UpdateUserPolicy(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
		return
	}
	var raw map[string]json.RawMessage
	if err := common.DecodeJson(c.Request.Body, &raw); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	payload, err := common.Marshal(raw)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var update model.UserPolicyUpdate
	if err := common.Unmarshal(payload, &update); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if _, exists := raw["model_blocklist_enabled"]; !exists {
		update.ModelBlocklistEnabled = user.ModelBlocklistEnabled
	}
	if _, exists := raw["model_blocklist"]; !exists {
		update.ModelBlocklist = append([]string(nil), user.ModelBlocklist...)
	}
	if _, exists := raw["checkin_enabled"]; !exists {
		update.CheckinEnabled = user.CheckinEnabled
	}
	if _, exists := raw["checkin_min_quota"]; !exists {
		update.CheckinMinQuota = user.CheckinMinQuota
	}
	if _, exists := raw["checkin_max_quota"]; !exists {
		update.CheckinMaxQuota = user.CheckinMaxQuota
	}
	if _, exists := raw["quota_cap"]; !exists {
		update.QuotaCap = user.QuotaCap
	}
	if _, exists := raw["rpm_limit"]; !exists {
		update.RpmLimit = user.RpmLimit
	}
	if _, exists := raw["concurrency_limit"]; !exists {
		update.ConcurrencyLimit = user.ConcurrencyLimit
	}
	if _, exists := raw["stream_tps_limit"]; !exists {
		update.StreamTpsLimit = user.StreamTpsLimit
	}
	if _, exists := raw["first_token_delay_ms"]; !exists {
		update.FirstTokenDelayMs = user.FirstTokenDelayMs
	}
	update, err = normalizeUserPolicyUpdate(update)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if err := model.ReplaceUserPolicy(id, update); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, id, "user.policy.update", map[string]interface{}{
		"groups":                  update.Groups,
		"primary_group":           update.PrimaryGroup,
		"topup_group":             update.TopupGroup,
		"model_limits_enabled":    update.ModelLimitsEnabled,
		"model_limit_count":       len(update.ModelLimits),
		"model_blocklist_enabled": update.ModelBlocklistEnabled,
		"model_blocklist_count":   len(update.ModelBlocklist),
		"rate_limits":             userRateLimitAudit(raw, &update),
	})
	common.ApiSuccess(c, nil)
}

func UpdateUser(c *gin.Context) {
	updatedUser, rawMutation, err := decodeManagedUserMutation(c)
	if err != nil || updatedUser.Id == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	updatedUser.Username = strings.TrimSpace(updatedUser.Username)
	if updatedUser.Username == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if updatedUser.Password == "" {
		updatedUser.Password = "$I_LOVE_U" // make Validator happy :)
	}
	if err := common.Validate.Struct(&updatedUser); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserInputInvalid, map[string]any{"Error": err.Error()})
		return
	}
	originUser, err := model.GetUserById(updatedUser.Id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if updatedUser.Role != common.RoleGuestUser && updatedUser.Role != originUser.Role {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	updatedUser.Role = originUser.Role
	myRole := c.GetInt("role")
	if !canManageTargetRole(myRole, originUser.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
		return
	}
	originPolicy := model.UserPolicyUpdate{
		Groups:                append([]string(nil), originUser.Groups...),
		PrimaryGroup:          originUser.Group,
		TopupGroup:            originUser.TopupGroup,
		ModelLimitsEnabled:    originUser.ModelLimitsEnabled,
		ModelLimits:           append([]string(nil), originUser.ModelLimits...),
		ModelBlocklistEnabled: originUser.ModelBlocklistEnabled,
		ModelBlocklist:        append([]string(nil), originUser.ModelBlocklist...),
		CheckinEnabled:        originUser.CheckinEnabled,
		CheckinMinQuota:       originUser.CheckinMinQuota,
		CheckinMaxQuota:       originUser.CheckinMaxQuota,
		QuotaCap:              originUser.QuotaCap,
		RpmLimit:              originUser.RpmLimit,
		ConcurrencyLimit:      originUser.ConcurrencyLimit,
		StreamTpsLimit:        originUser.StreamTpsLimit,
		FirstTokenDelayMs:     originUser.FirstTokenDelayMs,
	}
	policyUpdate, err := userPolicyFromMutation(updatedUser, rawMutation, &originPolicy)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	// The membership table is authoritative. Keep EditWithTx from writing a
	// legacy group value that could diverge from the policy transaction below.
	updatedUser.Group = originUser.Group
	if updatedUser.Password == "$I_LOVE_U" {
		updatedUser.Password = "" // rollback to what it should be
	}
	updatePassword := updatedUser.Password != ""
	authzTouched := false
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := updatedUser.EditWithTx(tx, updatePassword); err != nil {
			return err
		}
		if policyUpdate != nil {
			if _, err := model.ReplaceUserPolicyWithTx(tx, updatedUser.Id, *policyUpdate); err != nil {
				return err
			}
		}
		touched, err := updateAdminPermissionsForUserInTx(c, tx, updatedUser.Id, originUser.Role, updatedUser.AdminPermissions)
		authzTouched = touched
		return err
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	if authzTouched {
		if err := authz.ReloadPolicy(); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	if updatedUser.AuthVersion > originUser.AuthVersion {
		if _, err := model.RevokeAllUserSessions(updatedUser.Id, "admin_user_update"); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	if policyUpdate != nil {
		if err := model.PublishUserPolicyCache(updatedUser.Id); err != nil {
			common.ApiError(c, err)
			return
		}
		if err := model.InvalidateUserTokensCache(updatedUser.Id); err != nil {
			common.ApiError(c, err)
			return
		}
	} else if err := model.PublishUserAuthCache(updatedUser.Id); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, updatedUser.Id, "user.update", map[string]interface{}{
		"username":       originUser.Username,
		"id":             updatedUser.Id,
		"policy_updated": policyUpdate != nil,
		"rate_limits":    userRateLimitAudit(rawMutation, policyUpdate),
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func AdminClearUserBinding(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	bindingType := strings.ToLower(strings.TrimSpace(c.Param("binding_type")))
	if bindingType == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	myRole := c.GetInt("role")
	if !canManageTargetRole(myRole, user.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionSameLevel)
		return
	}

	if err := user.ClearBinding(bindingType); err != nil {
		common.ApiError(c, err)
		return
	}

	recordManageAuditFor(c, user.Id, "user.binding_clear", map[string]interface{}{
		"bindingType": bindingType,
		"username":    user.Username,
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "success",
	})
}

func UpdateSelf(c *gin.Context) {
	var requestData map[string]interface{}
	if err := common.DecodeJson(c.Request.Body, &requestData); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	// 检查是否是用户设置更新请求 (sidebar_modules 或 language)
	if sidebarModules, sidebarExists := requestData["sidebar_modules"]; sidebarExists {
		userId := c.GetInt("id")
		user, err := model.GetUserById(userId, false)
		if err != nil {
			common.ApiError(c, err)
			return
		}

		// 获取当前用户设置
		currentSetting := user.GetSetting()

		// 更新sidebar_modules字段
		if sidebarModulesStr, ok := sidebarModules.(string); ok {
			currentSetting.SidebarModules = sidebarModulesStr
		}

		if err := model.UpdateUserSetting(user.Id, currentSetting); err != nil {
			common.ApiErrorI18n(c, i18n.MsgUpdateFailed)
			return
		}

		common.ApiSuccessI18n(c, i18n.MsgUpdateSuccess, nil)
		return
	}

	// 检查是否是语言偏好更新请求
	if language, langExists := requestData["language"]; langExists {
		userId := c.GetInt("id")
		user, err := model.GetUserById(userId, false)
		if err != nil {
			common.ApiError(c, err)
			return
		}

		// 获取当前用户设置
		currentSetting := user.GetSetting()

		// 更新language字段
		if langStr, ok := language.(string); ok {
			currentSetting.Language = langStr
		}

		if err := model.UpdateUserSetting(user.Id, currentSetting); err != nil {
			common.ApiErrorI18n(c, i18n.MsgUpdateFailed)
			return
		}

		common.ApiSuccessI18n(c, i18n.MsgUpdateSuccess, nil)
		return
	}

	// 原有的用户信息更新逻辑
	var user model.User
	requestDataBytes, err := common.Marshal(requestData)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err = common.Unmarshal(requestDataBytes, &user); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	if user.Password == "" {
		user.Password = "$I_LOVE_U" // make Validator happy :)
	}
	if err := common.Validate.Struct(&user); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidInput)
		return
	}

	cleanUser := model.User{
		Id:          c.GetInt("id"),
		Username:    user.Username,
		Password:    user.Password,
		DisplayName: user.DisplayName,
	}
	if user.Password == "$I_LOVE_U" {
		user.Password = "" // rollback to what it should be
		cleanUser.Password = ""
	}
	updatePassword, err := checkUpdatePassword(user.OriginalPassword, user.Password, cleanUser.Id)
	if err != nil {
		if errors.Is(err, errUserPasswordUnset) {
			common.ApiErrorI18n(c, i18n.MsgUserPasswordUnset)
			return
		}
		if errors.Is(err, errOriginalPasswordFail) {
			common.ApiErrorI18n(c, i18n.MsgUserOriginalPasswordError)
			return
		}
		common.ApiError(c, err)
		return
	}
	if updatePassword {
		identity, ok := middleware.GetSessionAuthIdentity(c)
		if !ok {
			common.ApiError(c, errors.New("当前认证方式不支持安全验证"))
			return
		}
		if err := model.DB.Transaction(func(tx *gorm.DB) error {
			return cleanUser.UpdateWithTx(tx, true)
		}); err != nil {
			common.ApiError(c, err)
			return
		}
		if err := model.PublishUserAuthCache(cleanUser.Id); err != nil {
			common.ApiError(c, err)
			return
		}
		bundle, err := service.AdvanceCurrentSessionToUserVersion(identity, "password_changed")
		if err != nil {
			common.ApiError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"access_token":      bundle.AccessToken,
				"token_type":        bundle.TokenType,
				"access_expires_at": bundle.AccessExpiresAt,
				"session":           bundle.Session,
			},
		})
		return
	}
	if err := cleanUser.Update(false); err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
	return
}

func checkUpdatePassword(originalPassword string, newPassword string, userId int) (updatePassword bool, err error) {
	if newPassword == "" {
		return
	}
	var currentUser *model.User
	currentUser, err = model.GetUserById(userId, true)
	if err != nil {
		return
	}

	// 密码不为空,需要验证原密码
	if currentUser.Password == "" {
		err = errUserPasswordUnset
		return
	}
	if !common.ValidatePasswordAndHash(originalPassword, currentUser.Password) {
		err = errOriginalPasswordFail
		return
	}
	updatePassword = true
	return
}

func DeleteUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	originUser, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	myRole := c.GetInt("role")
	if myRole <= originUser.Role {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
		return
	}
	err = model.HardDeleteUserById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, originUser.Id, "user.delete", map[string]interface{}{
		"username": originUser.Username,
		"id":       originUser.Id,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func DeleteSelf(c *gin.Context) {
	id := c.GetInt("id")
	user, _ := model.GetUserById(id, false)

	if user.Role == common.RoleRootUser {
		common.ApiErrorI18n(c, i18n.MsgUserCannotDeleteRootUser)
		return
	}

	err := model.DeleteUserById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func CreateUser(c *gin.Context) {
	user, rawMutation, err := decodeManagedUserMutation(c)
	user.Username = strings.TrimSpace(user.Username)
	if err != nil || user.Username == "" || user.Password == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := common.Validate.Struct(&user); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserInputInvalid, map[string]any{"Error": err.Error()})
		return
	}
	if user.DisplayName == "" {
		user.DisplayName = user.Username
	}
	myRole := c.GetInt("role")
	if user.Role >= myRole {
		common.ApiErrorI18n(c, i18n.MsgUserCannotCreateHigherLevel)
		return
	}
	policyUpdate, err := userPolicyFromMutation(user, rawMutation, nil)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	// Even for admin users, we cannot fully trust them!
	cleanUser := model.User{
		Username:    user.Username,
		Password:    user.Password,
		DisplayName: user.DisplayName,
		Role:        user.Role, // 保持管理员设置的角色
		Group:       user.Group,
	}
	authzTouched := false
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := cleanUser.InsertWithTx(tx, 0); err != nil {
			return err
		}
		if policyUpdate != nil {
			if _, err := model.ReplaceUserPolicyWithTx(tx, cleanUser.Id, *policyUpdate); err != nil {
				return err
			}
		}
		touched, err := updateAdminPermissionsForUserInTx(c, tx, cleanUser.Id, cleanUser.Role, user.AdminPermissions)
		authzTouched = touched
		return err
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	if authzTouched {
		if err := authz.ReloadPolicy(); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	if policyUpdate != nil {
		if err := model.PublishUserPolicyCache(cleanUser.Id); err != nil {
			common.ApiError(c, err)
			return
		}
		if err := model.InvalidateUserTokensCache(cleanUser.Id); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	cleanUser.FinishInsert(0)

	recordManageAuditFor(c, cleanUser.Id, "user.create", map[string]interface{}{
		"username":       cleanUser.Username,
		"role":           cleanUser.Role,
		"policy_updated": policyUpdate != nil,
		"rate_limits":    userRateLimitAudit(rawMutation, policyUpdate),
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func updateAdminPermissionsForUserInTx(c *gin.Context, tx *gorm.DB, userID int, userRole int, permissions map[string]map[string]bool) (bool, error) {
	if permissions == nil {
		if userRole < common.RoleAdminUser && c.GetInt("role") == common.RoleRootUser {
			return true, authz.ClearUserAuthorizationInTx(tx, userID)
		}
		return false, nil
	}
	if c.GetInt("role") != common.RoleRootUser {
		return false, fmt.Errorf("only root can update admin permissions")
	}
	if userRole < common.RoleAdminUser {
		return true, authz.ClearUserAuthorizationInTx(tx, userID)
	}
	return true, authz.SetUserPermissionsInTx(tx, userID, permissions)
}

type ManageRequest struct {
	Id     int    `json:"id"`
	Action string `json:"action"`
	Value  int    `json:"value"`
	Mode   string `json:"mode"`
}

// ManageUser Only admin user can do this
func ManageUser(c *gin.Context) {
	var req ManageRequest
	err := common.DecodeJson(c.Request.Body, &req)

	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	user := model.User{
		Id: req.Id,
	}
	// Fill attributes
	model.DB.Unscoped().Where(&user).First(&user)
	if user.Id == 0 {
		common.ApiErrorI18n(c, i18n.MsgUserNotExists)
		return
	}
	myRole := c.GetInt("role")
	if !canManageTargetRole(myRole, user.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
		return
	}
	switch req.Action {
	case "disable":
		user.Status = common.UserStatusDisabled
		if user.Role == common.RoleRootUser {
			common.ApiErrorI18n(c, i18n.MsgUserCannotDisableRootUser)
			return
		}
	case "enable":
		user.Status = common.UserStatusEnabled
	case "delete":
		if user.Role == common.RoleRootUser {
			common.ApiErrorI18n(c, i18n.MsgUserCannotDeleteRootUser)
			return
		}
		if err := user.Delete(); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
		// 删除用户后，强制清理 Redis 中所有该用户令牌的缓存，
		// 避免已缓存的令牌在 TTL 过期前仍能通过 TokenAuth 校验。
		if err := model.InvalidateUserTokensCache(user.Id); err != nil {
			common.SysLog(fmt.Sprintf("failed to invalidate tokens cache for user %d: %s", user.Id, err.Error()))
		}
		recordManageAuditFor(c, user.Id, "user.manage", map[string]interface{}{
			"action":   req.Action,
			"username": user.Username,
			"id":       user.Id,
		})
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
		})
		return
	case "promote":
		if myRole != common.RoleRootUser {
			common.ApiErrorI18n(c, i18n.MsgUserAdminCannotPromote)
			return
		}
		if user.Role >= common.RoleAdminUser {
			common.ApiErrorI18n(c, i18n.MsgUserAlreadyAdmin)
			return
		}
		user.Role = common.RoleAdminUser
	case "demote":
		if user.Role == common.RoleRootUser {
			common.ApiErrorI18n(c, i18n.MsgUserCannotDemoteRootUser)
			return
		}
		if user.Role == common.RoleCommonUser {
			common.ApiErrorI18n(c, i18n.MsgUserAlreadyCommon)
			return
		}
		user.Role = common.RoleCommonUser
	case "add_quota":
		switch req.Mode {
		case "add":
			if req.Value <= 0 {
				common.ApiErrorI18n(c, i18n.MsgUserQuotaChangeZero)
				return
			}
			if err := model.IncreaseUserQuota(user.Id, req.Value, true); err != nil {
				common.ApiError(c, err)
				return
			}
			recordManageAuditFor(c, user.Id, "user.quota_add", map[string]interface{}{
				"quota": logger.LogQuota(req.Value),
			})
		case "subtract":
			if req.Value <= 0 {
				common.ApiErrorI18n(c, i18n.MsgUserQuotaChangeZero)
				return
			}
			if err := model.DecreaseUserQuota(user.Id, req.Value, true); err != nil {
				common.ApiError(c, err)
				return
			}
			recordManageAuditFor(c, user.Id, "user.quota_subtract", map[string]interface{}{
				"quota": logger.LogQuota(req.Value),
			})
		case "override":
			oldQuota := user.Quota
			if err := model.DB.Model(&model.User{}).Where("id = ?", user.Id).Update("quota", req.Value).Error; err != nil {
				common.ApiError(c, err)
				return
			}
			recordManageAuditFor(c, user.Id, "user.quota_override", map[string]interface{}{
				"from": logger.LogQuota(oldQuota),
				"to":   logger.LogQuota(req.Value),
			})
		default:
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
		})
		return
	default:
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	if req.Action == "demote" {
		if err := model.DB.Transaction(func(tx *gorm.DB) error {
			if err := user.UpdateWithTx(tx, false); err != nil {
				return err
			}
			return authz.ClearUserAuthorizationInTx(tx, user.Id)
		}); err != nil {
			common.ApiError(c, err)
			return
		}
		if err := authz.ReloadPolicy(); err != nil {
			common.ApiError(c, err)
			return
		}
		if err := model.PublishUserAuthCache(user.Id); err != nil {
			common.ApiError(c, err)
			return
		}
		if _, err := model.RevokeAllUserSessions(user.Id, "admin_demote"); err != nil {
			common.ApiError(c, err)
			return
		}
	} else {
		if err := user.Update(false); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	// Update/UpdateWithTx has already published the new user hash and revoked
	// browser sessions exactly once. Only PAT/relay token caches still need an
	// explicit invalidation; deleting the user hash here would discard the
	// freshly published auth-version floor.
	if err := model.InvalidateUserTokensCache(user.Id); err != nil {
		common.SysLog(fmt.Sprintf("failed to invalidate tokens cache for user %d: %s", user.Id, err.Error()))
	}
	recordManageAuditFor(c, user.Id, "user.manage", map[string]interface{}{
		"action":   req.Action,
		"username": user.Username,
		"id":       user.Id,
	})
	clearUser := model.User{
		Role:   user.Role,
		Status: user.Status,
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    clearUser,
	})
	return
}

type emailBindRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func EmailBind(c *gin.Context) {
	var req emailBindRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, errors.New("invalid request body"))
		return
	}
	email := req.Email
	email = model.NormalizeEmail(email)
	code := req.Code
	if !common.VerifyCodeWithKey(email, code, common.EmailVerificationPurpose) {
		common.ApiErrorI18n(c, i18n.MsgUserVerificationCodeError)
		return
	}
	user := model.User{
		Id: c.GetInt("id"),
	}
	if user.Id == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "not authenticated"})
		return
	}
	err := user.FillUserById()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.BindEmailToUser(&user, email); err != nil {
		if errors.Is(err, model.ErrEmailAlreadyTaken) {
			common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
			return
		}
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

type topUpRequest struct {
	Key string `json:"key"`
}

var topUpLocks sync.Map
var topUpCreateLock sync.Mutex

type topUpTryLock struct {
	ch chan struct{}
}

func newTopUpTryLock() *topUpTryLock {
	return &topUpTryLock{ch: make(chan struct{}, 1)}
}

func (l *topUpTryLock) TryLock() bool {
	select {
	case l.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

func (l *topUpTryLock) Unlock() {
	select {
	case <-l.ch:
	default:
	}
}

func getTopUpLock(userID int) *topUpTryLock {
	if v, ok := topUpLocks.Load(userID); ok {
		return v.(*topUpTryLock)
	}
	topUpCreateLock.Lock()
	defer topUpCreateLock.Unlock()
	if v, ok := topUpLocks.Load(userID); ok {
		return v.(*topUpTryLock)
	}
	l := newTopUpTryLock()
	topUpLocks.Store(userID, l)
	return l
}

func TopUp(c *gin.Context) {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		common.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
		return
	}

	id := c.GetInt("id")
	lock := getTopUpLock(id)
	if !lock.TryLock() {
		common.ApiErrorI18n(c, i18n.MsgUserTopUpProcessing)
		return
	}
	defer lock.Unlock()
	req := topUpRequest{}
	err := c.ShouldBindJSON(&req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	quota, err := model.Redeem(req.Key, id)
	if err != nil {
		// The quota cap is the user's own account state, so surfacing it leaks
		// nothing about the redemption code itself (the code stays unused).
		if errors.Is(err, model.ErrQuotaCapExceeded) {
			common.ApiErrorMsg(c, "兑换失败：该账户已达额度上限")
			return
		}
		// 不向用户暴露兑换失败的细分原因，避免攻击者根据错误类型判断兑换码状态。
		common.ApiErrorI18n(c, i18n.MsgRedeemFailed)
		logger.LogError(c, fmt.Sprintf("failed to redeem key %s for user %d: %s", req.Key, id, err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    quota,
	})
}

type UpdateUserSettingRequest struct {
	QuotaWarningType                 string  `json:"notify_type"`
	QuotaWarningThreshold            float64 `json:"quota_warning_threshold"`
	WebhookUrl                       string  `json:"webhook_url,omitempty"`
	WebhookSecret                    string  `json:"webhook_secret,omitempty"`
	NotificationEmail                string  `json:"notification_email,omitempty"`
	BarkUrl                          string  `json:"bark_url,omitempty"`
	GotifyUrl                        string  `json:"gotify_url,omitempty"`
	GotifyToken                      string  `json:"gotify_token,omitempty"`
	GotifyPriority                   int     `json:"gotify_priority,omitempty"`
	UpstreamModelUpdateNotifyEnabled *bool   `json:"upstream_model_update_notify_enabled,omitempty"`
	AcceptUnsetModelRatioModel       bool    `json:"accept_unset_model_ratio_model"`
	RecordIpLog                      bool    `json:"record_ip_log"`
}

func UpdateUserSetting(c *gin.Context) {
	var req UpdateUserSettingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	// 验证预警类型
	if req.QuotaWarningType != dto.NotifyTypeEmail && req.QuotaWarningType != dto.NotifyTypeWebhook && req.QuotaWarningType != dto.NotifyTypeBark && req.QuotaWarningType != dto.NotifyTypeGotify {
		common.ApiErrorI18n(c, i18n.MsgSettingInvalidType)
		return
	}

	// 验证预警阈值
	if req.QuotaWarningThreshold <= 0 {
		common.ApiErrorI18n(c, i18n.MsgQuotaThresholdGtZero)
		return
	}

	// 如果是webhook类型,验证webhook地址
	if req.QuotaWarningType == dto.NotifyTypeWebhook {
		if req.WebhookUrl == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingWebhookEmpty)
			return
		}
		// 验证URL格式
		if _, err := url.ParseRequestURI(req.WebhookUrl); err != nil {
			common.ApiErrorI18n(c, i18n.MsgSettingWebhookInvalid)
			return
		}
	}

	// 如果是邮件类型，验证邮箱地址
	if req.QuotaWarningType == dto.NotifyTypeEmail && req.NotificationEmail != "" {
		// 验证邮箱格式
		if !strings.Contains(req.NotificationEmail, "@") {
			common.ApiErrorI18n(c, i18n.MsgSettingEmailInvalid)
			return
		}
	}

	// 如果是Bark类型，验证Bark URL
	if req.QuotaWarningType == dto.NotifyTypeBark {
		if req.BarkUrl == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingBarkUrlEmpty)
			return
		}
		// 验证URL格式
		if _, err := url.ParseRequestURI(req.BarkUrl); err != nil {
			common.ApiErrorI18n(c, i18n.MsgSettingBarkUrlInvalid)
			return
		}
		// 检查是否是HTTP或HTTPS
		if !strings.HasPrefix(req.BarkUrl, "https://") && !strings.HasPrefix(req.BarkUrl, "http://") {
			common.ApiErrorI18n(c, i18n.MsgSettingUrlMustHttp)
			return
		}
	}

	// 如果是Gotify类型，验证Gotify URL和Token
	if req.QuotaWarningType == dto.NotifyTypeGotify {
		if req.GotifyUrl == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingGotifyUrlEmpty)
			return
		}
		if req.GotifyToken == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingGotifyTokenEmpty)
			return
		}
		// 验证URL格式
		if _, err := url.ParseRequestURI(req.GotifyUrl); err != nil {
			common.ApiErrorI18n(c, i18n.MsgSettingGotifyUrlInvalid)
			return
		}
		// 检查是否是HTTP或HTTPS
		if !strings.HasPrefix(req.GotifyUrl, "https://") && !strings.HasPrefix(req.GotifyUrl, "http://") {
			common.ApiErrorI18n(c, i18n.MsgSettingUrlMustHttp)
			return
		}
	}

	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	existingSettings := user.GetSetting()
	upstreamModelUpdateNotifyEnabled := existingSettings.UpstreamModelUpdateNotifyEnabled
	if user.Role >= common.RoleAdminUser && req.UpstreamModelUpdateNotifyEnabled != nil {
		upstreamModelUpdateNotifyEnabled = *req.UpstreamModelUpdateNotifyEnabled
	}

	// 构建设置
	settings := dto.UserSetting{
		NotifyType:                       req.QuotaWarningType,
		QuotaWarningThreshold:            req.QuotaWarningThreshold,
		UpstreamModelUpdateNotifyEnabled: upstreamModelUpdateNotifyEnabled,
		AcceptUnsetRatioModel:            req.AcceptUnsetModelRatioModel,
		RecordIpLog:                      req.RecordIpLog,
	}

	// 如果是webhook类型,添加webhook相关设置
	if req.QuotaWarningType == dto.NotifyTypeWebhook {
		settings.WebhookUrl = req.WebhookUrl
		if req.WebhookSecret != "" {
			settings.WebhookSecret = req.WebhookSecret
		}
	}

	// 如果提供了通知邮箱，添加到设置中
	if req.QuotaWarningType == dto.NotifyTypeEmail && req.NotificationEmail != "" {
		settings.NotificationEmail = req.NotificationEmail
	}

	// 如果是Bark类型，添加Bark URL到设置中
	if req.QuotaWarningType == dto.NotifyTypeBark {
		settings.BarkUrl = req.BarkUrl
	}

	// 如果是Gotify类型，添加Gotify配置到设置中
	if req.QuotaWarningType == dto.NotifyTypeGotify {
		settings.GotifyUrl = req.GotifyUrl
		settings.GotifyToken = req.GotifyToken
		// Gotify优先级范围0-10，超出范围则使用默认值5
		if req.GotifyPriority < 0 || req.GotifyPriority > 10 {
			settings.GotifyPriority = 5
		} else {
			settings.GotifyPriority = req.GotifyPriority
		}
	}

	// 更新用户设置
	if err := model.UpdateUserSetting(user.Id, settings); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUpdateFailed)
		return
	}

	common.ApiSuccessI18n(c, i18n.MsgSettingSaved, nil)
}
