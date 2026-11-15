package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

type palette struct {
	Title, Label, Value, Good, Warning, Error                 string
	ActiveTab, InactiveTab, Selected, Played, Playing, Queued string
	Controls, Hints, Progress, EmptyPlaying                   string
}

// Muted 256-color accents leave most text neutral and reserve background
// highlights for the current selection. Avoid bold, which can brighten colors.
func accentPalette(primary, secondary, background int) palette {
	foreground := func(color int) string { return fmt.Sprintf("\033[38;5;%dm", color) }
	selection := fmt.Sprintf("\033[38;5;253;48;5;%dm", background)
	return palette{
		Title: foreground(primary), Label: foreground(secondary), Value: foreground(252),
		Good: foreground(108), Warning: foreground(180), Error: foreground(174),
		ActiveTab: selection, InactiveTab: foreground(245), Selected: selection,
		Played: foreground(243), Playing: foreground(primary), Queued: foreground(250),
		Controls: foreground(250), Hints: foreground(245),
		Progress: foreground(secondary), EmptyPlaying: foreground(243),
	}
}

var themes = map[string]palette{
	"retro": {
		bbsTitle, bbsLabel, bbsValue, bbsGood, bbsWarning, bbsError,
		bbsActiveTab, bbsInactiveTab, bbsSelected, bbsPlayed, bbsPlaying, bbsQueued,
		bbsControls, bbsHints, bbsProgress, bbsEmptyPlaying,
	},
	"ocean": {
		Title: "\033[38;5;67m", Label: "\033[38;5;67m", Value: "\033[38;5;110m",
		Good: "\033[38;5;67m", Warning: "\033[38;5;110m", Error: "\033[4;38;5;110m",
		ActiveTab: "\033[38;5;153;48;5;17m", InactiveTab: "\033[38;5;67m", Selected: "\033[38;5;153;48;5;17m",
		Played: "\033[38;5;60m", Playing: "\033[38;5;110m", Queued: "\033[38;5;67m",
		Controls: "\033[38;5;67m", Hints: "\033[38;5;60m", Progress: "\033[38;5;67m", EmptyPlaying: "\033[38;5;60m",
	},
	"sunset": accentPalette(180, 138, 95),
	"matrix": {
		Title: "\033[38;5;46m", Label: "\033[38;5;108m", Value: "\033[38;5;151m",
		Good: "\033[38;5;108m", Warning: "\033[38;5;151m", Error: "\033[4;38;5;151m",
		ActiveTab: "\033[38;5;194;48;5;22m", InactiveTab: "\033[38;5;108m", Selected: "\033[38;5;194;48;5;22m",
		Played: "\033[38;5;65m", Playing: "\033[38;5;151m", Queued: "\033[38;5;108m",
		Controls: "\033[38;5;46m", Hints: "\033[38;5;40m", Progress: "\033[38;5;108m", EmptyPlaying: "\033[38;5;65m",
	},
	"mono": {
		Title: "\033[1;97m", Label: "\033[1;97m", Value: "\033[37m",
		Good: "\033[1;97m", Warning: "\033[97m", Error: "\033[1;4;97m",
		ActiveTab: "\033[1;30;107m", InactiveTab: "\033[37m", Selected: "\033[1;30;107m",
		Played: "\033[2;37m", Playing: "\033[1;97m", Queued: "\033[37m",
		Controls: "\033[1;97m", Hints: "\033[2;37m", Progress: "\033[97m", EmptyPlaying: "\033[2;37m",
	},
}

const themeNames = "retro, ocean, sunset, matrix, mono"

type launchOptions struct{ theme, query string }

func parseOptions(args []string, output io.Writer) (launchOptions, error) {
	options := launchOptions{}
	flags := flag.NewFlagSet("boardtube", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&options.theme, "theme", "retro", "color theme: "+themeNames)
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: boardtube [--theme NAME] [search query]")
		fmt.Fprintln(output, "Place options before the query; use -- to search for text beginning with a dash.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if _, ok := themes[options.theme]; !ok {
		return options, fmt.Errorf("unknown theme %q (choose: %s)", options.theme, themeNames)
	}
	options.query = strings.TrimSpace(strings.Join(flags.Args(), " "))
	return options, nil
}
