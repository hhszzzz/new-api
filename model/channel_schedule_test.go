package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func channelScheduleTestTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, channelScheduleLocation)
	require.NoError(t, err)
	return parsed
}

func channelScheduleTimestamp(t *testing.T, value string) *int64 {
	t.Helper()
	timestamp := channelScheduleTestTime(t, value).Unix()
	return &timestamp
}

func TestEmptyChannelScheduleIsAvailableWithoutTransition(t *testing.T) {
	state := (ChannelSchedule{}).StateAt(channelScheduleTestTime(t, "2026-07-27 12:00"))

	assert.True(t, state.Available)
	assert.Equal(t, ChannelScheduleReasonNone, state.Reason)
	assert.Nil(t, state.NextTransitionAt)
}

func TestChannelScheduleTimeBoundaries(t *testing.T) {
	schedule := ChannelSchedule{
		StartsAt:    channelScheduleTimestamp(t, "2026-07-27 09:00"),
		PausedUntil: channelScheduleTimestamp(t, "2026-07-27 10:00"),
		ExpiresAt:   channelScheduleTimestamp(t, "2026-07-27 18:00"),
	}
	require.NoError(t, schedule.Normalize())

	tests := []struct {
		name      string
		now       string
		available bool
		reason    string
	}{
		{name: "before start", now: "2026-07-27 08:59", reason: ChannelScheduleReasonBeforeStart},
		{name: "paused at start", now: "2026-07-27 09:00", reason: ChannelScheduleReasonPaused},
		{name: "available when pause ends", now: "2026-07-27 10:00", available: true, reason: ChannelScheduleReasonNone},
		{name: "available before expiry", now: "2026-07-27 17:59", available: true, reason: ChannelScheduleReasonNone},
		{name: "expired at boundary", now: "2026-07-27 18:00", reason: ChannelScheduleReasonExpired},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := schedule.StateAt(channelScheduleTestTime(t, test.now))
			assert.Equal(t, test.available, state.Available)
			assert.Equal(t, test.reason, state.Reason)
		})
	}
}

func TestChannelScheduleWeeklyWindowsSupportAllDayMultipleAndCrossMidnight(t *testing.T) {
	schedule := ChannelSchedule{
		WeeklyEnabled: true,
		WeeklyWindows: map[string][]ChannelScheduleWindow{
			"monday": {
				{Start: "09:00", End: "12:00"},
				{Start: "14:00", End: "18:00"},
				{Start: "22:00", End: "02:00"},
			},
			"wednesday": {{AllDay: true}},
		},
	}
	require.NoError(t, schedule.Normalize())

	tests := []struct {
		at        string
		available bool
	}{
		{at: "2026-07-27 09:00", available: true},
		{at: "2026-07-27 12:00", available: false},
		{at: "2026-07-27 14:30", available: true},
		{at: "2026-07-27 23:30", available: true},
		{at: "2026-07-28 01:59", available: true},
		{at: "2026-07-28 02:00", available: false},
		{at: "2026-07-29 15:00", available: true},
	}
	for _, test := range tests {
		assert.Equal(t, test.available, schedule.IsAvailableAt(channelScheduleTestTime(t, test.at)), test.at)
	}
}

func TestChannelScheduleNextTransitionSkipsBoundariesBlockedByOtherRules(t *testing.T) {
	schedule := ChannelSchedule{
		StartsAt:      channelScheduleTimestamp(t, "2026-08-03 13:00"),
		WeeklyEnabled: true,
		WeeklyWindows: map[string][]ChannelScheduleWindow{
			"monday": {{Start: "14:00", End: "16:00"}},
		},
	}
	require.NoError(t, schedule.Normalize())

	state := schedule.StateAt(channelScheduleTestTime(t, "2026-07-27 12:00"))
	require.False(t, state.Available)
	require.NotNil(t, state.NextTransitionAt)
	assert.Equal(t, channelScheduleTestTime(t, "2026-08-03 14:00").Unix(), *state.NextTransitionAt)
}

func TestChannelScheduleNextTransitionEndsCrossMidnightWindow(t *testing.T) {
	schedule := ChannelSchedule{
		WeeklyEnabled: true,
		WeeklyWindows: map[string][]ChannelScheduleWindow{
			"monday": {{Start: "22:00", End: "02:00"}},
		},
	}
	require.NoError(t, schedule.Normalize())

	state := schedule.StateAt(channelScheduleTestTime(t, "2026-07-28 01:00"))
	require.True(t, state.Available)
	require.NotNil(t, state.NextTransitionAt)
	assert.Equal(t, channelScheduleTestTime(t, "2026-07-28 02:00").Unix(), *state.NextTransitionAt)
}

func TestChannelScheduleValidationRejectsInvalidConfiguration(t *testing.T) {
	expiresAt := channelScheduleTestTime(t, "2026-07-27 09:00").Unix()
	startsAt := expiresAt
	tests := []ChannelSchedule{
		{Timezone: "UTC"},
		{StartsAt: &startsAt, ExpiresAt: &expiresAt},
		{WeeklyEnabled: true, WeeklyWindows: map[string][]ChannelScheduleWindow{"funday": {{AllDay: true}}}},
		{WeeklyEnabled: true, WeeklyWindows: map[string][]ChannelScheduleWindow{"monday": {{Start: "9:00", End: "12:00"}}}},
		{WeeklyEnabled: true, WeeklyWindows: map[string][]ChannelScheduleWindow{"monday": {{Start: "09:00", End: "09:00"}}}},
	}
	for _, schedule := range tests {
		require.Error(t, schedule.Normalize())
	}
}

func TestChannelEffectiveStatusPreservesBaseStatusPriority(t *testing.T) {
	future := time.Now().Add(time.Hour).Unix()
	tests := []struct {
		status   int
		expected string
	}{
		{status: common.ChannelStatusManuallyDisabled, expected: ChannelEffectiveStatusManualDisabled},
		{status: common.ChannelStatusAutoDisabled, expected: ChannelEffectiveStatusAutoDisabled},
		{status: common.ChannelStatusEnabled, expected: ChannelEffectiveStatusScheduledDisabled},
	}
	for _, test := range tests {
		channel := Channel{Status: test.status, Schedule: ChannelSchedule{StartsAt: &future}}
		channel.PopulateScheduleStateAt(time.Now())
		assert.Equal(t, test.expected, channel.EffectiveStatus)
	}
}
