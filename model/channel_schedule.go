package model

import (
	"database/sql/driver"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const ChannelScheduleTimezone = "Asia/Shanghai"

const (
	ChannelEffectiveStatusUnknown           = "unknown"
	ChannelEffectiveStatusEnabled           = "enabled"
	ChannelEffectiveStatusManualDisabled    = "manual_disabled"
	ChannelEffectiveStatusAutoDisabled      = "auto_disabled"
	ChannelEffectiveStatusScheduledDisabled = "scheduled_disabled"
)

const (
	ChannelScheduleReasonNone          = "none"
	ChannelScheduleReasonBeforeStart   = "before_start"
	ChannelScheduleReasonPaused        = "paused"
	ChannelScheduleReasonExpired       = "expired"
	ChannelScheduleReasonOutsideWindow = "outside_weekly_window"
)

var channelScheduleWeekdays = []string{
	"sunday",
	"monday",
	"tuesday",
	"wednesday",
	"thursday",
	"friday",
	"saturday",
}

var channelScheduleLocation = time.FixedZone(ChannelScheduleTimezone, 8*60*60)

type ChannelScheduleWindow struct {
	Start  string `json:"start,omitempty"`
	End    string `json:"end,omitempty"`
	AllDay bool   `json:"all_day,omitempty"`
}

type ChannelSchedule struct {
	Timezone      string                             `json:"timezone,omitempty"`
	StartsAt      *int64                             `json:"starts_at,omitempty"`
	ExpiresAt     *int64                             `json:"expires_at,omitempty"`
	PausedUntil   *int64                             `json:"paused_until,omitempty"`
	WeeklyEnabled bool                               `json:"weekly_enabled,omitempty"`
	WeeklyWindows map[string][]ChannelScheduleWindow `json:"weekly_windows,omitempty"`
}

type ChannelScheduleState struct {
	Available        bool   `json:"available"`
	Reason           string `json:"reason"`
	NextTransitionAt *int64 `json:"next_transition_at,omitempty"`
}

func (schedule ChannelSchedule) Value() (driver.Value, error) {
	if err := schedule.Normalize(); err != nil {
		return nil, err
	}
	return common.Marshal(schedule)
}

func (schedule *ChannelSchedule) Scan(value any) error {
	*schedule = ChannelSchedule{}
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
		return fmt.Errorf("unsupported channel schedule type %T", value)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	if err := common.Unmarshal(data, schedule); err != nil {
		return fmt.Errorf("invalid channel schedule: %w", err)
	}
	return schedule.Normalize()
}

func (schedule *ChannelSchedule) Normalize() error {
	if schedule == nil {
		return nil
	}
	if schedule.Timezone != "" && schedule.Timezone != ChannelScheduleTimezone {
		return fmt.Errorf("channel schedule timezone must be %s", ChannelScheduleTimezone)
	}
	schedule.Timezone = ChannelScheduleTimezone

	for name, value := range map[string]*int64{
		"starts_at":    schedule.StartsAt,
		"expires_at":   schedule.ExpiresAt,
		"paused_until": schedule.PausedUntil,
	} {
		if value != nil && *value <= 0 {
			return fmt.Errorf("channel schedule %s must be a positive Unix timestamp", name)
		}
	}
	if schedule.StartsAt != nil && schedule.ExpiresAt != nil && *schedule.StartsAt >= *schedule.ExpiresAt {
		return fmt.Errorf("channel schedule starts_at must be earlier than expires_at")
	}

	if !schedule.WeeklyEnabled {
		schedule.WeeklyWindows = nil
		return nil
	}
	if schedule.WeeklyWindows == nil {
		schedule.WeeklyWindows = map[string][]ChannelScheduleWindow{}
	}
	validWeekdays := make(map[string]struct{}, len(channelScheduleWeekdays))
	for _, weekday := range channelScheduleWeekdays {
		validWeekdays[weekday] = struct{}{}
	}
	for weekday, windows := range schedule.WeeklyWindows {
		if _, ok := validWeekdays[weekday]; !ok {
			return fmt.Errorf("invalid channel schedule weekday %q", weekday)
		}
		if len(windows) > 24 {
			return fmt.Errorf("channel schedule weekday %q has too many windows", weekday)
		}
		normalized := make([]ChannelScheduleWindow, 0, len(windows))
		for _, window := range windows {
			if window.AllDay {
				normalized = []ChannelScheduleWindow{{AllDay: true}}
				break
			}
			window.Start = strings.TrimSpace(window.Start)
			window.End = strings.TrimSpace(window.End)
			startMinute, err := parseChannelScheduleClock(window.Start)
			if err != nil {
				return fmt.Errorf("invalid %s start time: %w", weekday, err)
			}
			endMinute, err := parseChannelScheduleClock(window.End)
			if err != nil {
				return fmt.Errorf("invalid %s end time: %w", weekday, err)
			}
			if startMinute == endMinute {
				return fmt.Errorf("channel schedule window on %s must not have identical start and end times", weekday)
			}
			normalized = append(normalized, window)
		}
		schedule.WeeklyWindows[weekday] = normalized
	}
	return nil
}

func parseChannelScheduleClock(value string) (int, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, fmt.Errorf("time must use HH:mm format")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, fmt.Errorf("hour must be between 00 and 23")
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("minute must be between 00 and 59")
	}
	return hour*60 + minute, nil
}

