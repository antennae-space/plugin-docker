// Package i18n translates the messages of the backend. Messages are written in English (the
// format string is the key); the catalogs of the plugin (i18n/<lang>.json of its folder, the
// same as its page's) give their text in another language, picked by the language of the
// window or conversation of each call (backend.LangOf), when an error is sent.
package i18n

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	once     sync.Once
	catalogs = map[string]map[string]string{}
)

// Load reads the catalogs of the folder dir (i18n/ of the plugin); missing ones are skipped.
func Load(dir string) {
	once.Do(func() {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			lang, ok := strings.CutSuffix(e.Name(), ".json")
			if !ok {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			c := map[string]string{}
			if json.Unmarshal(data, &c) == nil {
				catalogs[lang] = c
			}
		}
	})
}

// T is a message in a language.
func T(lang, format string, args ...any) string {
	if tr, ok := catalogs[lang][format]; ok {
		format = tr
	}
	format = strings.ReplaceAll(format, "%w", "%v")
	for i, a := range args {
		if err, ok := a.(error); ok {
			args[i] = Translate(lang, err)
		}
	}
	return fmt.Sprintf(format, args...)
}

// Error is a message that can be translated: its English text is Error().
type Error struct {
	format string
	args   []any
	err    error
}

func (e *Error) Error() string { return e.err.Error() }
func (e *Error) Unwrap() error { return errors.Unwrap(e.err) }

// Errorf is fmt.Errorf with a translatable message (%w wraps as usual).
func Errorf(format string, args ...any) error {
	return &Error{format: format, args: args, err: fmt.Errorf(format, args...)}
}

// New is errors.New with a translatable message.
func New(text string) error { return &Error{format: "%s", err: errors.New(text)} }

// Translate gives the message of an error in a language; an error not made here (a driver's,
// the network's) keeps its text.
func Translate(lang string, err error) string {
	var e *Error
	if !errors.As(err, &e) || e.Error() != err.Error() {
		return err.Error()
	}
	if e.args == nil && e.format == "%s" {
		if tr, ok := catalogs[lang][e.err.Error()]; ok {
			return tr
		}
		return e.err.Error()
	}
	return T(lang, e.format, append([]any(nil), e.args...)...)
}
