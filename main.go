package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	resultLimit     = 20
	seekStepSeconds = 10

	ansiReset       = "\033[0m"
	bbsTitle        = "\033[1;95m"
	bbsLabel        = "\033[1;96m"
	bbsValue        = "\033[93m"
	bbsGood         = "\033[1;92m"
	bbsWarning      = "\033[1;93m"
	bbsError        = "\033[1;91m"
	bbsActiveTab    = "\033[1;97;45m"
	bbsInactiveTab  = "\033[96m"
	bbsSelected     = "\033[1;30;106m"
	bbsPlayed       = "\033[2;37m"
	bbsPlaying      = "\033[1;92m"
	bbsQueued       = "\033[96m"
	bbsControls     = "\033[1;93m"
	bbsHints        = "\033[2;96m"
	bbsProgress     = "\033[96m"
	bbsEmptyPlaying = "\033[2;37m"
)

type video struct {
	ID       string
	Title    string
	Channel  string
	Duration float64
}

func (v video) URL() string {
	return "https://www.youtube.com/watch?v=" + v.ID
}

type searchResultMsg struct {
	query  string
	videos []video
	err    error
}

type playbackDoneMsg struct {
	generation int
	err        error
}

type progressTickMsg struct {
	generation int
}

type progressMsg struct {
	generation int
	position   float64
	err        error
}

type controlDoneMsg struct {
	generation int
	paused     bool
	err        error
}

type seekDoneMsg struct {
	generation int
	offset     int
	err        error
}

type pane int

const (
	resultsPane pane = iota
	playlistPane
)

type model struct {
	query              string
	input              string
	searching          bool
	enteringSearch     bool
	results            []video
	selected           int
	playlist           []video
	playlistSelected   int
	playlistIndex      int
	activePane         pane
	width              int
	height             int
	status             string
	player             *exec.Cmd
	nowPlaying         *video
	position           float64
	duration           float64
	ipcSocket          string
	paused             bool
	playbackGeneration int
}

func initialModel(query string) model {
	m := model{
		query:         query,
		input:         query,
		playlistIndex: -1,
		status:        "Press / to search",
	}
	if query == "" {
		m.enteringSearch = true
		m.status = "Type a search and press Enter"
	}
	return m
}

func (m model) Init() tea.Cmd {
	if m.query == "" {
		return nil
	}
	return searchCmd(m.query)
}

func searchCmd(query string) tea.Cmd {
	return func() tea.Msg {
		args := []string{
			fmt.Sprintf("ytsearch%d:%s", resultLimit, query),
			"--flat-playlist",
			"--dump-json",
			"--no-warnings",
		}
		cmd := exec.Command("yt-dlp", args...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return searchResultMsg{query: query, err: err}
		}
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			return searchResultMsg{query: query, err: err}
		}

		videos, decodeErr := decodeVideos(stdout)
		waitErr := cmd.Wait()
		if decodeErr != nil {
			return searchResultMsg{query: query, err: decodeErr}
		}
		if waitErr != nil {
			message := strings.TrimSpace(stderr.String())
			if message == "" {
				message = waitErr.Error()
			}
			return searchResultMsg{query: query, err: errors.New(lastLine(message))}
		}
		return searchResultMsg{query: query, videos: videos}
	}
}

