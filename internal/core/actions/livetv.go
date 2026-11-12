package actions

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/nightah/encore/internal/core/livetv"
	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// WatchChannel tunes to the channel spoken names, asking which was meant when several might be, and which channel when Alexa heard none.
func (s *Service) WatchChannel(ctx context.Context, t Target, spoken string, progress Progress) Outcome {
	var found livetv.Found

	// Alexa sends "?" for a number it couldn't read.
	none := !strings.ContainsFunc(spoken, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) })

	if o, ok := s.withGuide(ctx, t, progress, func() (err error) {
		if !none {
			found, err = t.Guide.Find(ctx, spoken)
		}

		return err
	}); !ok {
		return o
	}

	if none {
		msg := say("livetv.which_channel_to_watch", nil)

		return Outcome{Say: msg, Reprompt: &msg}
	}

	switch found.Decision {
	case match.DecisionPlay:
		o := s.tune(ctx, t, found.Channel, progress)
		o.Match = found.Result

		return o
	case match.DecisionAsk:
		o := offer(channelChoices(found.Choices))
		o.Match = found.Result

		return o
	}

	o := done(say("livetv.channel_not_found", map[string]string{"channel": spoken}))
	o.Match = found.Result

	return o
}

// TuneIfChannel tunes to the channel spoken names when it is plainly one, for a request that found nothing in the library: "put on ABC News". It never asks, so a library question isn't traded for a channel one, and reports false when it didn't tune.
func (s *Service) TuneIfChannel(ctx context.Context, t Target, spoken string, progress Progress) (Outcome, bool) {
	if t.Guide == nil || !t.Allows(media.KindChannel) {
		return Outcome{}, false
	}

	if ok, err := t.Guide.Available(ctx); err != nil || !ok {
		return Outcome{}, false
	}

	found, err := t.Guide.Find(ctx, spoken)
	if err != nil || found.Decision != match.DecisionPlay {
		return Outcome{}, false
	}

	o := s.tune(ctx, t, found.Channel, progress)
	o.Match = found.Result

	return o, true
}

// WatchChannelNumber tunes to a channel by number; sub is the number after the point, -1 when none was said.
func (s *Service) WatchChannelNumber(ctx context.Context, t Target, major, sub int, progress Progress) Outcome {
	var (
		c  media.Channel
		ok bool
	)

	if o, fine := s.withGuide(ctx, t, progress, func() (err error) {
		c, ok, err = t.Guide.ByNumber(ctx, major, sub)

		return err
	}); !fine {
		return o
	}

	if !ok {
		n := strconv.Itoa(major)
		if sub >= 0 {
			n += "." + strconv.Itoa(sub)
		}

		return done(say("livetv.number_not_found", map[string]string{"number": n}))
	}

	return s.tune(ctx, t, c, progress)
}

// OpenTVGuide opens the TV guide or the channel list.
func (s *Service) OpenTVGuide(ctx context.Context, t Target, section media.Section, progress Progress) Outcome {
	if o, ok := s.withGuide(ctx, t, progress, func() error { return nil }); !ok {
		return o
	}

	if err := withWake(ctx, t, progress, func() error { return t.Backend.ShowView(ctx, media.View{Section: section}) }); err != nil {
		return failure(t, err)
	}

	return silent()
}

// WhatsOn says what's on a channel now and next. With no channel named, it is the channel playing.
func (s *Service) WhatsOn(ctx context.Context, t Target, spoken string, progress Progress) Outcome {
	var id int

	if o, ok := s.withGuide(ctx, t, progress, func() error {
		var err error

		id, err = s.channelMeant(ctx, t, spoken)

		return err
	}); !ok {
		return o
	}

	if id == 0 {
		if spoken != "" {
			return done(say("livetv.channel_not_found", map[string]string{"channel": spoken}))
		}

		msg := say("livetv.which_channel", nil)

		return Outcome{Say: msg, Reprompt: &msg}
	}

	c, ok, err := t.Guide.ChannelNow(ctx, id)
	if err != nil {
		return failure(t, err)
	}

	if !ok || c.Now == nil {
		name := c.Name
		if name == "" {
			name = spoken
		}

		return done(say("livetv.no_guide", map[string]string{"channel": name}))
	}

	msg := say("livetv.now", map[string]string{"channel": c.Name, "title": c.Now.Title})
	if c.Next != nil {
		msg = say("livetv.now_next", map[string]string{"channel": c.Name, "title": c.Now.Title, "next": c.Next.Title})
	}

	msg.Times = map[string]time.Time{"end": c.Now.End}

	return done(msg)
}

