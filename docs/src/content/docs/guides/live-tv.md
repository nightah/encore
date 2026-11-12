---
title: Live TV
description: Watching Kodi's TV channels by voice, finding what's on, and getting channel numbers that match the ones on the remote.
---

When Kodi has a PVR client, such as IPTV Simple Client fed by IPTV Merge, Encore can put its TV channels on by voice: "Alexa, ask Encore to switch to ABC News". It can also open the guide, say what's on, and put a programme on wherever it's showing.

## Turning it on

Live TV is a content group of its own, off until you turn it on. In **Alexa skill → What it can play**, tick **Live TV**, or set it as code with [`alexa.content`](/encore/reference/configuration/#alexacontent), e.g. `content: [movies, tv, music, livetv]`. The next deploy adds the live TV voice commands to the skill, and gives Alexa the names of the channels of every Kodi instance with live TV, so it hears them better.

Kodi needs a PVR client add-on that is set up and has finished loading its channels. A Kodi instance without one answers that live TV isn't set up.

## Saying it

- "switch to ABC News", "change the channel to Seven", "put on channel Nine Go", "tune to SBS": a channel by its name.
- "switch to channel number 24", "go to channel number 7 point 2": a channel by its number.
- "open the TV guide", "show the channel list", "what's on TV": Kodi's guide or channel list.
- "what's on ABC TV": what's on a channel now and next. Asked without a channel while you're watching TV, it's the channel you're watching.
- "watch the cricket live": the channel showing a programme now. A programme that starts next is answered with when it starts and where, and nothing changes.
- "put on ABC News": "play" and "put on" try your library first, then a channel, so "put on Seinfeld" still plays the show when the library has it.

Each language says it its own way; see the [voice commands](/encore/voice-commands/) for yours. Australian English also takes "chuck on channel Seven".

## How a channel is found

Channel names are matched like titles (see [matching](/encore/guides/matching/)), so numbers are read either way and near misses still find the channel: "nine go" finds *9Go!*, "seven two" *7two*, "ten drama" *10 Drama* and "racing dot com" *Racing.com*.

**Names come before numbers.** "Switch to seven" is the channel named *Seven*, wherever it is numbered. A number is used only when you say "channel number", or when no channel has the name you said, nor "Channel" and it: "nine" finds *Channel 9* before the channel numbered 9.

**The same channel every time.** When channels share a name or a number, as when two IPTV sources both have *BBC News*, Encore picks the lowest numbered. Among channels with the same number it picks the one the guide has something on now for, then the one Kodi added first. When a name could be several channels ("the ABC"), Encore asks which you meant.

Hidden channels are left out, and a locked channel is named but not tuned, since Kodi would ask for its PIN on the TV.

## Getting real channel numbers

Encore uses the numbers Kodi shows. Kodi numbers channels itself, 1, 2, 3 and so on in the order its PVR clients list them, unless it is told to use the clients' own numbers. With more than one PVR client it only does so when **Settings → PVR & Live TV → Menu/OSD → Always use the channel numbers from the backend** is on (`pvrmanager.usebackendchannelnumbersalways`).

Australian free-to-air playlists, such as the ones IPTV Merge's Australian sources use, carry each channel's broadcast number as `tvg-chno`. With that setting on, and IPTV Merge left to keep the playlist's numbers rather than number the playlist from a start number of its own, a Melbourne setup gets the numbers on the remote:

| Channel | Kodi's own numbering | Broadcast number |
|---|---|---|
| ABC TV | 10 | 21 |
| ABC NEWS | 158 | 24 |
| SBS | 135 | 30 |
| Seven | 1 | 71 |
| 7two | 2 | 72 |
| Channel 9 | 5 | 91 |
| 10 | 11 | 10 |

Channels a playlist gives no number keep Kodi's, which can be a number a broadcast channel also has; the rules above still pick the same one every time, and saying the name always works.

## When it doesn't work

- **"Doesn't have live TV set up"**: Kodi has no PVR client, or it hasn't finished starting. Encore asks Kodi again within a minute.
- **A new channel isn't found**: Encore keeps each Kodi instance's channel list for 15 minutes, and fetches it again when a name matches nothing and the list is more than a minute old. The skill learns new channel names on its next deploy; until then Alexa may hear them less well.
- **What's on is out of date**: the guide is read from Kodi again within a minute, and as soon as a programme on now ends. If Kodi's guide itself is empty, check the PVR client's EPG source.
