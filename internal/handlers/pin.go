package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"
	"golang.org/x/sync/errgroup"
)

const (
	emojiPinned     = "📌"
	pinMessageColor = 0xbb0303

	// maxEmbeds is the maximum number of embeds Discord accepts in a single message
	maxEmbeds = 10
)

func PinMessageCommandHandler(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) (err error) {
	m := data.Resolved.Messages[data.TargetID]
	m.GuildID = i.GuildID // guildID is missing from message in resolved context

	log := slog.With("guild_id", i.GuildID, "channel_id", i.ChannelID, "message_id", m.ID)

	log.Debug("Starting pin message")

	// API operations are slow, so fanout and execute concurrently
	var pinned bool
	var channels []*discordgo.Channel

	group := errgroup.Group{}
	group.Go(func() error {
		var err error
		pinned, err = isAlreadyPinned(ctx, s, i, m)
		if err != nil {
			log.Error("Could not check if message is already pinned", "error", err)
		}
		return err
	})
	group.Go(func() error {
		var err error
		channels, err = s.GuildChannels(i.GuildID, discordgo.WithContext(ctx))
		if err != nil {
			log.Error("Could not get guild channels", "error", err)
		}
		return err
	})

	if err := group.Wait(); err != nil {
		return respond(ctx, s, i.Interaction, "💩 Temporary error, please retry")
	}

	if pinned {
		return respond(ctx, s, i.Interaction, "🔄 Message already pinned")
	}

	sourceChannel, err := getSourceChannel(ctx, s, channels, m.ChannelID)
	if err != nil {
		log.Error("Could not determine source channel", "error", err)
		return respond(ctx, s, i.Interaction, "💩 Temporary error, please retry")
	}

	// determine the target pin channel for the message
	targetChannel, err := getTargetChannel(channels, sourceChannel)
	if err != nil {
		log.Error("Could not determine target channel", "error", err)
		return respond(ctx, s, i.Interaction, "💩 Temporary error, please retry")
	}
	log = log.With("target_channel_id", targetChannel.ID)

	// build the rich embed pin message
	pinMessage := buildPinMessage(sourceChannel, m, i.Member.User)

	// send the pin message
	log.Debug("Sending pin message")
	pin, err := s.ChannelMessageSendComplex(targetChannel.ID, pinMessage, discordgo.WithContext(ctx))
	if err != nil {
		log.Error("Could not send pin message", "error", err)

		var restErr *discordgo.RESTError
		if errors.As(err, &restErr) && restErr.Response != nil && restErr.Response.StatusCode == http.StatusForbidden {
			return respond(ctx, s, i.Interaction, "🙅 Could not send pin message. Please ensure bot has permission to post in "+targetChannel.Mention())
		}

		return respond(ctx, s, i.Interaction, "💩 Could not send pin message")
	}

	// mark the message as done
	if err := s.MessageReactionAdd(m.ChannelID, m.ID, emojiPinned, discordgo.WithContext(ctx)); err != nil {
		log.Error("Could not react to message", "error", err)
	}

	log.Info("Pinned message", "pin_message_id", pin.ID)

	return respond(ctx, s, i.Interaction, "📌 Pinned: "+url(i.GuildID, pin.ChannelID, pin.ID))
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

func url(guildID, channelID, messageID string) string {
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

	u := url(sourceChannel.GuildID, m.ChannelID, m.ID)
	embed := &discordgo.MessageEmbed{
		Author: &discordgo.MessageEmbedAuthor{
			Name:    m.Author.Username,
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

	// Discord rejects messages with too many embeds, so drop any overflow
	if len(pinMessage.Embeds) > maxEmbeds {
		pinMessage.Embeds = pinMessage.Embeds[:maxEmbeds]
	}

	return pinMessage
}

func isAlreadyPinned(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, m *discordgo.Message) (bool, error) {
	acks, err := s.MessageReactions(m.ChannelID, m.ID, emojiPinned, 0, "", "", discordgo.WithContext(ctx))
	if err != nil {
		return false, err
	}

	for _, ack := range acks {
		if ack.ID == i.AppID {
			return true, nil
		}
	}

	return false, nil
}

// getTargetChannel returns the target pin channel for a given channel #channel in the following order:
// #channel-pins (a specific pin channel)
// #pins (a generic pin channel)
// #channel (the channel itself)
// For threads, #channel is the thread's parent channel. The fallback is still the thread itself, as some parent
// channels (e.g. forums) cannot be posted in directly.
func getTargetChannel(channels []*discordgo.Channel, origin *discordgo.Channel) (*discordgo.Channel, error) {
	name := origin.Name
	if origin.IsThread() {
		parent := findChannel(channels, origin.ParentID)
		if parent == nil {
			return nil, fmt.Errorf("could not find parent channel with id %s", origin.ParentID)
		}
		name = parent.Name
	}

	// check for #channel-pins first
	for _, c := range channels {
		if c.Name == name+"-pins" && c.Type == discordgo.ChannelTypeGuildText {
			return c, nil
		}
	}

	// fallback to general pins channel
	for _, c := range channels {
		if c.Name == "pins" && c.Type == discordgo.ChannelTypeGuildText {
			return c, nil
		}
	}

	// use the same channel by default
	return origin, nil
}
