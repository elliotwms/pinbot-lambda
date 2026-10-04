package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"github.com/elliotwms/bot/interactions/router"
	"golang.org/x/sync/errgroup"
)

const (
	emojiPinned     = "📌"
	pinMessageColor = 0xbb0303

	// maxEmbeds is the maximum number of embeds Discord accepts in a single message
	maxEmbeds = 10

	// maxEmbedsLength is the maximum number of characters Discord accepts across all embeds in a single message
	maxEmbedsLength = 6000

	// maxReactionsPage is the maximum number of users Discord returns per page of reactions
	maxReactionsPage = 100
)

// NewPinMessageCommandHandler returns the handler for the "Pin" message command
func NewPinMessageCommandHandler(l *slog.Logger) router.ApplicationCommandHandler {
	return func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) error {
		return pinMessage(ctx, l, s, i, data)
	}
}

func pinMessage(ctx context.Context, log *slog.Logger, s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) error {
	log = log.With("guild_id", i.GuildID, "channel_id", i.ChannelID)

	if i.GuildID == "" {
		return respond(ctx, s, i.Interaction, "🙅 Messages can only be pinned in servers")
	}

	var m *discordgo.Message
	if data.Resolved != nil {
		m = data.Resolved.Messages[data.TargetID]
	}
	if m == nil {
		log.Error("Could not find target message in interaction", "target_id", data.TargetID)
		return respond(ctx, s, i.Interaction, "💩 Could not find message to pin")
	}
	m.GuildID = i.GuildID // guildID is missing from message in resolved context

	log = log.With("message_id", m.ID)

	log.Debug("Starting pin message")

	// API operations are slow, so fanout and execute concurrently
	var pinned bool
	var channels []*discordgo.Channel

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		var err error
		pinned, err = isAlreadyPinned(groupCtx, s, i, m)
		if err != nil {
			log.Error("Could not check if message is already pinned", "error", err)
		}
		return err
	})
	group.Go(func() error {
		var err error
		channels, err = s.GuildChannels(i.GuildID, discordgo.WithContext(groupCtx))
		if err != nil {
			log.Error("Could not get guild channels", "error", err)
		}
		return err
	})

	if err := group.Wait(); err != nil {
		return respondError(ctx, s, i.Interaction, err)
	}

	if pinned {
		return respond(ctx, s, i.Interaction, "🔄 Message already pinned")
	}

	sourceChannel, err := getSourceChannel(ctx, s, channels, m.ChannelID)
	if err != nil {
		log.Error("Could not determine source channel", "error", err)
		return respondError(ctx, s, i.Interaction, err)
	}

	// determine the target pin channel for the message
	targetChannel, err := getTargetChannel(channels, sourceChannel)
	if err != nil {
		log.Error("Could not determine target channel", "error", err)
		return respond(ctx, s, i.Interaction, "💩 Temporary error, please retry")
	}
	log = log.With("target_channel_id", targetChannel.ID)

	// build the rich embed pin message
	var pinnedBy *discordgo.User
	if i.Member != nil {
		pinnedBy = i.Member.User
	}
	pin := buildPinMessage(sourceChannel, m, pinnedBy)

	// send the pin message
	log.Debug("Sending pin message")
	sent, err := s.ChannelMessageSendComplex(targetChannel.ID, pin, discordgo.WithContext(ctx))
	if err != nil {
		log.Error("Could not send pin message", "error", err)

		if isForbidden(err) {
			return respond(ctx, s, i.Interaction, "🙅 Could not send pin message. Please ensure bot has permission to post in "+targetChannel.Mention())
		}

		return respond(ctx, s, i.Interaction, "💩 Could not send pin message")
	}

	// mark the message as done
	if err := s.MessageReactionAdd(m.ChannelID, m.ID, emojiPinned, discordgo.WithContext(ctx)); err != nil {
		log.Error("Could not react to message", "error", err)
	}

	log.Info("Pinned message", "pin_message_id", sent.ID)

	return respond(ctx, s, i.Interaction, "📌 Pinned: "+messageURL(i.GuildID, sent.ChannelID, sent.ID))
}

