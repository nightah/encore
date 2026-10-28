package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/core/match"
)

// NoLibraryGroup reports whether the instance is forced to keep a library of its own.
func (k *KodiInstance) NoLibraryGroup() bool {
	return strings.EqualFold(k.LibraryGroup, LibraryGroupNone)
}

func validateKodi(instances []KodiInstance) []error {
	var errs []error

	names := map[string]bool{}
	defaults := 0

	for i, k := range instances {
		where := fmt.Sprintf("kodi[%d]", i)

		if k.Name == "" {
			errs = append(errs, fmt.Errorf("%s.name must not be empty", where))
		} else if names[k.Name] {
			errs = append(errs, fmt.Errorf("%s.name %q is used twice", where, k.Name))
		}

		names[k.Name] = true

		if u, err := url.Parse(k.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, fmt.Errorf("%s.url %q must be an http(s) URL such as http://kodi:8080/jsonrpc", where, kodi.RedactURL(k.URL)))
		}

		if k.WebSocketURL != "" {
			if u, err := url.Parse(k.WebSocketURL); err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" {
				errs = append(errs, fmt.Errorf("%s.websocket_url %q must be a ws(s) URL such as ws://kodi:9090/jsonrpc", where, kodi.RedactURL(k.WebSocketURL)))
			}
		}

		if k.MAC != "" {
			if _, err := net.ParseMAC(k.MAC); err != nil {
				errs = append(errs, fmt.Errorf("%s.mac %q is not a MAC address", where, k.MAC))
			}
		}

		if k.Timezone != "" {
			if _, err := time.LoadLocation(k.Timezone); err != nil {
				errs = append(errs, fmt.Errorf("%s.timezone %q is not a known IANA time zone", where, k.Timezone))
			}
		}

		if k.Language != "" && !slices.Contains(match.Languages(), k.Language) {
			errs = append(errs, fmt.Errorf("%s.language %q must be one of %s", where, k.Language, strings.Join(match.Languages(), ", ")))
		}

		if k.LibraryGroup != strings.TrimSpace(k.LibraryGroup) {
			errs = append(errs, fmt.Errorf("%s.library_group %q must not start or end with spaces", where, k.LibraryGroup))
		}

		if k.Default {
			defaults++
		}
	}

	if defaults > 1 {
		errs = append(errs, errors.New("only one kodi instance may set default: true"))
	}

	return errs
}

func (c *Config) validateRoomsAndDevices() []error {
	var errs []error

	instances := map[string]bool{}
	for _, k := range c.Kodi {
		instances[k.Name] = true
	}

	rooms := map[string]bool{}

	for i, r := range c.Rooms {
		if r.Name == "" {
			errs = append(errs, fmt.Errorf("rooms[%d].name must not be empty", i))
		}

		if rooms[r.Name] {
			errs = append(errs, fmt.Errorf("rooms[%d].name %q is used twice", i, r.Name))
		}

		rooms[r.Name] = true

		if r.Kodi != "" && !instances[r.Kodi] {
			errs = append(errs, fmt.Errorf("rooms[%d].kodi %q is not a Kodi instance in the kodi list", i, r.Kodi))
		}
	}

	devices := map[string]bool{}

	for i, d := range c.Devices {
		if d.ID == "" {
			errs = append(errs, fmt.Errorf("devices[%d].id must be the Alexa device ID", i))
		}

		if devices[d.ID] {
			errs = append(errs, fmt.Errorf("devices[%d].id is used twice", i))
		}

		devices[d.ID] = true

		if d.Kodi != "" && !instances[d.Kodi] {
			errs = append(errs, fmt.Errorf("devices[%d].kodi %q is not a Kodi instance in the kodi list", i, d.Kodi))
		}

		if d.Room != "" && !rooms[d.Room] {
			errs = append(errs, fmt.Errorf("devices[%d].room %q is not a room in the rooms list", i, d.Room))
		}
	}

	return errs
}
