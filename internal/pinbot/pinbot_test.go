package pinbot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bwmarrin/discordgo"
	bot_lambda "github.com/elliotwms/bot-lambda"
	"github.com/elliotwms/bot-lambda/sessionprovider"
	"github.com/elliotwms/pinbot/internal/handlers"
	"github.com/elliotwms/pinbot/internal/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommands(t *testing.T) {
	e := New(nil, nil, slog.Default(), nil)

	assert.Equal(t, []*discordgo.ApplicationCommand{handlers.PinCommand}, e.Commands())
}

func TestReportMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v9/applications/@me", r.URL.Path)
		_ = json.NewEncoder(w).Encode(bot_lambda.Application{ID: "1", ApproximateGuildCount: 42, ApproximateUserInstallCount: 7})
	}))
	t.Cleanup(server.Close)

	endpoint := discordgo.EndpointApplications
	discordgo.EndpointApplications = server.URL + "/api/v9/applications"
	t.Cleanup(func() { discordgo.EndpointApplications = endpoint })

	s, err := discordgo.New("Bot token")
	require.NoError(t, err)

	var buf bytes.Buffer
	e := New(nil, sessionprovider.Static(s), slog.Default(), metrics.New(&buf, map[string]string{"Stack": "test"}))

	_, err = e.HandleTask(context.Background(), &bot_lambda.TaskRequest{Task: TaskReportMetrics})
	require.NoError(t, err)

	recorded := map[string]float64{}
	scanner := bufio.NewScanner(&buf)
	for scanner.Scan() {
		var line map[string]any
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &line))
		for _, name := range []string{"Guilds", "UserInstalls"} {
			if v, ok := line[name].(float64); ok {
				recorded[name] = v
			}
		}
	}

	assert.Equal(t, map[string]float64{"Guilds": 42, "UserInstalls": 7}, recorded)
}
