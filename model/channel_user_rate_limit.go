package model

import (
	"database/sql/driver"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// maxChannelUserRateLimits keeps the serialized JSON well below MySQL TEXT's
// 64 KiB payload limit on the shared channel table.
const maxChannelUserRateLimits = 500

// ChannelUserRateLimit is a per-user override of the channel-wide request
// limits. It applies only to the channel that owns it.
type ChannelUserRateLimit struct {
	UserId           int    `json:"user_id"`
	Username         string `json:"username,omitempty" gorm:"-"`
	RPMLimit         *int   `json:"rpm_limit"`
	ConcurrencyLimit *int   `json:"concurrency_limit"`
}

type ChannelUserRateLimits []ChannelUserRateLimit

func (limits ChannelUserRateLimits) Value() (driver.Value, error) {
	normalized, err := limits.normalized()
	if err != nil {
		return nil, err
	}
	data, err := common.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func (limits *ChannelUserRateLimits) Scan(value any) error {
	*limits = nil
	if value == nil {
		return nil
	}
	var data []byte
	switch typed := value.(type) {
	case []byte:
		data = typed
	case string:
		data = []byte(typed)
	default:
		return fmt.Errorf("unsupported channel user rate limits type %T", value)
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil
	}
	var parsed ChannelUserRateLimits
	if err := common.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("invalid channel user rate limits: %w", err)
	}
	normalized, err := parsed.normalized()
	if err != nil {
		return err
	}
	*limits = normalized
	return nil
}

func (limits ChannelUserRateLimits) Validate() error {
	_, err := limits.normalized()
	return err
}

func (limits ChannelUserRateLimits) ForUser(userId int) (ChannelUserRateLimit, bool) {
	for _, limit := range limits {
		if limit.UserId == userId {
			return limit, true
		}
	}
	return ChannelUserRateLimit{}, false
}

func (limits ChannelUserRateLimits) UserIDs() []int {
	ids := make([]int, 0, len(limits))
	for _, limit := range limits {
		ids = append(ids, limit.UserId)
	}
	return ids
}

func (limits ChannelUserRateLimits) Equal(other ChannelUserRateLimits) bool {
	if len(limits) != len(other) {
		return false
	}
	byUser := make(map[int]ChannelUserRateLimit, len(other))
	for _, limit := range other {
		byUser[limit.UserId] = limit
	}
	for _, limit := range limits {
		otherLimit, exists := byUser[limit.UserId]
		if !exists ||
			!equalChannelUserLimit(limit.RPMLimit, otherLimit.RPMLimit) ||
			!equalChannelUserLimit(limit.ConcurrencyLimit, otherLimit.ConcurrencyLimit) {
			return false
		}
	}
	return true
}

func equalChannelUserLimit(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (limits ChannelUserRateLimits) normalized() (ChannelUserRateLimits, error) {
	if len(limits) > maxChannelUserRateLimits {
		return nil, fmt.Errorf("at most %d users can be configured", maxChannelUserRateLimits)
	}
	normalized := make(ChannelUserRateLimits, 0, len(limits))
	seen := make(map[int]struct{}, len(limits))
	for _, limit := range limits {
		if limit.UserId <= 0 {
			return nil, fmt.Errorf("user_id must be positive")
		}
		if _, exists := seen[limit.UserId]; exists {
			return nil, fmt.Errorf("duplicate user_id %d", limit.UserId)
		}
		seen[limit.UserId] = struct{}{}
		if limit.RPMLimit == nil && limit.ConcurrencyLimit == nil {
			continue
		}
		if err := validateChannelUserLimit("rpm_limit", limit.RPMLimit); err != nil {
			return nil, err
		}
		if err := validateChannelUserLimit("concurrency_limit", limit.ConcurrencyLimit); err != nil {
			return nil, err
		}
		// Usernames are display-only and resolved from the users table when a
		// channel is read; keeping them out avoids stale copied values.
		limit.Username = ""
		normalized = append(normalized, limit)
	}
	return normalized, nil
}

func validateChannelUserLimit(name string, value *int) error {
	if value == nil {
		return nil
	}
	if *value < 1 || int64(*value) > math.MaxInt32 {
		return fmt.Errorf("%s must be between 1 and %d", name, math.MaxInt32)
	}
	return nil
}

// HydrateChannelUserRateLimitUsernames resolves display names for the user
// IDs stored on a channel. It is only used by admin read/write responses and
// never on the relay hot path.
func HydrateChannelUserRateLimitUsernames(channel *Channel) {
	if channel == nil || len(channel.UserRateLimits) == 0 {
		return
	}
	ids := make([]int, 0, len(channel.UserRateLimits))
	for _, limit := range channel.UserRateLimits {
		ids = append(ids, limit.UserId)
	}
	var users []struct {
		Id          int
		Username    string
		DisplayName string `gorm:"column:display_name"`
	}
	if err := DB.Unscoped().Model(&User{}).Select("id, username, display_name").Where("id IN ?", ids).Find(&users).Error; err != nil {
		return
	}
	usernames := make(map[int]string, len(users))
	for _, user := range users {
		if user.DisplayName != "" {
			usernames[user.Id] = user.DisplayName
			continue
		}
		usernames[user.Id] = user.Username
	}
	for index := range channel.UserRateLimits {
		channel.UserRateLimits[index].Username = usernames[channel.UserRateLimits[index].UserId]
	}
}
