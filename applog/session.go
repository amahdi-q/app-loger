package applog

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Session is the log file of one transaction, named after its KeySessionID.
// It embeds Writer, so Info/Warn/Error/Field/Payload/Step all work on it.
type Session struct {
	*Writer
	ID string
}

// Session opens (or continues) the transaction file for id inside folder,
// e.g. Session("STC", "100245") -> <RootDir>/STC/2026/10/02/100245.txt.
//
// Every request of the same transaction (inquiry, authorize, confirm, ...)
// calls Session with the same id and appends to the same file. The file stays
// in the date folder where the transaction started, even across midnight.
func (l *Logger) Session(folder, id string) (*Session, error) {
	if err := validate(folder, id); err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now().In(l.cfg.Location)
	path := ""
	for _, day := range []time.Time{now, now.AddDate(0, 0, -1)} {
		p := l.resolve(folder, id, day)
		if _, err := os.Stat(p); err == nil {
			path = p
			break
		}
	}
	if path == "" {
		path = l.resolve(folder, id, now)
		bar := strings.Repeat("#", 80)
		l.write(path, fmt.Sprintf("%s\n Service      : %s\n KeySessionID : %s\n Started      : %s\n%s\n",
			bar, folder, id, now.Format(l.cfg.TimeFormat), bar))
	}

	return &Session{
		Writer: &Writer{
			l:      l,
			source: folder + "/" + id,
			path:   func(time.Time) string { return path },
		},
		ID: id,
	}, nil
}

// End writes the closing footer with the final status (e.g. "SUCCESS", "FAILED").
func (s *Session) End(status string) {
	t := s.l.now().In(s.l.cfg.Location)
	bar := strings.Repeat("-", 80)
	s.l.write(s.path(t), fmt.Sprintf("\n%s\n[%s] TRANSACTION END - STATUS: %s\n%s\n",
		bar, t.Format(s.l.cfg.TimeFormat), strings.ToUpper(status), bar))
}