// getSourceChannel returns the channel with the given id. Threads are not included in the guild channels list, so if
// the channel is not found there then it is fetched directly.
func getSourceChannel(ctx context.Context, s *discordgo.Session, channels []*discordgo.Channel, id string) (*discordgo.Channel, error) {
	if c := findChannel(channels, id); c != nil {
		return c, nil
	}

	return s.Channel(id, discordgo.WithContext(ctx))
}

func findChannel(channels []*discordgo.Channel, id string) *discordgo.Channel {
	for _, channel := range channels {
		if channel.ID == id {
			return channel
		}
	}

	return nil
}

func respond(ctx context.Context, s *discordgo.Session, i *discordgo.Interaction, c string) error {
	_, err := s.InteractionResponseEdit(i, &discordgo.WebhookEdit{
		Content: &c,
	}, discordgo.WithContext(ctx))

	return err
}

// respondError responds to a failed Discord API call. Permission errors won't be fixed by retrying, so they get their own
// message.
func respondError(ctx context.Context, s *discordgo.Session, i *discordgo.Interaction, err error) error {
	if isForbidden(err) {
		return respond(ctx, s, i, "🙅 Could not read this channel. Please ensure the bot has permission to view it and read its message history")
	}

	return respond(ctx, s, i, "💩 Temporary error, please retry")
}

func isForbidden(err error) bool {
	var restErr *discordgo.RESTError
	return errors.As(err, &restErr) && restErr.Response != nil && restErr.Response.StatusCode == http.StatusForbidden
}

func messageURL(guildID, channelID, messageID string) string {
	return fmt.Sprintf(
		"https://discord.com/channels/%s/%s/%s",
		guildID,
		channelID,
		messageID,
	)
}

func buildPinMessage(sourceChannel *discordgo.Channel, m *discordgo.Message, pinnedBy *discordgo.User) *discordgo.MessageSend {
	fields := []*discordgo.MessageEmbedField{
		{
			Name:   "Channel",
			Value:  sourceChannel.Mention(),
			Inline: true,
		},
	}

	u := messageURL(sourceChannel.GuildID, m.ChannelID, m.ID)
	embed := &discordgo.MessageEmbed{
		Author: &discordgo.MessageEmbedAuthor{
			Name:    m.Author.DisplayName(),
			IconURL: m.Author.AvatarURL(""),
			URL:     u,
		},
		Title:       "📌 Pinned",
		Color:       pinMessageColor,
		Description: m.Content,
		URL:         u,
		Timestamp:   m.Timestamp.Format(time.RFC3339),
	}

	if pinnedBy != nil {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Pinned by",
			Value:  pinnedBy.Mention(),
			Inline: true,
		})
	}

	embed.Fields = fields

	pinMessage := &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
	}

	// If there are multiple images then add them to separate embeds
	for _, a := range m.Attachments {
		if a.Width == 0 || a.Height == 0 {
			// only embed images
			continue
		}
		e := &discordgo.MessageEmbedImage{URL: a.URL}

		if embed.Image == nil {
			// add the first image to the existing embed
			embed.Image = e
		} else {
			// add any other images to their own embed
			pinMessage.Embeds = append(pinMessage.Embeds, &discordgo.MessageEmbed{
				Type:  discordgo.EmbedTypeImage,
				Color: pinMessageColor,
				Image: e,
			})
		}
	}

	// preserve the existing embeds
	pinMessage.Embeds = append(pinMessage.Embeds, m.Embeds...)

	// Discord rejects messages with too many embeds, or too many characters across them, so drop any overflow. The
	// first embed is always kept, as it contains the pinned message itself.
	if len(pinMessage.Embeds) > maxEmbeds {
		pinMessage.Embeds = pinMessage.Embeds[:maxEmbeds]
	}
	for len(pinMessage.Embeds) > 1 && embedsLength(pinMessage.Embeds) > maxEmbedsLength {
		pinMessage.Embeds = pinMessage.Embeds[:len(pinMessage.Embeds)-1]
	}

	return pinMessage
}

