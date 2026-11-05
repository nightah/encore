package kodi

import (
	"context"
	"strconv"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

// Screen reports the largest picture Kodi's display shows: the GUI's resolution, or a larger mode Kodi may switch to for playback (videoscreen.whitelist), as when the GUI runs at 1080p on a 4K TV.
func (b *Backend) Screen(ctx context.Context) (media.Screen, error) {
	labels, err := b.c.XBMCGetInfoLabels(ctx, kodirpc.XBMCGetInfoLabelsParams{Labels: []string{"System.ScreenWidth", "System.ScreenHeight"}})
	if err != nil {
		return media.Screen{}, err
	}

	best := media.Screen{}
	best.Width, _ = strconv.Atoi(labels["System.ScreenWidth"])
	best.Height, _ = strconv.Atoi(labels["System.ScreenHeight"])

	// Kodi before 18 and some platforms have no whitelist; the GUI's resolution stands.
	if r, err := b.c.SettingsGetSettingValue(ctx, kodirpc.SettingsGetSettingValueParams{Setting: "videoscreen.whitelist"}); err == nil {
		if modes, err := r.Value.AsSettingValueList(); err == nil {
			for _, m := range modes {
				mode, _ := m.AsString()
				if s, ok := parseMode(mode); ok && s.Width*s.Height > best.Width*best.Height {
					best = s
				}
			}
		}
	}

	return best, nil
}

// parseMode reads a videoscreen mode such as "0384002160060.00000pstd": five digits of width, five of height, then the refresh rate.
func parseMode(mode string) (media.Screen, bool) {
	if len(mode) < 10 {
		return media.Screen{}, false
	}

	w, werr := strconv.Atoi(mode[:5])
	h, herr := strconv.Atoi(mode[5:10])

	if werr != nil || herr != nil || w <= 0 || h <= 0 {
		return media.Screen{}, false
	}

	return media.Screen{Width: w, Height: h}, true
}
