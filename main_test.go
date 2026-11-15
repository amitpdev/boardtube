package main

import (
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDecodeVideos(t *testing.T) {
	input := strings.Join([]string{
		`{"id":"abc","title":"First","channel":"Channel","duration":125}`,
		`not json`,
		`{"id":"def","title":"Second","uploader":"Uploader","duration":3661}`,
	}, "\n")

	videos, err := decodeVideos(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) != 2 {
		t.Fatalf("got %d videos, want 2", len(videos))
	}
	if videos[0].URL() != "https://www.youtube.com/watch?v=abc" {
		t.Fatalf("unexpected URL: %s", videos[0].URL())
	}
	if videos[1].Channel != "Uploader" {
		t.Fatalf("got channel %q, want Uploader", videos[1].Channel)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := map[float64]string{
		0:    "--:--",
		65:   "1:05",
		3661: "1:01:01",
	}
	for seconds, want := range tests {
		if got := formatDuration(seconds); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", seconds, got, want)
		}
	}
}

func TestSearchInputAcceptsSpaces(t *testing.T) {
	m := initialModel("")
	updated, _ := m.updateSearchInput(tea.KeyMsg{
		Type:  tea.KeySpace,
		Runes: []rune{' '},
	})
	got := updated.(model)
	if got.input != " " {
		t.Fatalf("input = %q, want one space", got.input)
	}
}

func TestSlashPreservesCurrentSearchText(t *testing.T) {
	m := initialModel("boards of canada")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	got := updated.(model)

	if !got.enteringSearch {
		t.Fatal("slash did not open search input")
	}
	if got.input != "boards of canada" {
		t.Fatalf("input = %q, want current query", got.input)
	}
}

func TestSearchCursorEditing(t *testing.T) {
	m := initialModel("café music")
	m.enteringSearch = true
	steps := []struct {
		key    tea.KeyMsg
		want   string
		cursor int
	}{
		{tea.KeyMsg{Type: tea.KeyHome}, "café music", 0},
		{tea.KeyMsg{Type: tea.KeyLeft}, "café music", 0},
		{tea.KeyMsg{Type: tea.KeyBackspace}, "café music", 0},
		{tea.KeyMsg{Type: tea.KeyRight}, "café music", 1},
		{tea.KeyMsg{Type: tea.KeyDelete}, "cfé music", 1},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("afé")}, "caféfé music", 4},
		{tea.KeyMsg{Type: tea.KeyBackspace}, "caffé music", 3},
		{tea.KeyMsg{Type: tea.KeySpace}, "caf fé music", 4},
		{tea.KeyMsg{Type: tea.KeyEnd}, "caf fé music", 12},
		{tea.KeyMsg{Type: tea.KeyRight}, "caf fé music", 12},
		{tea.KeyMsg{Type: tea.KeyDelete}, "caf fé music", 12},
	}
	for i, step := range steps {
		updated, cmd := m.Update(step.key)
		m = updated.(model)
		if cmd != nil || m.input != step.want || m.inputCursor != step.cursor {
			t.Fatalf("step %d: input=%q cursor=%d cmd present=%v; want %q cursor=%d", i, m.input, m.inputCursor, cmd != nil, step.want, step.cursor)
		}
	}
}

func TestArrowSeekWithoutPlayback(t *testing.T) {
	m := initialModel("query")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	got := updated.(model)

	if cmd != nil {
		t.Fatal("right arrow returned a command without active playback")
	}
	if got.status != "Nothing is playing" {
		t.Fatalf("status = %q, want Nothing is playing", got.status)
	}
}

func TestOnlyControlCQuits(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyEsc} {
		m := initialModel("query")
		_, cmd := m.Update(tea.KeyMsg{Type: key})
		if cmd != nil {
			t.Fatalf("%v returned a quit command", key)
		}
	}

	m := initialModel("query")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd != nil {
		t.Fatal("q returned a quit command")
	}

	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("Ctrl-C did not return a quit command")
	}
}