// embedsLength returns the number of characters in the embeds, as counted by Discord towards maxEmbedsLength
func embedsLength(embeds []*discordgo.MessageEmbed) int {
	n := 0
	for _, e := range embeds {
		n += utf8.RuneCountInString(e.Title) + utf8.RuneCountInString(e.Description)
		for _, f := range e.Fields {
			n += utf8.RuneCountInString(f.Name) + utf8.RuneCountInString(f.Value)
		}
		if e.Footer != nil {
			n += utf8.RuneCountInString(e.Footer.Text)
		}
		if e.Author != nil {
			n += utf8.RuneCountInString(e.Author.Name)
		}
	}

	return n
}

// isAlreadyPinned checks whether the bot has already reacted to the message, paging through all users who reacted
func isAlreadyPinned(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, m *discordgo.Message) (bool, error) {
	after := ""
	for {
		acks, err := s.MessageReactions(m.ChannelID, m.ID, emojiPinned, maxReactionsPage, "", after, discordgo.WithContext(ctx))
		if err != nil {
			return false, err
		}

		for _, ack := range acks {
			if ack.ID == i.AppID {
				return true, nil
			}
		}

		if len(acks) < maxReactionsPage {
			return false, nil
		}

		after = acks[len(acks)-1].ID
	}
}

// getTargetChannel returns the target pin channel for a given channel #channel in the following order:
// #channel-pins (a specific pin channel)
// #pins (a generic pin channel)
// #channel (the channel itself)
// For threads, #channel is the thread's parent channel. The fallback is still the thread itself, as some parent
// channels (e.g. forums) cannot be posted in directly.
// A pins channel is skipped if posting there would expose the message to people who cannot see the original.
func getTargetChannel(channels []*discordgo.Channel, origin *discordgo.Channel) (*discordgo.Channel, error) {
	parent := origin
	if origin.IsThread() {
		parent = findChannel(channels, origin.ParentID)
		if parent == nil {
			return nil, fmt.Errorf("could not find parent channel with id %s", origin.ParentID)
		}
	}

	for _, name := range []string{parent.Name + "-pins", "pins"} {
		c := findTextChannelByName(channels, name)
		if c != nil && canPinTo(origin, parent, c) {
			return c, nil
		}
	}

	// use the same channel by default
	return origin, nil
}

func findTextChannelByName(channels []*discordgo.Channel, name string) *discordgo.Channel {
	for _, c := range channels {
		if c.Name == name && c.Type == discordgo.ChannelTypeGuildText {
			return c
		}
	}

	return nil
}

// canPinTo reports whether messages from origin (with parent as its parent channel, or itself if it is not a thread)
// can be pinned to target without leaking them to a wider audience:
// messages from NSFW channels are only pinned to NSFW channels, and messages from channels hidden from @everyone are
// only pinned to channels which are also hidden from @everyone.
func canPinTo(origin, parent, target *discordgo.Channel) bool {
	if parent.NSFW && !target.NSFW {
		return false
	}

	hidden := origin.Type == discordgo.ChannelTypeGuildPrivateThread || isHiddenFromEveryone(parent)

	return !hidden || isHiddenFromEveryone(target)
}

// isHiddenFromEveryone reports whether the @everyone role (which shares the guild's ID) is denied from viewing c
func isHiddenFromEveryone(c *discordgo.Channel) bool {
	for _, o := range c.PermissionOverwrites {
		if o.Type == discordgo.PermissionOverwriteTypeRole && o.ID == c.GuildID && o.Deny&discordgo.PermissionViewChannel != 0 {
			return true
		}
	}

	return false
}
