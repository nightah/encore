package kodirpc

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// Rewrites testdata/name with -update.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s mismatch\ngot:\n%s\nwant:\n%s", name, got, want)
	}
}

func marshalIndent(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

func TestGetMoviesResultRoundTrip(t *testing.T) {
	sample, err := os.ReadFile(filepath.Join("testdata", "getmovies.json"))
	if err != nil {
		t.Fatal(err)
	}

	var res VideoLibraryGetMoviesResult
	if err := json.Unmarshal(sample, &res); err != nil {
		t.Fatal(err)
	}

	if res.Limits.Total != 2 || len(res.Movies) != 2 {
		t.Fatalf("limits/movies = %+v / %d", res.Limits, len(res.Movies))
	}
	m := res.Movies[0]
	// Fields promoted through the embedded Video.Details.* hierarchy.
	testCases := []struct {
		name     string
		actual   any
		expected any
	}{
		{name: "ShouldDecodeMovieID", actual: m.MovieID, expected: LibraryID(12)},
		{name: "ShouldDecodeLabel", actual: m.Label, expected: "Blade Runner 2049"},
		{name: "ShouldDecodeTitle", actual: m.Title, expected: "Blade Runner 2049"},
		{name: "ShouldDecodeYear", actual: m.Year, expected: 2017},
		{name: "ShouldDecodeRuntime", actual: m.Runtime, expected: 9840},
		{name: "ShouldDecodePlaycount", actual: m.Playcount, expected: 1},
		{name: "ShouldDecodePoster", actual: m.Art.Poster, expected: GlobalStringNotEmpty("image://%2fmedia%2fmovies%2fBlade%20Runner%202049%20(2017)%2fposter.jpg/")},
		{name: "ShouldDecodeResume", actual: m.Resume.Position, expected: 3605.5},
		{name: "ShouldDecodeRating", actual: m.Ratings["themoviedb"].Rating, expected: 7.5},
		{name: "ShouldDecodeIMDbID", actual: m.UniqueID["imdb"], expected: GlobalStringNotEmpty("tt1856101")},
		{name: "ShouldDecodeCast", actual: m.Cast[1].Role, expected: "Rick Deckard"},
		{name: "ShouldDecodeCodec", actual: m.Streamdetails.Video[0].Codec, expected: "hevc"},
		{name: "ShouldDecodeGenre", actual: []string(m.Genre), expected: []string{"Science Fiction", "Drama"}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if !reflect.DeepEqual(tc.actual, tc.expected) {
				t.Errorf("got %#v, want %#v", tc.actual, tc.expected)
			}
		})
	}
	if res.Movies[1].Resume != nil || res.Movies[1].Art != nil {
		t.Error("absent optional structs must decode as nil")
	}

	out := marshalIndent(t, res)
	golden(t, "getmovies.golden.json", out)

	var again VideoLibraryGetMoviesResult
	if err := json.Unmarshal(out, &again); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, res) {
		t.Errorf("typed round trip changed the value:\n%+v\n%+v", again, res)
	}
}

func TestPlayerOpenParams(t *testing.T) {
	testCases := []struct {
		name     string
		params   PlayerOpenParams
		expected string
	}{
		{
			name: "ShouldMarshalMovieWithResume",
			params: PlayerOpenParams{
				Item: NewPlayerOpenItemFromPlaylistItem(
					NewPlaylistItemFromMovieID(PlaylistItemMovieID{MovieID: 12}),
				),
				Options: &PlayerOpenOptions{Resume: NewPlayerOpenOptionsResumeFromBool(true)},
			},
			expected: `{"item":{"movieid":12},"options":{"resume":true}}`,
		},
		{
			name: "ShouldMarshalPlaylistPositionResumingAtATime",
			params: PlayerOpenParams{
				Item: NewPlayerOpenItemFromPlaylistID(PlayerOpenItemPlaylistID{PlaylistID: 1, Position: 3}),
				Options: &PlayerOpenOptions{
					Resume: NewPlayerOpenOptionsResumeFromPlayerPositionTime(
						PlayerPositionTime{Hours: 1, Minutes: 2, Seconds: 3},
					),
				},
			},
			expected: `{"item":{"playlistid":1,"position":3},"options":{"resume":{"hours":1,"minutes":2,"seconds":3}}}`,
		},
		{name: "ShouldMarshalNoItem", expected: `{}`},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.params)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.expected {
				t.Errorf("marshal = %s, want %s", got, tc.expected)
			}
		})
	}

	// Decoding keeps the raw union so the receiver picks the alternative.
	var p PlayerOpenParams
	if err := json.Unmarshal([]byte(testCases[0].expected), &p); err != nil {
		t.Fatal(err)
	}
	item, err := p.Item.AsPlaylistItem()
	if err != nil {
		t.Fatal(err)
	}
	movie, err := item.AsMovieID()
	if err != nil || movie.MovieID != 12 {
		t.Errorf("AsMovieID = %+v, %v", movie, err)
	}
	resume, err := p.Options.Resume.AsBool()
	if err != nil || !resume {
		t.Errorf("AsBool = %v, %v", resume, err)
	}
	if again, _ := json.Marshal(p); string(again) != testCases[0].expected {
		t.Errorf("re-marshal = %s, want %s", again, testCases[0].expected)
	}
}

