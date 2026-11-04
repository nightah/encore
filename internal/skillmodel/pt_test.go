package skillmodel

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/i18n"
)

// TestShippedPortuguesePhrasings checks that a one-shot request ("peça ao kodi para mostrar os filmes") reaches its intent: Alexa drops "para" and hands the skill an infinitive, and "peça ao kodi que toque" a formal imperative.
func TestShippedPortuguesePhrasings(t *testing.T) {
	src, err := Load(i18n.FS)
	require.NoError(t, err)

	m, err := Build(src, "pt-BR", BuildOptions{})
	require.NoError(t, err)

	samples := map[string][]string{}
	for _, in := range m.InteractionModel.LanguageModel.Intents {
		samples[in.Name] = in.Samples
	}

	for _, tc := range []struct{ intent, sample string }{
		{"ViewLibrary", "mostrar os {Section}"},
		{"ViewLibrary", "mostre os {Section}"},
		{"ViewPlaylist", "abrir a playlist {Playlist}"},
		{"Recommend", "me recomendar um {MediaType}"},
		{"ShuffleShow", "embaralhar a série {Show}"},
		{"ShuffleAlbum", "misturar o álbum {Album}"},
		{"ShufflePlaylist", "embaralhar a playlist {Playlist}"},
		{"PlayerSeek", "pular {Duration}"},
		{"PlayerSeek", "avançar {Duration}"},
		{"Mute", "mutar"},
		{"Mute", "silenciar"},
		{"IdentifyEcho", "identificar este echo"},
		{"WhatAlbums", "listar os álbuns de {Artist}"},
		{"WatchMovieTrailer", "mostrar o trailer de {Movie}"},
		{"TypeText", "digitar {Text}"},
		{"PartyMode", "ativar o modo festa"},
		{"StereoscopicMode", "ligar o modo estereoscópico"},
		{"PlayMedia", "toque {Query}"},
		{"PlayMedia", "coloque {Query}"},
		{"WatchMovie", "coloque o filme {Movie}"},
		{"WatchEpisode", "ponha a temporada {Season} episódio {Episode} de {Show}"},
		{"WatchNextEpisode", "reproduza o próximo episódio de {Show}"},
		{"ListenToSong", "toque a música {Song}"},
		{"PlayPlaylist", "bote a playlist {Playlist}"},
		{"AudioStream", "áudio {Step}"},
		{"AudioStream", "faixa de áudio {Step}"},
		{"AudioStream", "idioma {Step}"},
	} {
		assert.True(t, slices.Contains(samples[tc.intent], tc.sample), "%s: %q", tc.intent, tc.sample)
	}
}
