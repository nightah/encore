package fakekodi

import "time"

// AustralianTV is live TV as a Melbourne Kodi instance with two IPTV sources has it, numbered as Kodi numbers channels itself, with the guide relative to now. It has the awkward cases: names that are numbers or hold them, a name two channels share, a number two sources both use, channels without a guide, a sub-channel, and hidden and locked channels.
func AustralianTV(now time.Time) *PVR {
	slot := func(id int, title, episode, genre string, from, to time.Duration) *Broadcast {
		return &Broadcast{
			BroadcastID: id, Title: title, EpisodeName: episode, Genre: []string{genre},
			StartTime: now.Add(from).UTC().Format(TimeLayout), EndTime: now.Add(to).UTC().Format(TimeLayout),
		}
	}
	now30 := func(id int, title, genre string) *Broadcast {
		return slot(id, title, "", genre, -10*time.Minute, 20*time.Minute)
	}
	next := func(id int, title, genre string) *Broadcast {
		return slot(id, title, "", genre, 20*time.Minute, 80*time.Minute)
	}

	return &PVR{Channels: []Channel{
		{
			ChannelID: 3072, Channel: "Seven", ChannelNumber: 1, ClientID: 5,
			Now:  slot(748079, "House Of Wellness", "Episode 23 of Season 11", "Special Interest", -10*time.Minute, 20*time.Minute),
			Next: next(748080, "The Amazing Race", "Special Interest"),
		},
		{ChannelID: 3099, Channel: "7two", ChannelNumber: 2, ClientID: 5, Now: now30(748101, "Foyle's War", "Drama")},
		{ChannelID: 3137, Channel: "7mate", ChannelNumber: 3, ClientID: 5, Now: now30(748102, "The Force - Behind The Line", "Documentary")},
		{ChannelID: 2999, Channel: "Channel 9", ChannelNumber: 5, ClientID: 5, Now: now30(748103, "Taronga: Who's Who In The Zoo", "Documentary")},
		{ChannelID: 3079, Channel: "9Go!", ChannelNumber: 7, ClientID: 5, Now: now30(748104, "Seinfeld", "Comedy")},
		{ChannelID: 3140, Channel: "ABC TV", ChannelNumber: 10, ClientID: 5, Now: now30(748105, "Austin", "Comedy"), Next: next(748106, "Gardening Australia", "Lifestyle")},
		{ChannelID: 3004, Channel: "10", ChannelNumber: 11, ClientID: 5, Now: now30(748107, "Boston Blue", "Drama")},
		{ChannelID: 3104, Channel: "10 Drama", ChannelNumber: 12, ClientID: 5, Now: now30(748108, "NCIS", "Drama")},
		{ChannelID: 4030, Channel: "SBS HD", ChannelNumber: 30, SubChannelNumber: 1, ClientID: 5},
		{ChannelID: 3195, Channel: "Seinfeld", ChannelNumber: 64, ClientID: 5},
		{ChannelID: 3190, Channel: "BBC News", ChannelNumber: 73, ClientID: 5},
		{ChannelID: 3080, Channel: "SBS", ChannelNumber: 135, ClientID: 5, Now: now30(748109, "2.6 Seconds: Death in the Outback", "Documentary")},
		{ChannelID: 3077, Channel: "SBS Radio 1", ChannelNumber: 151, ClientID: 5, Now: now30(748110, "Korean (RPT)", "Radio")},
		{ChannelID: 3096, Channel: "ABC NEWS", ChannelNumber: 158, ClientID: 5, Now: now30(748111, "The World", "News"), Next: next(748112, "Planet America", "News")},
		{ChannelID: 3005, Channel: "BBC News", ChannelNumber: 169, ClientID: 5, Now: now30(748113, "BBC News", "News")},
		{ChannelID: 3159, Channel: "DW English", ChannelNumber: 173, ClientID: 5},
		{ChannelID: 3164, Channel: "Fox Sports News", ChannelNumber: 173, ClientID: 6, Now: now30(748114, "FOX SPORTS News", "Sport")},
		{ChannelID: 3170, Channel: "Fox Sports 503", ChannelNumber: 176, ClientID: 6, Now: now30(748115, "Test Cricket", "Sport"), Next: next(748116, "Big Bash League", "Sport")},
		{ChannelID: 3998, Channel: "Late Night Movies", ChannelNumber: 990, ClientID: 5, Locked: true},
		{ChannelID: 3999, Channel: "Test Card", ChannelNumber: 999, ClientID: 5, Hidden: true},
	}}
}