type recordingCaller struct {
	method string
	params []byte
	result string
}

func (c *recordingCaller) Call(_ context.Context, method string, params, result any) error {
	c.method = method
	var err error
	if c.params, err = json.Marshal(params); err != nil {
		return err
	}
	return json.Unmarshal([]byte(c.result), result)
}

func TestClientWrappers(t *testing.T) {
	rc := &recordingCaller{result: `{"limits":{"start":0,"end":1,"total":1},"movies":[{"label":"Arrival","movieid":7}]}`}
	c := Client{rc}

	res, err := c.VideoLibraryGetMovies(context.Background(), VideoLibraryGetMoviesParams{
		Properties: VideoFieldsMovie{VideoFieldsMovieItemTitle, VideoFieldsMovieItemYear},
		Limits:     &ListLimits{End: 1},
		Sort:       &ListSort{Method: ListSortMethodTitle, Order: ListSortOrderAscending},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rc.method != MethodVideoLibraryGetMovies {
		t.Errorf("method = %q", rc.method)
	}
	wantParams := `{"properties":["title","year"],"limits":{"end":1},"sort":{"method":"title","order":"ascending"}}`
	if string(rc.params) != wantParams {
		t.Errorf("params = %s, want %s", rc.params, wantParams)
	}
	if len(res.Movies) != 1 || res.Movies[0].MovieID != 7 {
		t.Errorf("result = %+v", res)
	}

	rc.result = `"pong"`
	pong, err := c.JSONRPCPing(context.Background())
	if err != nil || pong != "pong" || string(rc.params) != `{}` {
		t.Errorf("JSONRPCPing = %q, %v (params %s)", pong, err, rc.params)
	}
}

func TestAvailability(t *testing.T) {
	omega := BaselineVersion
	chapters := Methods[MethodPlayerGetChapters]
	if chapters.AvailableIn(omega) || !chapters.AvailableIn(SchemaVersion) {
		t.Errorf("Player.GetChapters availability wrong: since %v", chapters.Since)
	}
	if !Methods[MethodPlayerOpen].AvailableIn(omega) {
		t.Error("Player.Open must be available in the baseline")
	}
	setShow := Methods[MethodVideoLibrarySetTVShowDetails]
	if setShow.ParamAvailableIn("trailer", omega) || !setShow.ParamAvailableIn("title", omega) {
		t.Errorf("ParamsSince not honoured: %+v", setShow.ParamsSince)
	}
}

func TestNotificationPayload(t *testing.T) {
	raw := `{"sender":"xbmc","data":{"item":{"id":12,"type":"movie"},"player":{"playerid":1,"speed":1}}}`
	v, ok := NewNotificationPayload(NotificationPlayerOnPlay)
	if !ok {
		t.Fatal("no payload for Player.OnPlay")
	}
	if err := json.Unmarshal([]byte(raw), v); err != nil {
		t.Fatal(err)
	}
	n := v.(*PlayerOnPlayNotification)
	if n.Sender != "xbmc" || n.Data.Player.PlayerID != 1 {
		t.Errorf("payload = %+v", n)
	}
}
