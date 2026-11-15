package main

import (
	"errors"
	"flag"
	"io"
	"strings"
	"testing"
)

func TestLaunchOptions(t *testing.T) {
	for _, tc := range []struct {
		name         string
		args         []string
		theme, query string
		wantError    bool
	}{
		{name: "defaults", theme: "retro"},
		{name: "existing query", args: []string{"boards", "of canada"}, theme: "retro", query: "boards of canada"},
		{name: "theme and query", args: []string{"--theme", "mono", "jazz music"}, theme: "mono", query: "jazz music"},
		{name: "equals", args: []string{"--theme=ocean"}, theme: "ocean"},
		{name: "dash query", args: []string{"--", "--help"}, theme: "retro", query: "--help"},
		{name: "unknown theme", args: []string{"--theme", "missing"}, wantError: true},
		{name: "missing value", args: []string{"--theme"}, wantError: true},
		{name: "unknown flag", args: []string{"--missing"}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseOptions(tc.args, io.Discard)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v", err)
			}
			if err == nil && (got.theme != tc.theme || got.query != tc.query) {
				t.Fatalf("got %+v", got)
			}
		})
	}
	var help strings.Builder
	if _, err := parseOptions([]string{"--help"}, &help); !errors.Is(err, flag.ErrHelp) || !strings.Contains(help.String(), themeNames) {
		t.Fatalf("help: %q, error: %v", help.String(), err)
	}
}

func TestThemeRendering(t *testing.T) {
	for name, theme := range themes {
		t.Run(name, func(t *testing.T) {
			m := initialModel("test")
			m.theme, m.width, m.height = theme, 100, 24
			m.status = "Search error: test"
			view := m.View()
			for _, want := range []string{theme.Title + "◆ BoardTube ◆", theme.ActiveTab + " Results ", theme.Error + m.status} {
				if !strings.Contains(view, want) {
					t.Fatalf("missing styled content %q", want)
				}
			}
			m.enteringSearch = true
			if !strings.Contains(m.View(), theme.Selected+" ") {
				t.Fatal("cursor does not use theme")
			}
		})
	}
	mono := themes["mono"]
	// Verify every role is neutral, including errors and playing states.
	for _, style := range []string{mono.Title, mono.Label, mono.Value, mono.Good, mono.Warning, mono.Error, mono.ActiveTab, mono.InactiveTab, mono.Selected, mono.Played, mono.Playing, mono.Queued, mono.Controls, mono.Hints, mono.Progress, mono.EmptyPlaying} {
		codes := strings.Split(strings.TrimSuffix(strings.TrimPrefix(style, "\033["), "m"), ";")
		for _, code := range codes {
			switch code {
			case "1", "2", "4", "30", "37", "97", "107":
			default:
				t.Fatalf("non-monochrome style %q", style)
			}
		}
	}
}
