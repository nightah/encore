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

	testCases := []struct {
		name   string
		intent string
		have   string
	}{
		{name: "ShouldViewLibraryWithInfinitive", intent: "ViewLibrary", have: "mostrar os {Section}"},
		{name: "ShouldViewLibraryWithImperative", intent: "ViewLibrary", have: "mostre os {Section}"},
		{name: "ShouldViewPlaylistWithInfinitive", intent: "ViewPlaylist", have: "abrir a playlist {Playlist}"},
		{name: "ShouldRecommendWithInfinitive", intent: "Recommend", have: "me recomendar um {MediaType}"},
		{name: "ShouldShuffleShowWithInfinitive", intent: "ShuffleShow", have: "embaralhar a série {Show}"},
		{name: "ShouldShuffleAlbumWithInfinitive", intent: "ShuffleAlbum", have: "misturar o álbum {Album}"},
		{name: "ShouldShufflePlaylistWithInfinitive", intent: "ShufflePlaylist", have: "embaralhar a playlist {Playlist}"},
		{name: "ShouldSeekWithPular", intent: "PlayerSeek", have: "pular {Duration}"},
		{name: "ShouldSeekWithAvancar", intent: "PlayerSeek", have: "avançar {Duration}"},
		{name: "ShouldMuteWithMutar", intent: "Mute", have: "mutar"},
		{name: "ShouldMuteWithSilenciar", intent: "Mute", have: "silenciar"},
		{name: "ShouldIdentifyEchoWithInfinitive", intent: "IdentifyEcho", have: "identificar este echo"},
		{name: "ShouldListAlbumsWithInfinitive", intent: "WhatAlbums", have: "listar os álbuns de {Artist}"},
		{name: "ShouldWatchMovieTrailerWithInfinitive", intent: "WatchMovieTrailer", have: "mostrar o trailer de {Movie}"},
		{name: "ShouldTypeTextWithInfinitive", intent: "TypeText", have: "digitar {Text}"},
		{name: "ShouldEnablePartyModeWithInfinitive", intent: "PartyMode", have: "ativar o modo festa"},
		{name: "ShouldEnableStereoscopicModeWithInfinitive", intent: "StereoscopicMode", have: "ligar o modo estereoscópico"},
		{name: "ShouldPlayMediaWithToque", intent: "PlayMedia", have: "toque {Query}"},
		{name: "ShouldPlayMediaWithColoque", intent: "PlayMedia", have: "coloque {Query}"},
		{name: "ShouldWatchMovieWithColoque", intent: "WatchMovie", have: "coloque o filme {Movie}"},
		{name: "ShouldWatchEpisodeWithPonha", intent: "WatchEpisode", have: "ponha a temporada {Season} episódio {Episode} de {Show}"},
		{name: "ShouldWatchNextEpisodeWithReproduza", intent: "WatchNextEpisode", have: "reproduza o próximo episódio de {Show}"},
		{name: "ShouldListenToSongWithToque", intent: "ListenToSong", have: "toque a música {Song}"},
		{name: "ShouldPlayPlaylistWithBote", intent: "PlayPlaylist", have: "bote a playlist {Playlist}"},
		{name: "ShouldSwitchAudioStreamWithAudio", intent: "AudioStream", have: "áudio {Step}"},
		{name: "ShouldSwitchAudioStreamWithFaixaDeAudio", intent: "AudioStream", have: "faixa de áudio {Step}"},
		{name: "ShouldSwitchAudioStreamWithIdioma", intent: "AudioStream", have: "idioma {Step}"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.True(t, slices.Contains(samples[tc.intent], tc.have))
		})
	}
}