func (schedule ChannelSchedule) IsAvailableAt(now time.Time) bool {
	return schedule.reasonAt(now) == ChannelScheduleReasonNone
}

func (schedule ChannelSchedule) StateAt(now time.Time) ChannelScheduleState {
	reason := schedule.reasonAt(now)
	state := ChannelScheduleState{
		Available: reason == ChannelScheduleReasonNone,
		Reason:    reason,
	}
	if next := schedule.nextTransitionAt(now, state.Available); !next.IsZero() {
		timestamp := next.Unix()
		state.NextTransitionAt = &timestamp
	}
	return state
}

func (schedule ChannelSchedule) reasonAt(now time.Time) string {
	nowUnix := now.Unix()
	if schedule.ExpiresAt != nil && nowUnix >= *schedule.ExpiresAt {
		return ChannelScheduleReasonExpired
	}
	if schedule.StartsAt != nil && nowUnix < *schedule.StartsAt {
		return ChannelScheduleReasonBeforeStart
	}
	if schedule.PausedUntil != nil && nowUnix < *schedule.PausedUntil {
		return ChannelScheduleReasonPaused
	}
	if schedule.WeeklyEnabled && !schedule.weeklyAvailableAt(now) {
		return ChannelScheduleReasonOutsideWindow
	}
	return ChannelScheduleReasonNone
}

func (schedule ChannelSchedule) weeklyAvailableAt(now time.Time) bool {
	localNow := now.In(channelScheduleLocation)
	minuteOfDay := localNow.Hour()*60 + localNow.Minute()
	weekday := channelScheduleWeekdays[int(localNow.Weekday())]
	for _, window := range schedule.WeeklyWindows[weekday] {
		if window.AllDay {
			return true
		}
		startMinute, startErr := parseChannelScheduleClock(window.Start)
		endMinute, endErr := parseChannelScheduleClock(window.End)
		if startErr != nil || endErr != nil {
			continue
		}
		if startMinute < endMinute && minuteOfDay >= startMinute && minuteOfDay < endMinute {
			return true
		}
		if startMinute > endMinute && minuteOfDay >= startMinute {
			return true
		}
	}

	previousDay := localNow.AddDate(0, 0, -1)
	previousWeekday := channelScheduleWeekdays[int(previousDay.Weekday())]
	for _, window := range schedule.WeeklyWindows[previousWeekday] {
		if window.AllDay {
			continue
		}
		startMinute, startErr := parseChannelScheduleClock(window.Start)
		endMinute, endErr := parseChannelScheduleClock(window.End)
		if startErr == nil && endErr == nil && startMinute > endMinute && minuteOfDay < endMinute {
			return true
		}
	}
	return false
}

