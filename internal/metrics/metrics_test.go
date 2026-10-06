package metrics

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecord(t *testing.T) {
	var buf bytes.Buffer
	m := New(&buf, map[string]string{"Stack": "test"})
	m.now = func() time.Time { return time.UnixMilli(1000) }

	m.Record("Pins", 1, UnitCount, map[string]string{"Outcome": "pinned"}, map[string]any{"guild_id": "123"})

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line))

	assert.Equal(t, map[string]any{
		"_aws": map[string]any{
			"Timestamp": float64(1000),
			"CloudWatchMetrics": []any{map[string]any{
				"Namespace":  "Pinbot",
				"Dimensions": []any{[]any{"Outcome", "Stack"}},
				"Metrics":    []any{map[string]any{"Name": "Pins", "Unit": "Count"}},
			}},
		},
		"Stack":    "test",
		"Outcome":  "pinned",
		"Pins":     float64(1),
		"guild_id": "123",
	}, line)
}

func TestRecord_Nil(t *testing.T) {
	var m *Metrics

	assert.NotPanics(t, func() {
		m.Record("Pins", 1, UnitCount, nil, nil)
	})
}