// channelMeant is the ID of the channel spoken names, or of the channel playing when none is named; 0 when there is none. A name that might be several channels is taken as the likeliest: only what's on is asked.
func (s *Service) channelMeant(ctx context.Context, t Target, spoken string) (int, error) {
	if spoken == "" {
		np, err := t.Backend.NowPlaying(ctx)
		if err != nil || np == nil || np.Item.Kind != media.KindChannel {
			return 0, err
		}

		return np.Item.ID, nil
	}

	found, err := t.Guide.Find(ctx, spoken)
	if err != nil {
		return 0, err
	}

	switch {
	case found.Decision == match.DecisionPlay:
		return found.Channel.ID, nil
	case found.Decision == match.DecisionAsk && len(found.Choices) > 0:
		return found.Choices[0].ID, nil
	}

	return 0, nil
}

// WatchLive tunes to the channel showing a programme now, or says when it starts when it is on next. A name that is no programme's may be a channel's.
func (s *Service) WatchLive(ctx context.Context, t Target, spoken string, progress Progress) Outcome {
	var (
		p  livetv.Programme
		ok bool
	)

	if o, fine := s.withGuide(ctx, t, progress, func() (err error) {
		p, ok, err = t.Guide.FindProgramme(ctx, spoken)

		return err
	}); !fine {
		return o
	}

	switch {
	case ok && p.OnNow:
		o := s.tune(ctx, t, p.Channel, progress)
		if o.Played != nil {
			o.Say = say("livetv.switching_programme", map[string]string{"channel": p.Channel.Name, "title": p.Broadcast.Title})
		}

		return o
	case ok:
		msg := say("livetv.programme_later", map[string]string{"channel": p.Channel.Name, "title": p.Broadcast.Title})
		msg.Times = map[string]time.Time{"start": p.Broadcast.Start}

		return done(msg)
	}

	if o, tuned := s.TuneIfChannel(ctx, t, spoken, progress); tuned {
		return o
	}

	return done(say("livetv.programme_not_found", map[string]string{"title": spoken}))
}

// withGuide checks the target has live TV and runs fn, waking the media centre if it is asleep. It returns the outcome to give, and false, when the request can't go on.
func (s *Service) withGuide(ctx context.Context, t Target, progress Progress, fn func() error) (Outcome, bool) {
	unavailable := done(say("livetv.unavailable", map[string]string{"kodi": t.Name}))

	if t.Guide == nil {
		return unavailable, false
	}

	var live bool

	err := withWake(ctx, t, progress, func() error {
		var err error

		live, err = t.Guide.Available(ctx)

		return err
	})

	switch {
	case err != nil && !livetv.IsNoLiveTV(err):
		return failure(t, err), false
	case err != nil || !live:
		return unavailable, false
	}

	if err := fn(); err != nil {
		if livetv.IsNoLiveTV(err) {
			return unavailable, false
		}

		return failure(t, err), false
	}

	return Outcome{}, true
}

// tune switches to a channel and says so.
func (s *Service) tune(ctx context.Context, t Target, c media.Channel, progress Progress) Outcome {
	if c.Locked {
		return done(say("livetv.locked", map[string]string{"channel": c.Name}))
	}

	err := withWake(ctx, t, progress, func() error { return t.Backend.PlayChannel(ctx, c.ID) })

	switch {
	case errors.Is(err, media.ErrNotFound):
		return done(say("livetv.channel_not_found", map[string]string{"channel": c.Name}))
	case err != nil:
		return failure(t, err)
	}

	o := done(say("livetv.switching", map[string]string{"channel": c.Name}))
	o.Played = &media.Item{Kind: media.KindChannel, ID: c.ID, Title: c.Name}

	return o
}

func channelChoices(channels []media.Channel) []Choice {
	out := make([]Choice, len(channels))
	for i, c := range channels {
		out[i] = Choice{DocID: "channel/" + strconv.Itoa(c.ID), Ref: media.Ref{Kind: media.KindChannel, ID: c.ID}, Title: c.Name}
	}

	return out
}
