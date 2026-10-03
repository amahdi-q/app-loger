package applog

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTest(t *testing.T, cfg Config, now time.Time) *Logger {
	t.Helper()
	cfg.RootDir = t.TempDir()
	cfg.Location = time.UTC
	l, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	l.now = func() time.Time { return now }
	return l
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSessionFileLayoutAndFormat(t *testing.T) {
	now := time.Date(2026, 10, 2, 14, 3, 11, 0, time.UTC)
	l := newTest(t, Config{}, now)

	s, err := l.Session("STC", "100245")
	if err != nil {
		t.Fatal(err)
	}
	s.Step("Bill inquiry")
	s.Info("Bill payment request received")
	s.Field("Mobile number", "33333333")
	s.End("success")

	got := read(t, filepath.Join(l.cfg.RootDir, "STC", "2026", "10", "02", "100245.txt"))
	for _, want := range []string{
		" KeySessionID : 100245",
		"[02/10/2026 14:03:11] BILL INQUIRY",
		"[02/10/2026 14:03:11] Bill payment request received\n",
		"[02/10/2026 14:03:11] Mobile number: 33333333\n",
		"TRANSACTION END - STATUS: SUCCESS",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestSessionStaysInStartDayAcrossMidnight(t *testing.T) {
	start := time.Date(2026, 10, 2, 23, 59, 50, 0, time.UTC)
	l := newTest(t, Config{}, start)
	s, _ := l.Session("Zain", "7")
	s.Info("inquiry")

	l.now = func() time.Time { return start.Add(time.Minute) }
	s2, _ := l.Session("Zain", "7")
	s2.Info("confirm")

	got := read(t, filepath.Join(l.cfg.RootDir, "Zain", "2026", "10", "02", "7.txt"))
	if strings.Count(got, "KeySessionID") != 1 || !strings.Contains(got, "confirm") {
		t.Errorf("unexpected file:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(l.cfg.RootDir, "Zain", "2026", "10", "03")); err == nil {
		t.Error("transaction split into next day's folder")
	}
}

func TestCustomLayoutNameAndErrorsFile(t *testing.T) {
	now := time.Date(2026, 1, 5, 8, 0, 0, 0, time.UTC)
	l := newTest(t, Config{
		DirLayout:    "{yyyy}-{mm}/{folder}",
		FileExt:      ".log",
		ErrorsFolder: "Errors",
		ErrorsFile:   "all_errors",
		MinLevel:     LevelInfo,
	}, now)

	sys, err := l.Stream("System/Core", "server_{dd}")
	if err != nil {
		t.Fatal(err)
	}
	sys.Debug("hidden")
	sys.Warn("disk almost full")

	got := read(t, filepath.Join(l.cfg.RootDir, "2026-01", "System", "Core", "server_05.log"))
	if strings.Contains(got, "hidden") || !strings.Contains(got, "[WARNING] disk almost full") {
		t.Errorf("stream file:\n%s", got)
	}
	errs := read(t, filepath.Join(l.cfg.RootDir, "2026-01", "Errors", "all_errors.log"))
	if !strings.Contains(errs, "[WARNING] [System/Core/server_{dd}] disk almost full") {
		t.Errorf("errors file:\n%s", errs)
	}
}

func TestRejectsPathTraversal(t *testing.T) {
	l := newTest(t, Config{}, time.Now())
	for _, c := range [][2]string{{"../etc", "x"}, {"STC", "../x"}, {"STC", "a/b"}, {"", "x"}} {
		if _, err := l.Session(c[0], c[1]); err == nil {
			t.Errorf("accepted %q/%q", c[0], c[1])
		}
	}
}

func TestConcurrentWritesDoNotInterleave(t *testing.T) {
	l := newTest(t, Config{}, time.Now())
	w, _ := l.Stream("System", "server")
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			w.Payload("body", strings.Repeat("x", 500)+"\n"+strings.Repeat("y", 500))
		})
	}
	wg.Wait()
	got := read(t, w.path(l.now()))
	if strings.Count(got, "body:\n    "+strings.Repeat("x", 500)+"\n    "+strings.Repeat("y", 500)+"\n") != 50 {
		t.Error("payloads interleaved")
	}
}
