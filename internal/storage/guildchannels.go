package storage

import (
	"sync"

	"github.com/bwmarrin/discordgo"
)

var Guilds = sync.Map{}

type GuildChannels struct {
	mx       sync.Mutex
	Channels []*discordgo.Channel
}

func (gc *GuildChannels) Add(channels ...*discordgo.Channel) (bool, error) {
	gc.mx.Lock()
	defer gc.mx.Unlock()

	for _, c := range channels {
		if c.Type != discordgo.ChannelTypeGuildText {
			return false, nil
		}

		// skip adding if this channel already exists
		for _, channel := range gc.Channels {
			if channel.ID == c.ID {
				return false, nil
			}
		}

		gc.Channels = append(gc.Channels, c)
	}

	return true, nil
}

func (gc *GuildChannels) Delete(id string) (bool, error) {
	gc.mx.Lock()
	defer gc.mx.Unlock()

	for i, channel := range gc.Channels {
		if channel.ID == id {
			gc.Channels = append(gc.Channels[:i], gc.Channels[i+1:]...)
			return true, nil
		}
	}

	return false, nil
}