func TestSeekDoneUpdatesDisplayedPosition(t *testing.T) {
	tests := []struct {
		name     string
		position float64
		offset   int
		want     float64
		status   string
	}{
		{"forward clamps at duration", 15, 10, 20, "Seeked forward 10 seconds"},
		{"backward clamps at zero", 5, -10, 0, "Seeked back 10 seconds"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				player:             &exec.Cmd{},
				position:           tt.position,
				duration:           20,
				playbackGeneration: 3,
			}
			updated, _ := m.Update(seekDoneMsg{generation: 3, offset: tt.offset})
			got := updated.(model)

			if got.position != tt.want {
				t.Fatalf("position = %v, want %v", got.position, tt.want)
			}
			if got.status != tt.status {
				t.Fatalf("status = %q, want %q", got.status, tt.status)
			}
		})
	}
}

func TestDiagnosticLinePrefersMissingLibrary(t *testing.T) {
	output := "dyld: Library not loaded: /missing/lib.dylib\nReason: not found\n"
	if got := diagnosticLine(output); got != "dyld: Library not loaded: /missing/lib.dylib" {
		t.Fatalf("unexpected diagnostic: %q", got)
	}
}

func TestRenderProgress(t *testing.T) {
	got := renderProgress(30, 60, 34)
	if got != "━━━━━───── 0:30 / 1:00" {
		t.Fatalf("unexpected progress bar: %q", got)
	}
}

func TestPlaylistPreservesOrderAndDuplicates(t *testing.T) {
	first := video{ID: "first", Title: "First"}
	second := video{ID: "second", Title: "Second"}
	m := model{playlistIndex: -1}

	m.addToPlaylist(first)
	m.addToPlaylist(second)
	m.addToPlaylist(first)

	want := []video{first, second, first}
	if !reflect.DeepEqual(m.playlist, want) {
		t.Fatalf("playlist = %#v, want %#v", m.playlist, want)
	}
}

