# BoardTube

BoardTube is a Go terminal app that searches YouTube and plays audio without
opening a video window. It targets macOS and Linux.

## Dependencies

- Go 1.24.0 or newer to build
- [`yt-dlp`](https://github.com/yt-dlp/yt-dlp) for YouTube search and media URL handling
- [`mpv`](https://mpv.io/) for audio playback

`yt-dlp` is primarily a separate command-line program. Although it can also be
used as a Python package, this Go app invokes its CLI. `mpv` uses `yt-dlp` to
resolve a YouTube page to a media stream, then plays only its audio.

On macOS:

```sh
brew install yt-dlp mpv
```

## Build and run

```sh
cd ~/projects/boardtube
go build -o boardtube .
./boardtube
```

An initial search can be supplied on the command line:

```sh
./boardtube "boards of canada"
```

## Color themes

Choose a theme with `--theme`, before the search query:

```sh
./boardtube --theme mono
./boardtube --theme ocean "boards of canada"
./boardtube --theme=matrix "ambient music"
./boardtube --help
```

| Theme | Palette |
| --- | --- |
| `retro` | Dusty mauve and muted teal (default) |
| `ocean` | Midnight blue highlights and muted steel-blue text |
| `sunset` | Warm sand and dusty rose |
| `matrix` | Muted sage text with phosphor-green title and controls |
| `mono` | Black and white, with bold/underline status distinctions |

Use `--` before a query that starts with a dash, such as
`./boardtube --theme mono -- --help`. Themes apply to the app's text and
highlights; the terminal supplies the background. There is no config file.

## Keys

| Key | Action |
| --- | --- |
| `Tab` | Switch between Results and Playlist |
| `↑`/`↓` or `j`/`k` | Select an item in the active pane |
| `Enter` | Play the selected result or playlist item |
| `a` | Add the selected result to the end of the playlist |
| `J`/`K` | Move the selected playlist item down/up |
| `d` | Remove the selected playlist item |
| `n` | Play the next playlist item |
| `b` | Play the previous track from the beginning |
| `←`/`→` | Seek backward/forward 10 seconds |
| `-` / `+` (or `=`) | Lower / raise playback volume by 5% |
| `Space` | Pause or resume current track |
| `p` | Resume, or play the selected result |
| `x` | Stop |
| `/` or `s` | New search |
| `Ctrl-C` | Quit |

While editing a search, use `←`/`→` to move the cursor, `Home`/`End` (or
`Ctrl-A`/`Ctrl-E`) to jump to the start/end, and `Backspace`/`Delete` to remove
text before/at the cursor. Press `/` to edit the current query or `s` to start
a blank search.

Playback volume starts at 100%, ranges from 0–100%, and carries across tracks
for the current session. These controls change mpv volume only, leaving macOS
/system volume unchanged. While editing a search, these keys enter text.

The playlist is manual, allows duplicates, and lasts only for the current app
session. Tracks remain in the list after playback and are marked `Played`,
`Playing`, or `Queued`. A track finishing normally starts the next playlist
item. Playlist saving is not currently supported.

The app streams media and does not save it. YouTube changes can affect search
or playback, so keep `yt-dlp` current. While playing, the footer shows the
current track, elapsed time, total duration, progress bar, and controls.