func decodeVideos(reader io.Reader) ([]video, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	videos := make([]video, 0, resultLimit)

	for scanner.Scan() {
		var item struct {
			ID       string  `json:"id"`
			Title    string  `json:"title"`
			Channel  string  `json:"channel"`
			Uploader string  `json:"uploader"`
			Duration float64 `json:"duration"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			continue
		}
		if item.ID == "" {
			continue
		}
		if item.Title == "" {
			item.Title = "Untitled"
		}
		if item.Channel == "" {
			item.Channel = item.Uploader
		}
		if item.Channel == "" {
			item.Channel = "Unknown channel"
		}
		videos = append(videos, video{
			ID:       item.ID,
			Title:    item.Title,
			Channel:  item.Channel,
			Duration: item.Duration,
		})
	}
	return videos, scanner.Err()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case searchResultMsg:
		if msg.query != m.query {
			return m, nil
		}
		m.searching = false
		if msg.err != nil {
			m.results = nil
			m.status = "Search error: " + msg.err.Error()
			return m, nil
		}
		m.results = msg.videos
		m.selected = 0
		m.status = fmt.Sprintf("%d results", len(msg.videos))
		return m, nil

	case playbackDoneMsg:
		if msg.generation != m.playbackGeneration {
			return m, nil
		}
		m.player = nil
		m.paused = false
		if m.ipcSocket != "" {
			_ = os.Remove(m.ipcSocket)
			m.ipcSocket = ""
		}
		if msg.err != nil {
			m.nowPlaying = nil
			m.position = 0
			m.duration = 0
			m.status = "Playback error: " + msg.err.Error()
			return m, nil
		}

		m.nowPlaying = nil
		m.position = 0
		m.duration = 0
		if !m.advancePlaylist() {
			m.status = "Playback finished"
			return m, nil
		}
		return m, m.startPlaylistTrack()

	case progressTickMsg:
		if msg.generation != m.playbackGeneration || m.player == nil {
			return m, nil
		}
		return m, queryProgressCmd(msg.generation, m.ipcSocket)

	case progressMsg:
		if msg.generation != m.playbackGeneration || m.player == nil {
			return m, nil
		}
		if msg.err == nil {
			m.position = msg.position
		}
		return m, progressTickCmd(msg.generation)

	case controlDoneMsg:
		if msg.generation != m.playbackGeneration {
			return m, nil
		}
		if msg.err != nil {
			m.status = "Playback control error: " + msg.err.Error()
		} else {
			m.paused = msg.paused
			if msg.paused {
				m.status = "Paused"
			} else if m.nowPlaying == nil {
				m.status = "Nothing is playing"
			} else {
				m.status = "Playing: " + m.nowPlaying.Title
			}
		}
		return m, nil

	case seekDoneMsg:
		if msg.generation != m.playbackGeneration || m.player == nil {
			return m, nil
		}
		if msg.err != nil {
			m.status = "Seek error: " + msg.err.Error()
			return m, nil
		}
		if msg.offset < 0 {
			m.status = fmt.Sprintf("Seeked back %d seconds", -msg.offset)
		} else {
			m.status = fmt.Sprintf("Seeked forward %d seconds", msg.offset)
		}
		m.position = max(0, m.position+float64(msg.offset))
		if m.duration > 0 {
			m.position = min(m.position, m.duration)
		}
		return m, nil

	case tea.KeyMsg:
		if m.enteringSearch {
			return m.updateSearchInput(msg)
		}

		switch msg.String() {
		case "ctrl+c":
			m.stopPlayer()
			return m, tea.Quit
		case "/":
			m.enteringSearch = true
			m.input = m.query
			m.status = "Type a search and press Enter"
		case "s":
			m.enteringSearch = true
			m.input = ""
			m.status = "Type a search and press Enter"
		case "tab":
			if m.activePane == resultsPane {
				m.activePane = playlistPane
			} else {
				m.activePane = resultsPane
			}
		case " ":
			return m, m.setPaused(!m.paused)
		case "left":
			return m, m.seek(-seekStepSeconds)
		case "right":
			return m, m.seek(seekStepSeconds)
		case "p":
			if m.player != nil {
				return m, m.setPaused(false)
			}
			if m.activePane == resultsPane && len(m.results) > 0 {
				return m, m.playSelected()
			}
		case "x":
			m.stopPlayer()
			m.status = "Stopped"
		case "n":
			return m, m.playNext()
		case "b":
			return m, m.playPrevious()
		default:
			if m.activePane == resultsPane {
				return m.updateResultsKey(msg)
			}
			return m.updatePlaylistKey(msg)
		}
	}
	return m, nil
}

func (m model) updateResultsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
	case "down", "j":
		if m.selected+1 < len(m.results) {
			m.selected++
		}
	case "a":
		if len(m.results) == 0 {
			m.status = "No result to add"
			break
		}
		selected := m.results[m.selected]
		m.addToPlaylist(selected)
		m.status = "Added to playlist: " + selected.Title
	case "enter":
		if len(m.results) == 0 {
			m.status = "No result to play"
			break
		}
		return m, m.playSelected()
	}
	return m, nil
}

func (m model) updatePlaylistKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		if len(m.playlist) == 0 {
			m.status = "Playlist is empty"
		} else if m.playlistSelected > 0 {
			m.playlistSelected--
		}
	case "down", "j":
		if len(m.playlist) == 0 {
			m.status = "Playlist is empty"
		} else if m.playlistSelected+1 < len(m.playlist) {
			m.playlistSelected++
		}
	case "K":
		if !m.movePlaylistSelected(-1) {
			m.status = "Cannot move playlist item up"
		}
	case "J":
		if !m.movePlaylistSelected(1) {
			m.status = "Cannot move playlist item down"
		}
	case "d":
		removed, ok := m.removePlaylistSelected()
		if !ok {
			if len(m.playlist) == 0 {
				m.status = "Playlist is empty"
			} else {
				m.status = "Cannot remove the playing track"
			}
		} else {
			m.status = "Removed from playlist: " + removed.Title
		}
	case "enter":
		if len(m.playlist) == 0 {
			m.status = "Playlist is empty"
			break
		}
		m.clampPlaylistSelection()
		m.playlistIndex = m.playlistSelected
		return m, m.startPlaylistTrack()
	}
	return m, nil
}

func (m model) updateSearchInput(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Type == tea.KeySpace {
		m.input += " "
		return m, nil
	}

	switch key.String() {
	case "ctrl+c":
		m.stopPlayer()
		return m, tea.Quit
	case "esc":
		m.enteringSearch = false
		m.input = m.query
		m.status = "Search cancelled"
	case "enter":
		query := strings.TrimSpace(m.input)
		if query == "" {
			return m, nil
		}
		m.query = query
		m.enteringSearch = false
		m.searching = true
		m.results = nil
		m.selected = 0
		m.status = fmt.Sprintf("Searching for %q...", query)
		return m, searchCmd(query)
	case "backspace", "ctrl+h":
		runes := []rune(m.input)
		if len(runes) > 0 {
			m.input = string(runes[:len(runes)-1])
		}
	default:
		if key.Type == tea.KeyRunes {
			m.input += string(key.Runes)
		}
	}
	return m, nil
}

func (m *model) playSelected() tea.Cmd {
	selected := m.results[m.selected]
	m.playlistIndex = m.insertPlaylistNext(selected)
	m.playlistSelected = m.playlistIndex
	return m.startPlaylistTrack()
}

func (m *model) insertPlaylistNext(item video) int {
	insertAt := min(max(m.playlistIndex+1, 0), len(m.playlist))
	m.playlist = append(m.playlist, video{})
	copy(m.playlist[insertAt+1:], m.playlist[insertAt:len(m.playlist)-1])
	m.playlist[insertAt] = item
	return insertAt
}

func (m *model) startVideo(selected video) tea.Cmd {
	m.stopPlayer()
	m.playbackGeneration++
	generation := m.playbackGeneration
	socket := fmt.Sprintf("%s/boardtube-%d-%d.sock", os.TempDir(), os.Getpid(), generation)
	_ = os.Remove(socket)
	cmd := exec.Command(
		"mpv",
		"--no-video",
		"--force-window=no",
		"--really-quiet",
		"--input-ipc-server="+socket,
		"--",
		selected.URL(),
	)
	cmd.Stdin = nil
	cmd.Stdout = nil
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		_ = os.Remove(socket)
		m.status = "Playback error: " + err.Error()
		return nil
	}

	m.player = cmd
	m.nowPlaying = &selected
	m.position = 0
	m.duration = selected.Duration
	m.ipcSocket = socket
	m.paused = false
	m.status = "Playing: " + selected.Title

	waitCmd := func() tea.Msg {
		err := cmd.Wait()
		if err != nil {
			err = commandError("mpv", err, stderr.String())
		}
		return playbackDoneMsg{generation: generation, err: err}
	}
	return tea.Batch(waitCmd, progressTickCmd(generation))
}

func (m *model) startPlaylistTrack() tea.Cmd {
	if m.playlistIndex < 0 || m.playlistIndex >= len(m.playlist) {
		m.status = "Playlist is empty"
		return nil
	}
	m.playlistSelected = m.playlistIndex
	return m.startVideo(m.playlist[m.playlistIndex])
}

func (m *model) addToPlaylist(item video) {
	m.playlist = append(m.playlist, item)
}

func (m *model) advancePlaylist() bool {
	next := m.playlistIndex + 1
	if next < 0 || next >= len(m.playlist) {
		return false
	}
	m.playlistIndex = next
	m.playlistSelected = next
	return true
}

func (m *model) playNext() tea.Cmd {
	if !m.advancePlaylist() {
		if len(m.playlist) == 0 {
			m.status = "Playlist is empty"
		} else {
			m.status = "No next track"
		}
		return nil
	}
	return m.startPlaylistTrack()
}

func (m *model) playPrevious() tea.Cmd {
	if m.retreatPlaylist() {
		return m.startPlaylistTrack()
	}
	if m.playlistIndex == 0 && len(m.playlist) > 0 {
		return m.startPlaylistTrack()
	}
	m.status = "No previous track"
	return nil
}

func (m *model) retreatPlaylist() bool {
	if m.playlistIndex <= 0 || m.playlistIndex >= len(m.playlist) {
		return false
	}
	m.playlistIndex--
	m.playlistSelected = m.playlistIndex
	return true
}

func (m *model) removePlaylistSelected() (video, bool) {
	if len(m.playlist) == 0 {
		return video{}, false
	}
	m.clampPlaylistSelection()
	if m.playlistSelected == m.playlistIndex && m.nowPlaying != nil {
		return video{}, false
	}
	removedAt := m.playlistSelected
	removed := m.playlist[removedAt]
	m.playlist = append(m.playlist[:removedAt], m.playlist[removedAt+1:]...)
	if removedAt <= m.playlistIndex {
		m.playlistIndex--
	}
	m.clampPlaylistSelection()
	return removed, true
}

func (m *model) movePlaylistSelected(delta int) bool {
	if len(m.playlist) == 0 {
		return false
	}
	m.clampPlaylistSelection()
	destination := m.playlistSelected + delta
	if destination < 0 || destination >= len(m.playlist) {
		return false
	}
	if m.playlistIndex == m.playlistSelected {
		m.playlistIndex = destination
	} else if m.playlistIndex == destination {
		m.playlistIndex = m.playlistSelected
	}
	m.playlist[m.playlistSelected], m.playlist[destination] = m.playlist[destination], m.playlist[m.playlistSelected]
	m.playlistSelected = destination
	return true
}

func (m *model) clampPlaylistSelection() {
	if len(m.playlist) == 0 {
		m.playlistSelected = 0
		return
	}
	m.playlistSelected = min(max(m.playlistSelected, 0), len(m.playlist)-1)
}

func (m *model) setPaused(paused bool) tea.Cmd {
	if m.player == nil || m.player.Process == nil {
		m.status = "Nothing is playing"
		return nil
	}
	generation := m.playbackGeneration
	socket := m.ipcSocket
	return func() tea.Msg {
		err := sendMPVCommand(socket, []any{"set_property", "pause", paused})
		return controlDoneMsg{generation: generation, paused: paused, err: err}
	}
}

func (m *model) seek(offset int) tea.Cmd {
	if m.player == nil || m.player.Process == nil {
		m.status = "Nothing is playing"
		return nil
	}
	generation := m.playbackGeneration
	socket := m.ipcSocket
	return func() tea.Msg {
		err := sendMPVCommand(socket, []any{"seek", offset, "relative+exact"})
		return seekDoneMsg{generation: generation, offset: offset, err: err}
	}
}

func (m *model) stopPlayer() {
	if m.player == nil || m.player.Process == nil {
		return
	}
	_ = m.player.Process.Kill()
	if m.ipcSocket != "" {
		_ = os.Remove(m.ipcSocket)
		m.ipcSocket = ""
	}
	m.player = nil
	m.nowPlaying = nil
	m.paused = false
	m.position = 0
	m.playbackGeneration++
}

func progressTickCmd(generation int) tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		return progressTickMsg{generation: generation}
	})
}

func queryProgressCmd(generation int, socket string) tea.Cmd {
	return func() tea.Msg {
		position, err := getMPVNumber(socket, "time-pos")
		return progressMsg{generation: generation, position: position, err: err}
	}
}

func sendMPVCommand(socket string, command []any) error {
	connection, err := net.DialTimeout("unix", socket, 500*time.Millisecond)
	if err != nil {
		return err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))

	request := struct {
		Command []any `json:"command"`
	}{Command: command}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return err
	}
	var response struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(connection).Decode(&response); err != nil {
		return err
	}
	if response.Error != "success" {
		return errors.New(response.Error)
	}
	return nil
}

func getMPVNumber(socket, property string) (float64, error) {
	connection, err := net.DialTimeout("unix", socket, 500*time.Millisecond)
	if err != nil {
		return 0, err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))

	request := struct {
		Command []any `json:"command"`
	}{Command: []any{"get_property", property}}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return 0, err
	}
	var response struct {
		Data  float64 `json:"data"`
		Error string  `json:"error"`
	}
	if err := json.NewDecoder(connection).Decode(&response); err != nil {
		return 0, err
	}
	if response.Error != "success" {
		return 0, errors.New(response.Error)
	}
	return response.Data, nil
}

func (m model) View() string {
	if m.width == 0 {
		return "Starting...\n"
	}

	var b strings.Builder
	b.WriteString(ansiStyle(bbsTitle, "◆ BoardTube ◆") + "\n")
	if m.enteringSearch {
		b.WriteString(ansiStyle(bbsLabel, "Search: ") + ansiStyle(bbsValue, m.input) + ansiStyle(bbsSelected, " ") + "\n")
	} else {
		b.WriteString(ansiStyle(bbsLabel, "Search: ") + ansiStyle(bbsValue, valueOr(m.query, "(none)")) + "\n")
	}
	b.WriteString(colorStatus(m.status) + "\n\n")

	resultsTab := "Results"
	playlistTab := fmt.Sprintf("Playlist (%d)", len(m.playlist))
	if m.activePane == resultsPane {
		resultsTab = ansiStyle(bbsActiveTab, " "+resultsTab+" ")
		playlistTab = ansiStyle(bbsInactiveTab, playlistTab)
	} else {
		resultsTab = ansiStyle(bbsInactiveTab, resultsTab)
		playlistTab = ansiStyle(bbsActiveTab, " "+playlistTab+" ")
	}
	b.WriteString(resultsTab + "  " + playlistTab + "\n")

	items := m.results
	selected := m.selected
	if m.activePane == playlistPane {
		items = m.playlist
		selected = m.playlistSelected
	}
	rows := max(1, m.height-10)
	start := max(0, selected-rows+1)
	if len(items) > rows {
		start = min(start, len(items)-rows)
	}
	end := min(len(items), start+rows)

	for i := start; i < end; i++ {
		item := items[i]
		marker := " "
		if i == selected {
			marker = "▶"
		}
		var line string
		if m.activePane == playlistPane {
			line = fmt.Sprintf(
				"%s %2d. %-9s [%8s] %s — %s",
				marker,
				i+1,
				"["+m.playlistTrackState(i)+"]",
				formatDuration(item.Duration),
				item.Title,
				item.Channel,
			)
		} else {
			line = fmt.Sprintf(
				"%s %2d. [%8s] %s — %s",
				marker,
				i+1,
				formatDuration(item.Duration),
				item.Title,
				item.Channel,
			)
		}
		line = truncate(line, m.width)
		if i == selected {
			b.WriteString(ansiStyle(bbsSelected, line))
		} else if m.activePane == playlistPane {
			switch m.playlistTrackState(i) {
			case "Played":
				b.WriteString(ansiStyle(bbsPlayed, line))
			case "Playing":
				b.WriteString(ansiStyle(bbsPlaying, line))
			default:
				b.WriteString(ansiStyle(bbsQueued, line))
			}
		} else {
			b.WriteString(ansiStyle(bbsInactiveTab, line))
		}
		b.WriteByte('\n')
	}

	if !m.enteringSearch {
		for i := end - start; i < rows; i++ {
			b.WriteByte('\n')
		}
		title := "Nothing playing"
		titleStyle := bbsEmptyPlaying
		if m.nowPlaying != nil {
			title = "Now playing: " + m.nowPlaying.Title
			titleStyle = bbsPlaying
		}
		b.WriteString(ansiStyle(titleStyle, truncate(title, m.width)) + "\n")
		b.WriteString(ansiStyle(bbsProgress, renderProgress(m.position, m.duration, m.width)) + "\n")
		if m.activePane == resultsPane {
			b.WriteString(ansiStyle(bbsControls, "[x Stop]  [Space Pause]  [←/→ Seek]  [p Play]  [a Add]  [b Previous]  [n Next]") + "\n")
			b.WriteString(ansiStyle(bbsHints, "↑/↓ or j/k select  Enter play  a add  Tab playlist  / search  Ctrl+C quit"))
		} else {
			b.WriteString(ansiStyle(bbsControls, "[x Stop]  [Space Pause]  [←/→ Seek]  [p Play]  [b Previous]  [n Next]") + "\n")
			b.WriteString(ansiStyle(bbsHints, "j/k select  J/K reorder  d remove  Enter play  Tab results  / search  Ctrl+C quit"))
		}
	}
	return b.String()
}

func ansiStyle(style, value string) string {
	return style + value + ansiReset
}

func colorStatus(status string) string {
	style := bbsWarning
	switch {
	case strings.Contains(status, "error"), strings.HasPrefix(status, "Cannot"):
		style = bbsError
	case strings.HasPrefix(status, "Playing"),
		strings.HasPrefix(status, "Added"),
		strings.HasPrefix(status, "Seeked"),
		status == "Playback finished":
		style = bbsGood
	}
	return ansiStyle(style, status)
}

func (m model) playlistTrackState(index int) string {
	if index < m.playlistIndex {
		return "Played"
	}
	if index == m.playlistIndex {
		if m.nowPlaying != nil {
			return "Playing"
		}
		return "Played"
	}
	return "Queued"
}

func renderProgress(position, duration float64, width int) string {
	barWidth := min(40, max(10, width-24))
	ratio := 0.0
	if duration > 0 {
		ratio = min(1, max(0, position/duration))
	}
	filled := int(ratio * float64(barWidth))
	bar := strings.Repeat("━", filled) + strings.Repeat("─", barWidth-filled)
	return fmt.Sprintf("%s %s / %s", bar, formatDuration(position), formatDuration(duration))
}

func formatDuration(seconds float64) string {
	if seconds <= 0 {
		return "--:--"
	}
	total := int(seconds)
	hours := total / 3600
	minutes := (total % 3600) / 60
	remaining := total % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, remaining)
	}
	return fmt.Sprintf("%d:%02d", minutes, remaining)
}

func truncate(value string, width int) string {
	runes := []rune(value)
	if width < 2 || len(runes) < width {
		return value
	}
	return string(runes[:width-1]) + "…"
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func lastLine(value string) string {
	lines := strings.Split(value, "\n")
	return lines[len(lines)-1]
}

func checkDependencies() error {
	var missing []string
	for _, name := range []string{"yt-dlp", "mpv"} {
		if _, err := exec.LookPath(name); err != nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required program(s): %s", strings.Join(missing, ", "))
	}

	if output, err := exec.Command("mpv", "--version").CombinedOutput(); err != nil {
		return fmt.Errorf(
			"%w; reinstall mpv (Homebrew: brew reinstall mpv)",
			commandError("mpv", err, string(output)),
		)
	}
	return nil
}

func commandError(name string, err error, output string) error {
	detail := diagnosticLine(output)
	if detail == "" {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return fmt.Errorf("%s failed: %s", name, detail)
}

func diagnosticLine(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Library not loaded:") {
			return line
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

func main() {
	if err := checkDependencies(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	query := strings.TrimSpace(strings.Join(os.Args[1:], " "))
	program := tea.NewProgram(initialModel(query), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "application error:", err)
		os.Exit(1)
	}
}
