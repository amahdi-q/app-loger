// Package applog writes human-readable text log files into a configurable
// folder tree, e.g.
//
//	<RootDir>/<folder>/<yyyy>/<mm>/<dd>/<name>.txt
//
// It supports two kinds of log files:
//
//   - Streams (Logger.Stream): general server logs such as "System/server" or
//     "Database/db". The date folders roll over automatically every day.
//   - Sessions (Logger.Session): one file per transaction (named after the
//     KeySessionID), pinned to the date the transaction started, with a header,
//     step sections and an end footer.
//
// Every line is written as "[dd/mm/yyyy hh:mm:ss] message".
package applog

import (
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Level is the severity of a log line.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (lv Level) tag() string {
	switch lv {
	case LevelDebug:
		return "[DEBUG] "
	case LevelWarn:
		return "[WARNING] "
	case LevelError:
		return "[ERROR] "
	}
	return ""
}

// Config controls where files are saved and how they are named.
type Config struct {
	// RootDir is the top-level folder, e.g. "/var/log/PIEServer". Required.
	RootDir string

	// DirLayout is the folder structure under RootDir. Tokens:
	// {folder} {yyyy} {mm} {dd} {hh}. Default: "{folder}/{yyyy}/{mm}/{dd}".
	DirLayout string

	// FileExt is appended to every file name. Default: ".txt".
	FileExt string

	// TimeFormat is the Go time layout used inside [...]. Default: "02/01/2006 15:04:05".
	TimeFormat string

	// Location is the time zone for timestamps and date folders. Default: time.Local.
	Location *time.Location

	// MinLevel drops lines below this level. Default: LevelDebug (log everything).
	MinLevel Level

	// ErrorsFolder and ErrorsFile, when both set, make every warning and error
	// from any stream or session also get copied into one central file
	// (e.g. "Errors" + "errors"), tagged with where it came from.
	ErrorsFolder string
	ErrorsFile   string

	// Mask, if set, is applied to every Field value, e.g. to hide card numbers.
	Mask func(label, value string) string

	// OnWriteError is called when a log line cannot be written. Default: print to stderr.
	OnWriteError func(err error)
}

// Logger is safe for concurrent use. Create one per process with New.
type Logger struct {
	cfg    Config
	now    func() time.Time
	locks  [64]sync.Mutex // striped per-file locks
	mu     sync.Mutex     // guards session creation
	errors *Writer
}

// New validates cfg, applies defaults and creates RootDir.
func New(cfg Config) (*Logger, error) {
	if strings.TrimSpace(cfg.RootDir) == "" {
		return nil, errors.New("applog: RootDir is required")
	}
	if cfg.DirLayout == "" {
		cfg.DirLayout = "{folder}/{yyyy}/{mm}/{dd}"
	}
	if cfg.FileExt == "" {
		cfg.FileExt = ".txt"
	}
	if cfg.TimeFormat == "" {
		cfg.TimeFormat = "02/01/2006 15:04:05"
	}
	if cfg.Location == nil {
		cfg.Location = time.Local
	}
	if cfg.OnWriteError == nil {
		cfg.OnWriteError = func(err error) { fmt.Fprintln(os.Stderr, "applog:", err) }
	}
	if err := os.MkdirAll(cfg.RootDir, 0o755); err != nil {
		return nil, fmt.Errorf("applog: create root dir: %w", err)
	}

	l := &Logger{cfg: cfg, now: time.Now}
	if cfg.ErrorsFolder != "" && cfg.ErrorsFile != "" {
		w, err := l.Stream(cfg.ErrorsFolder, cfg.ErrorsFile)
		if err != nil {
			return nil, fmt.Errorf("applog: errors file: %w", err)
		}
		l.errors = w
	}
	return l, nil
}

// Stream returns a daily-rolling log file named name inside folder.
// folder may be nested ("Services/STC"); name may use date tokens ("server_{yyyy}{mm}{dd}").
func (l *Logger) Stream(folder, name string) (*Writer, error) {
	if err := validate(folder, name); err != nil {
		return nil, err
	}
	return &Writer{
		l:      l,
		source: folder + "/" + name,
		path:   func(t time.Time) string { return l.resolve(folder, name, t) },
	}, nil
}

// Writer writes timestamped lines to one log file.
type Writer struct {
	l      *Logger
	source string // shown in the central errors file
	path   func(t time.Time) string
}

func (w *Writer) Debug(msg string) { w.log(LevelDebug, msg) }
func (w *Writer) Info(msg string)  { w.log(LevelInfo, msg) }
func (w *Writer) Warn(msg string)  { w.log(LevelWarn, msg) }
func (w *Writer) Error(msg string) { w.log(LevelError, msg) }

func (w *Writer) Debugf(format string, a ...any) { w.log(LevelDebug, fmt.Sprintf(format, a...)) }
func (w *Writer) Infof(format string, a ...any)  { w.log(LevelInfo, fmt.Sprintf(format, a...)) }
func (w *Writer) Warnf(format string, a ...any)  { w.log(LevelWarn, fmt.Sprintf(format, a...)) }
func (w *Writer) Errorf(format string, a ...any) { w.log(LevelError, fmt.Sprintf(format, a...)) }

// Field writes "label: value", e.g. Field("Mobile number", "33333333").
func (w *Writer) Field(label string, value any) {
	v := fmt.Sprint(value)
	if w.l.cfg.Mask != nil {
		v = w.l.cfg.Mask(label, v)
	}
	w.log(LevelInfo, label+": "+v)
}

// Payload writes a multi-line body (SOAP XML, JSON, ...) indented under a label.
func (w *Writer) Payload(label, body string) {
	if LevelInfo < w.l.cfg.MinLevel {
		return
	}
	t := w.l.now().In(w.l.cfg.Location)
	var b strings.Builder
	b.WriteString(w.l.line(t, LevelInfo, label+":"))
	for ln := range strings.SplitSeq(strings.TrimRight(body, "\r\n"), "\n") {
		b.WriteString("    ")
		b.WriteString(strings.TrimRight(ln, "\r"))
		b.WriteString("\n")
	}
	w.l.write(w.path(t), b.String())
}

// Step writes a section header, used to separate each request/response stage.
func (w *Writer) Step(title string) {
	t := w.l.now().In(w.l.cfg.Location)
	bar := strings.Repeat("=", 80)
	w.l.write(w.path(t), fmt.Sprintf("\n%s\n[%s] %s\n%s\n",
		bar, t.Format(w.l.cfg.TimeFormat), strings.ToUpper(title), bar))
}

func (w *Writer) log(lv Level, msg string) {
	if lv < w.l.cfg.MinLevel {
		return
	}
	t := w.l.now().In(w.l.cfg.Location)
	w.l.write(w.path(t), w.l.line(t, lv, msg))
	if lv >= LevelWarn && w.l.errors != nil && w != w.l.errors {
		w.l.write(w.l.errors.path(t), w.l.line(t, lv, "["+w.source+"] "+msg))
	}
}

func (l *Logger) line(t time.Time, lv Level, msg string) string {
	return "[" + t.Format(l.cfg.TimeFormat) + "] " + lv.tag() + msg + "\n"
}

// write appends data to path, creating folders as needed. Each call is one
// write under a per-file lock, so concurrent lines never interleave.
func (l *Logger) write(path, data string) {
	h := fnv.New32a()
	h.Write([]byte(path))
	mu := &l.locks[h.Sum32()%uint32(len(l.locks))]
	mu.Lock()
	defer mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		l.cfg.OnWriteError(err)
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		l.cfg.OnWriteError(err)
		return
	}
	if _, err := f.WriteString(data); err != nil {
		l.cfg.OnWriteError(err)
	}
	if err := f.Close(); err != nil {
		l.cfg.OnWriteError(err)
	}
}

func (l *Logger) resolve(folder, name string, t time.Time) string {
	dir := expand(l.cfg.DirLayout, folder, t)
	return filepath.Join(l.cfg.RootDir, filepath.FromSlash(dir), expand(name, folder, t)+l.cfg.FileExt)
}

func expand(s, folder string, t time.Time) string {
	return strings.NewReplacer(
		"{folder}", folder,
		"{yyyy}", t.Format("2006"),
		"{mm}", t.Format("01"),
		"{dd}", t.Format("02"),
		"{hh}", t.Format("15"),
	).Replace(s)
}

var safeName = regexp.MustCompile(`^[A-Za-z0-9 _.\-{}]+$`)

// validate rejects names that could escape RootDir or are not valid file names.
func validate(folder, name string) error {
	if folder == "" || name == "" {
		return errors.New("applog: folder and name are required")
	}
	for seg := range strings.SplitSeq(folder, "/") {
		if seg == "" || seg == "." || seg == ".." || !safeName.MatchString(seg) {
			return fmt.Errorf("applog: invalid folder %q", folder)
		}
	}
	if name == "." || name == ".." || !safeName.MatchString(name) {
		return fmt.Errorf("applog: invalid file name %q", name)
	}
	return nil
}
