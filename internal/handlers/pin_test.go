package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testMessage(attachments ...*discordgo.MessageAttachment) *discordgo.Message {
	return &discordgo.Message{
		ID:          "3",
		ChannelID:   "2",
		Content:     "Hello, World!",
		Author:      &discordgo.User{ID: "4", Username: "user"},
		Timestamp:   time.Now(),
		Attachments: attachments,
	}
}

func image(url string) *discordgo.MessageAttachment {
	return &discordgo.MessageAttachment{URL: url, Width: 100, Height: 100}
}

func file(url string) *discordgo.MessageAttachment {
	return &discordgo.MessageAttachment{URL: url}
}

func TestBuildPinMessage_FirstImageAfterFileIsInMainEmbed(t *testing.T) {
	source := &discordgo.Channel{ID: "2", GuildID: "1", Name: "test"}
	m := testMessage(file("hello.txt"), image("cheese.jpg"))

	pin := buildPinMessage(source, m, nil)

	require.Len(t, pin.Embeds, 1)
	require.NotNil(t, pin.Embeds[0].Image)
	assert.Equal(t, "cheese.jpg", pin.Embeds[0].Image.URL)
}

func TestBuildPinMessage_AdditionalImagesInSeparateEmbeds(t *testing.T) {
	source := &discordgo.Channel{ID: "2", GuildID: "1", Name: "test"}
	m := testMessage(image("a.jpg"), file("hello.txt"), image("b.jpg"))

	pin := buildPinMessage(source, m, nil)

	require.Len(t, pin.Embeds, 2)
	assert.Equal(t, "a.jpg", pin.Embeds[0].Image.URL)
	assert.Equal(t, "b.jpg", pin.Embeds[1].Image.URL)
}

func TestBuildPinMessage_LimitsEmbeds(t *testing.T) {
	source := &discordgo.Channel{ID: "2", GuildID: "1", Name: "test"}

	var attachments []*discordgo.MessageAttachment
	for range 8 {
		attachments = append(attachments, image("image.jpg"))
	}
	m := testMessage(attachments...)
	for range 5 {
		m.Embeds = append(m.Embeds, &discordgo.MessageEmbed{URL: "https://example.com"})
	}

	pin := buildPinMessage(source, m, nil)

	require.Len(t, pin.Embeds, maxEmbeds)
	assert.Equal(t, "📌 Pinned", pin.Embeds[0].Title)
}

func TestGetTargetChannel(t *testing.T) {
	text := func(id, name string) *discordgo.Channel {
		return &discordgo.Channel{ID: id, Name: name, Type: discordgo.ChannelTypeGuildText}
	}
	thread := &discordgo.Channel{ID: "10", Name: "a thread", ParentID: "1", Type: discordgo.ChannelTypeGuildPublicThread}

	testCases := []struct {
		name     string
		channels []*discordgo.Channel
		origin   *discordgo.Channel
		expected string
	}{
		{
			name:     "same channel",
			channels: []*discordgo.Channel{text("1", "test")},
			origin:   text("1", "test"),
			expected: "1",
		},
		{
			name:     "general pins channel",
			channels: []*discordgo.Channel{text("1", "test"), text("2", "pins")},
			origin:   text("1", "test"),
			expected: "2",
		},
		{
			name:     "specific pins channel",
			channels: []*discordgo.Channel{text("1", "test"), text("2", "pins"), text("3", "test-pins")},
			origin:   text("1", "test"),
			expected: "3",
		},
		{
			name:     "thread uses parent's specific pins channel",
			channels: []*discordgo.Channel{text("1", "test"), text("2", "pins"), text("3", "test-pins")},
			origin:   thread,
			expected: "3",
		},
		{
			name:     "thread uses general pins channel",
			channels: []*discordgo.Channel{text("1", "test"), text("2", "pins")},
			origin:   thread,
			expected: "2",
		},
		{
			name:     "thread falls back to itself",
			channels: []*discordgo.Channel{text("1", "test")},
			origin:   thread,
			expected: "10",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := getTargetChannel(tc.channels, tc.origin)

			require.NoError(t, err)
			assert.Equal(t, tc.expected, c.ID)
		})
	}
}

