package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestVolumeKeys(t *testing.T) {
	m := initialModel("music")
	for _, step := range []struct {
		key  string
		want int
	}{
		{"-", 95}, {"-", 90}, {"+", 95}, {"=", 100}, {"+", 100},
	} {
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(step.key)})
		m = updated.(model)
		if m.volume != step.want || cmd != nil {
			t.Fatalf("key %s: volume %d, command present %v", step.key, m.volume, cmd != nil)
		}
	}
	for i := 0; i < 25; i++ {
		m.changeVolume(-5)
	}
	if m.volume != 0 {
		t.Fatalf("minimum volume = %d", m.volume)
	}
	m.width, m.height = 100, 24
	if !strings.Contains(m.View(), "Volume: 0%") {
		t.Fatal("volume missing from footer")
	}
	m.enteringSearch = true
	m.input, m.inputCursor = "", 0
	for _, key := range []string{"-", "+", "="} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m = updated.(model)
	}
	if m.input != "-+=" || m.volume != 0 {
		t.Fatalf("search input=%q volume=%d", m.input, m.volume)
	}
}

func TestVolumeRequestsAreSerialized(t *testing.T) {
	m := initialModel("music")
	// A process marker enables controls; no commands are run against this process.
	m.player = &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}}
	m.playbackGeneration = 2
	if cmd := m.changeVolume(-5); cmd == nil || !m.volumeUpdating {
		t.Fatal("first volume request missing")
	}
	if cmd := m.changeVolume(-5); cmd != nil || m.volume != 90 {
		t.Fatal("second request was not queued")
	}
	updated, cmd := m.Update(volumeDoneMsg{generation: 1, volume: 95})
	m = updated.(model)
	if cmd != nil || !m.volumeUpdating {
		t.Fatal("stale response changed pending request")
	}
	updated, cmd = m.Update(volumeDoneMsg{generation: 2, volume: 95})
	m = updated.(model)
	if cmd == nil || !m.volumeUpdating {
		t.Fatal("latest volume was not sent after completion")
	}
	updated, cmd = m.Update(volumeDoneMsg{generation: 2, volume: 90})
	m = updated.(model)
	if cmd != nil || m.volumeUpdating {
		t.Fatal("completed volume request still pending")
	}
	m.changeVolume(-5)
	updated, _ = m.Update(volumeDoneMsg{generation: 2, err: errors.New("disconnected")})
	m = updated.(model)
	if m.volumeUpdating || !strings.Contains(m.status, "Volume control error") {
		t.Fatal("volume error was not reported")
	}
}