func TestInsertPlaylistNext(t *testing.T) {
	videos := []video{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	inserted := video{ID: "new"}
	m := model{playlist: append([]video(nil), videos...), playlistIndex: 0}

	index := m.insertPlaylistNext(inserted)

	if index != 1 {
		t.Fatalf("insert index = %d, want 1", index)
	}
	want := []video{videos[0], inserted, videos[1], videos[2]}
	if !reflect.DeepEqual(m.playlist, want) {
		t.Fatalf("playlist = %#v, want %#v", m.playlist, want)
	}
}

func TestRemovePlaylistSelectedAdjustsCursor(t *testing.T) {
	videos := []video{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	m := model{
		playlist:         append([]video(nil), videos...),
		playlistSelected: 0,
		playlistIndex:    1,
	}

	removed, ok := m.removePlaylistSelected()
	if !ok || removed != videos[0] {
		t.Fatalf("removePlaylistSelected() = (%#v, %v), want first item", removed, ok)
	}
	if !reflect.DeepEqual(m.playlist, videos[1:]) {
		t.Fatalf("playlist = %#v, want %#v", m.playlist, videos[1:])
	}
	if m.playlistIndex != 0 || m.playlistSelected != 0 {
		t.Fatalf("indices = (%d, %d), want (0, 0)", m.playlistIndex, m.playlistSelected)
	}
}

func TestCannotRemovePlayingTrack(t *testing.T) {
	playing := video{ID: "playing"}
	m := model{
		playlist:         []video{playing},
		playlistSelected: 0,
		playlistIndex:    0,
		nowPlaying:       &playing,
	}

	if _, ok := m.removePlaylistSelected(); ok {
		t.Fatal("removed the actively playing track")
	}
}

func TestMovePlaylistSelectedTracksPlayingItem(t *testing.T) {
	videos := []video{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	m := model{
		playlist:         append([]video(nil), videos...),
		playlistSelected: 0,
		playlistIndex:    1,
	}

	if !m.movePlaylistSelected(1) {
		t.Fatal("movePlaylistSelected(1) did not move")
	}
	want := []video{videos[1], videos[0], videos[2]}
	if !reflect.DeepEqual(m.playlist, want) {
		t.Fatalf("playlist = %#v, want %#v", m.playlist, want)
	}
	if m.playlistIndex != 0 || m.playlistSelected != 1 {
		t.Fatalf("indices = (%d, %d), want (0, 1)", m.playlistIndex, m.playlistSelected)
	}
}

func TestPlaylistNavigationKeepsAllTracks(t *testing.T) {
	playlist := []video{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	m := model{
		playlist:      append([]video(nil), playlist...),
		playlistIndex: 0,
	}

	if !m.advancePlaylist() || m.playlistIndex != 1 {
		t.Fatalf("advancePlaylist left index at %d, want 1", m.playlistIndex)
	}
	if !m.retreatPlaylist() || m.playlistIndex != 0 {
		t.Fatalf("retreatPlaylist left index at %d, want 0", m.playlistIndex)
	}
	if !reflect.DeepEqual(m.playlist, playlist) {
		t.Fatalf("navigation changed playlist: %#v", m.playlist)
	}
}

func TestPlaybackCompletionKeepsFinishedTrack(t *testing.T) {
	finished := video{ID: "finished", Title: "Finished"}
	m := model{
		playlist:           []video{finished},
		playlistIndex:      0,
		nowPlaying:         &finished,
		position:           59,
		duration:           60,
		playbackGeneration: 3,
	}

	updated, cmd := m.Update(playbackDoneMsg{generation: 3})
	got := updated.(model)
	if cmd != nil {
		t.Fatal("completion returned a command at the end of the playlist")
	}
	if got.nowPlaying != nil || got.position != 0 || got.duration != 0 {
		t.Fatalf("playback state was not cleared: %#v", got)
	}
	if !reflect.DeepEqual(got.playlist, []video{finished}) || got.playlistIndex != 0 {
		t.Fatalf("finished track was removed: %#v", got.playlist)
	}
	if got.status != "Playback finished" {
		t.Fatalf("status = %q, want Playback finished", got.status)
	}
}

func TestPlaybackErrorRetainsPlaylist(t *testing.T) {
	failed := video{ID: "failed"}
	queued := video{ID: "queued"}
	m := model{
		playlist:           []video{failed, queued},
		playlistIndex:      0,
		nowPlaying:         &failed,
		playbackGeneration: 4,
	}

	updated, cmd := m.Update(playbackDoneMsg{generation: 4, err: errors.New("broken stream")})
	got := updated.(model)
	if cmd != nil {
		t.Fatal("playback error returned a command")
	}
	if !reflect.DeepEqual(got.playlist, []video{failed, queued}) {
		t.Fatalf("playlist changed after error: %#v", got.playlist)
	}
	if got.nowPlaying != nil {
		t.Fatalf("failed track remained in playback state: %#v", got)
	}
	if got.status != "Playback error: broken stream" {
		t.Fatalf("status = %q, want playback diagnostic", got.status)
	}
}

func TestPlaylistViewShowsCountTitlesAndStates(t *testing.T) {
	playing := video{Title: "Playing Now", Channel: "Two", Duration: 120}
	m := model{
		width:         100,
		height:        20,
		activePane:    playlistPane,
		playlistIndex: 1,
		nowPlaying:    &playing,
		playlist: []video{
			{Title: "Already Played", Channel: "One", Duration: 60},
			playing,
			{Title: "Up Next", Channel: "Three", Duration: 180},
		},
	}

	view := m.View()
	for _, want := range []string{
		"Results", "Playlist (3)", "Already Played", "Playing Now", "Up Next",
		"[Played]", "[Playing]", "[Queued]",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() does not contain %q:\n%s", want, view)
		}
	}
}

func TestResultsViewShowsAddControl(t *testing.T) {
	m := model{width: 100, height: 20, activePane: resultsPane}
	view := m.View()
	if !strings.Contains(view, "[a Add]") {
		t.Fatalf("View() does not show the add control:\n%s", view)
	}
	for _, code := range []string{bbsTitle, bbsActiveTab, bbsControls} {
		if !strings.Contains(view, code) {
			t.Fatalf("View() does not contain BBS color %q:\n%s", code, view)
		}
	}
}
