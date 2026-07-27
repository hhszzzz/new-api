package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testConfigWithMap struct {
	Modes map[string]string `json:"modes"`
	Exprs map[string]string `json:"exprs"`
	Name  string            `json:"name"`
}

func TestUpdateConfigFromMap_MapReplacement(t *testing.T) {
	cfg := &testConfigWithMap{
		Modes: map[string]string{
			"model-a": "tiered_expr",
			"model-b": "tiered_expr",
		},
		Exprs: map[string]string{
			"model-a": "p * 5 + c * 25",
			"model-b": "p * 10 + c * 50",
		},
		Name: "billing",
	}

	// Simulate removing model-a: new value only has model-b
	err := UpdateConfigFromMap(cfg, map[string]string{
		"modes": `{"model-b": "tiered_expr"}`,
		"exprs": `{"model-b": "p * 10 + c * 50"}`,
	})
	if err != nil {
		t.Fatalf("UpdateConfigFromMap failed: %v", err)
	}

	if _, ok := cfg.Modes["model-a"]; ok {
		t.Errorf("Modes still contains model-a after it was removed from the update; got %v", cfg.Modes)
	}
	if _, ok := cfg.Exprs["model-a"]; ok {
		t.Errorf("Exprs still contains model-a after it was removed from the update; got %v", cfg.Exprs)
	}

	if cfg.Modes["model-b"] != "tiered_expr" {
		t.Errorf("Modes[model-b] = %q, want %q", cfg.Modes["model-b"], "tiered_expr")
	}
	if cfg.Exprs["model-b"] != "p * 10 + c * 50" {
		t.Errorf("Exprs[model-b] = %q, want %q", cfg.Exprs["model-b"], "p * 10 + c * 50")
	}
}

func TestUpdateConfigFromMap_EmptyMapClearsAll(t *testing.T) {
	cfg := &testConfigWithMap{
		Modes: map[string]string{
			"model-a": "tiered_expr",
		},
		Exprs: map[string]string{
			"model-a": "p * 5 + c * 25",
		},
	}

	err := UpdateConfigFromMap(cfg, map[string]string{
		"modes": `{}`,
		"exprs": `{}`,
	})
	if err != nil {
		t.Fatalf("UpdateConfigFromMap failed: %v", err)
	}

	if len(cfg.Modes) != 0 {
		t.Errorf("Modes should be empty after updating with {}, got %v", cfg.Modes)
	}
	if len(cfg.Exprs) != 0 {
		t.Errorf("Exprs should be empty after updating with {}, got %v", cfg.Exprs)
	}
}

func TestUpdateConfigFromMap_ScalarFieldsUnchanged(t *testing.T) {
	cfg := &testConfigWithMap{
		Modes: map[string]string{"m": "v"},
		Name:  "old",
	}

	err := UpdateConfigFromMap(cfg, map[string]string{
		"name": "new",
	})
	if err != nil {
		t.Fatalf("UpdateConfigFromMap failed: %v", err)
	}

	if cfg.Name != "new" {
		t.Errorf("Name = %q, want %q", cfg.Name, "new")
	}
	// modes was not in configMap, should remain unchanged
	if cfg.Modes["m"] != "v" {
		t.Errorf("Modes should be unchanged, got %v", cfg.Modes)
	}
}

func TestUpdateConfigFromMapRejectsPartialUpdate(t *testing.T) {
	type atomicConfig struct {
		Name  string `json:"name"`
		Limit int8   `json:"limit"`
	}

	cfg := &atomicConfig{Name: "before", Limit: 7}
	err := UpdateConfigFromMap(cfg, map[string]string{
		"name":  "after",
		"limit": "128",
	})

	require.Error(t, err)
	assert.Equal(t, &atomicConfig{Name: "before", Limit: 7}, cfg)
}

func TestUpdateConfigFromMapUsesJSONTagNameWithoutOptions(t *testing.T) {
	type taggedConfig struct {
		ChannelIDs []int `json:"channel_ids,omitempty"`
	}

	cfg := &taggedConfig{ChannelIDs: []int{1}}
	require.NoError(t, UpdateConfigFromMap(cfg, map[string]string{
		"channel_ids": `[2,3]`,
	}))
	assert.Equal(t, []int{2, 3}, cfg.ChannelIDs)

	values, err := ConfigToMap(cfg)
	require.NoError(t, err)
	assert.JSONEq(t, `[2,3]`, values["channel_ids"])
	assert.NotContains(t, values, "channel_ids,omitempty")
}

func TestConfigManagerPersistedLoadIgnoresUnknownFields(t *testing.T) {
	type rollingConfig struct {
		Name string `json:"name"`
	}

	manager := NewConfigManager()
	cfg := &rollingConfig{Name: "before"}
	manager.Register("rolling", cfg)

	require.NoError(t, manager.LoadFromDB(map[string]string{
		"rolling.name":             "after",
		"rolling.newer_node_field": "future-value",
	}))
	assert.Equal(t, "after", cfg.Name)

	handled, err := manager.UpdateFromDB("rolling", map[string]string{
		"newer_node_field": "future-value",
	})
	require.True(t, handled)
	require.NoError(t, err)
	assert.Equal(t, "after", cfg.Name)
}

func TestConfigManagerOnlineUpdateRejectsUnknownFields(t *testing.T) {
	type strictConfig struct {
		Name string `json:"name"`
	}

	manager := NewConfigManager()
	cfg := &strictConfig{Name: "before"}
	manager.Register("strict", cfg)

	handled, err := manager.Update("strict", map[string]string{
		"name": "after",
		"typo": "value",
	})
	require.True(t, handled)
	require.ErrorContains(t, err, "unknown config field typo")
	assert.Equal(t, "before", cfg.Name)
}