func (schedule ChannelSchedule) nextTransitionAt(now time.Time, currentlyAvailable bool) time.Time {
	if schedule.ExpiresAt != nil && now.Unix() >= *schedule.ExpiresAt {
		return time.Time{}
	}

	candidates := make(map[int64]struct{})
	anchors := []time.Time{now}
	for _, timestamp := range []*int64{schedule.StartsAt, schedule.PausedUntil, schedule.ExpiresAt} {
		if timestamp == nil || *timestamp <= now.Unix() {
			continue
		}
		candidates[*timestamp] = struct{}{}
		anchors = append(anchors, time.Unix(*timestamp, 0))
	}
	if schedule.WeeklyEnabled {
		for _, anchor := range anchors {
			schedule.addWeeklyTransitionCandidates(anchor, candidates)
		}
	}

	timestamps := make([]int64, 0, len(candidates))
	for timestamp := range candidates {
		if timestamp > now.Unix() {
			timestamps = append(timestamps, timestamp)
		}
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })
	for _, timestamp := range timestamps {
		candidate := time.Unix(timestamp, 0)
		if schedule.IsAvailableAt(candidate) != currentlyAvailable {
			return candidate
		}
	}
	return time.Time{}
}

func (schedule ChannelSchedule) addWeeklyTransitionCandidates(anchor time.Time, candidates map[int64]struct{}) {
	localAnchor := anchor.In(channelScheduleLocation)
	startOfDay := time.Date(localAnchor.Year(), localAnchor.Month(), localAnchor.Day(), 0, 0, 0, 0, channelScheduleLocation)
	// Include the previous day so a cross-midnight window that is active just
	// after midnight contributes its end boundary on the current day.
	for dayOffset := -1; dayOffset <= 8; dayOffset++ {
		day := startOfDay.AddDate(0, 0, dayOffset)
		candidates[day.Unix()] = struct{}{}
		candidates[day.AddDate(0, 0, 1).Unix()] = struct{}{}
		weekday := channelScheduleWeekdays[int(day.Weekday())]
		for _, window := range schedule.WeeklyWindows[weekday] {
			if window.AllDay {
				continue
			}
			startMinute, startErr := parseChannelScheduleClock(window.Start)
			endMinute, endErr := parseChannelScheduleClock(window.End)
			if startErr != nil || endErr != nil {
				continue
			}
			candidates[day.Add(time.Duration(startMinute)*time.Minute).Unix()] = struct{}{}
			end := day.Add(time.Duration(endMinute) * time.Minute)
			if startMinute > endMinute {
				end = end.AddDate(0, 0, 1)
			}
			candidates[end.Unix()] = struct{}{}
		}
	}
}

func (channel *Channel) IsSchedulableAt(now time.Time) bool {
	return channel != nil && channel.Status == common.ChannelStatusEnabled && channel.Schedule.IsAvailableAt(now)
}

func (channel *Channel) PopulateScheduleStateAt(now time.Time) {
	if channel == nil {
		return
	}
	channel.ScheduleState = channel.Schedule.StateAt(now)
	switch channel.Status {
	case common.ChannelStatusManuallyDisabled:
		channel.EffectiveStatus = ChannelEffectiveStatusManualDisabled
	case common.ChannelStatusAutoDisabled:
		channel.EffectiveStatus = ChannelEffectiveStatusAutoDisabled
	case common.ChannelStatusEnabled:
		if channel.ScheduleState.Available {
			channel.EffectiveStatus = ChannelEffectiveStatusEnabled
		} else {
			channel.EffectiveStatus = ChannelEffectiveStatusScheduledDisabled
		}
	default:
		channel.EffectiveStatus = ChannelEffectiveStatusUnknown
	}
}