func TestGetTargetChannel_DoesNotLeakMessages(t *testing.T) {
	const guildID = "100"
	text := func(id, name string) *discordgo.Channel {
		return &discordgo.Channel{ID: id, GuildID: guildID, Name: name, Type: discordgo.ChannelTypeGuildText}
	}
	hidden := func(c *discordgo.Channel) *discordgo.Channel {
		c.PermissionOverwrites = []*discordgo.PermissionOverwrite{{
			ID:   guildID,
			Type: discordgo.PermissionOverwriteTypeRole,
			Deny: discordgo.PermissionViewChannel,
		}}
		return c
	}
	nsfw := func(c *discordgo.Channel) *discordgo.Channel {
		c.NSFW = true
		return c
	}
	privateThread := &discordgo.Channel{ID: "10", GuildID: guildID, ParentID: "1", Type: discordgo.ChannelTypeGuildPrivateThread}

	testCases := []struct {
		name     string
		channels []*discordgo.Channel
		origin   *discordgo.Channel
		expected string
	}{
		{
			name:     "hidden channel does not pin to visible pins channel",
			channels: []*discordgo.Channel{hidden(text("1", "test")), text("2", "pins")},
			origin:   hidden(text("1", "test")),
			expected: "1",
		},
		{
			name:     "hidden channel pins to hidden pins channel",
			channels: []*discordgo.Channel{hidden(text("1", "test")), text("2", "pins"), hidden(text("3", "test-pins"))},
			origin:   hidden(text("1", "test")),
			expected: "3",
		},
		{
			name:     "hidden channel skips visible specific pins channel for hidden general pins channel",
			channels: []*discordgo.Channel{hidden(text("1", "test")), hidden(text("2", "pins")), text("3", "test-pins")},
			origin:   hidden(text("1", "test")),
			expected: "2",
		},
		{
			name:     "visible channel pins to hidden pins channel",
			channels: []*discordgo.Channel{text("1", "test"), hidden(text("2", "pins"))},
			origin:   text("1", "test"),
			expected: "2",
		},
		{
			name:     "private thread does not pin to visible pins channel",
			channels: []*discordgo.Channel{text("1", "test"), text("2", "pins")},
			origin:   privateThread,
			expected: "10",
		},
		{
			name:     "thread in hidden channel does not pin to visible pins channel",
			channels: []*discordgo.Channel{hidden(text("1", "test")), text("2", "pins")},
			origin:   &discordgo.Channel{ID: "10", GuildID: guildID, ParentID: "1", Type: discordgo.ChannelTypeGuildPublicThread},
			expected: "10",
		},
		{
			name:     "nsfw channel does not pin to sfw pins channel",
			channels: []*discordgo.Channel{nsfw(text("1", "test")), text("2", "pins")},
			origin:   nsfw(text("1", "test")),
			expected: "1",
		},
		{
			name:     "nsfw channel pins to nsfw pins channel",
			channels: []*discordgo.Channel{nsfw(text("1", "test")), nsfw(text("2", "pins"))},
			origin:   nsfw(text("1", "test")),
			expected: "2",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := getTargetChannel(tc.channels, tc.origin)

			require.NoError(t, err)
			assert.Equal(t, tc.expected, c.ID)
		})
	}
}

func TestGetTargetChannel_ThreadWithUnknownParent(t *testing.T) {
	thread := &discordgo.Channel{ID: "10", ParentID: "1", Type: discordgo.ChannelTypeGuildPublicThread}

	_, err := getTargetChannel(nil, thread)

	assert.Error(t, err)
}

func TestGetSourceChannel_FetchesChannelsMissingFromList(t *testing.T) {
	thread := &discordgo.Channel{ID: "10", GuildID: "1", ParentID: "2", Type: discordgo.ChannelTypeGuildPublicThread}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/channels/10" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(thread)
	}))
	t.Cleanup(server.Close)

	endpoint := discordgo.EndpointChannels
	discordgo.EndpointChannels = server.URL + "/channels/"
	t.Cleanup(func() { discordgo.EndpointChannels = endpoint })

	s, err := discordgo.New("Bot token")
	require.NoError(t, err)

	c, err := getSourceChannel(context.Background(), s, []*discordgo.Channel{{ID: "2"}}, "10")

	require.NoError(t, err)
	assert.Equal(t, "10", c.ID)
	assert.True(t, c.IsThread())
}

func TestIsAlreadyPinned_PagesThroughReactions(t *testing.T) {
	const appID = "999"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// the first page is full of other users, and the bot is on the second page
		var users []*discordgo.User
		if r.URL.Query().Get("after") == "" {
			for i := range maxReactionsPage {
				users = append(users, &discordgo.User{ID: strconv.Itoa(i + 1)})
			}
		} else {
			users = append(users, &discordgo.User{ID: appID})
		}
		_ = json.NewEncoder(w).Encode(users)
	}))
	t.Cleanup(server.Close)

	endpoint := discordgo.EndpointChannels
	discordgo.EndpointChannels = server.URL + "/channels/"
	t.Cleanup(func() { discordgo.EndpointChannels = endpoint })

	s, err := discordgo.New("Bot token")
	require.NoError(t, err)

	i := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{AppID: appID}}
	pinned, err := isAlreadyPinned(context.Background(), s, i, testMessage())

	require.NoError(t, err)
	assert.True(t, pinned)
}

func TestBuildPinMessage_AuthorUsesDisplayName(t *testing.T) {
	source := &discordgo.Channel{ID: "2", GuildID: "1", Name: "test"}
	m := testMessage()
	m.Author.GlobalName = "Display Name"

	pin := buildPinMessage(source, m, nil)

	assert.Equal(t, "Display Name", pin.Embeds[0].Author.Name)
}
