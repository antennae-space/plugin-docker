package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"

	"antennae.space/plugin-sdk/backend"

	"github.com/antennae-space/plugin-docker/backend/internal/i18n"
)

// The consoles the plugin feeds: a shell in a container, the progress of a long Compose action.
// The IDE draws them (the grant terminal); the views show them with ide.showConsole.

// terminal is a console of the IDE whose last size is kept: a resize told before the shell
// listens is given to it then.
type terminal struct {
	*backend.Terminal
	mu         sync.Mutex
	cols, rows int
	resize     func(cols, rows int)
}

func openTerminal(title string) (*terminal, error) {
	t, err := backend.OpenTerminal(base, title)
	if err != nil {
		return nil, err
	}
	term := &terminal{Terminal: t}
	t.OnResize(func(cols, rows int) {
		term.mu.Lock()
		term.cols, term.rows = cols, rows
		f := term.resize
		term.mu.Unlock()
		if f != nil {
			f(cols, rows)
		}
	})
	return term, nil
}

func (t *terminal) OnResize(f func(cols, rows int)) {
	t.mu.Lock()
	t.resize = f
	cols, rows := t.cols, t.rows
	t.mu.Unlock()
	if cols > 0 {
		f(cols, rows)
	}
}

// shell opens a console running a shell in the container id.
func shell(ctx context.Context, id, title string, root bool) (any, error) {
	if title == "" {
		title = id
	}
	if root {
		title += " (root)"
	}
	term, err := openTerminal(title)
	if err != nil {
		return nil, err
	}
	go func() {
		if err := docker.Shell(base, id, root, term); err != nil {
			_, _ = io.WriteString(term, "\r\n\x1b[31m"+i18n.Translate(backend.LangOf(ctx), err)+"\x1b[0m\r\n")
		}
	}()
	return map[string]string{"console": term.ID}, nil
}

// taskArgs are the words of the docker compose command a task does, for its title.
var taskArgs = map[string][]string{
	"recreate":    {"up", "-d", "--force-recreate"},
	"rebuild":     {"up", "-d", "--build"},
	"pull":        {"pull"},
	"down":        {"down"},
	"downVolumes": {"down", "-v"},
}

// task runs a long Compose action in a console showing its progress, and returns the console.
func task(ctx context.Context, profiles []string, kind, service string) (any, error) {
	words, ok := taskArgs[kind]
	if !ok {
		return nil, i18n.Errorf("unknown action %s", kind)
	}
	title := []string{"compose"}
	for _, p := range profiles {
		title = append(title, "--profile", p)
	}
	title = append(title, words...)
	if service != "" && kind != "down" && kind != "downVolumes" {
		title = append(title, service)
	}
	term, err := openTerminal(strings.Join(title, " "))
	if err != nil {
		return nil, err
	}
	lang := backend.LangOf(ctx)
	go func() {
		defer term.Close()
		out := crlf{term}
		err := docker.Task(base, out, profiles, kind, service)
		if err != nil {
			_, _ = io.WriteString(out, "\n\x1b[31m"+i18n.Translate(lang, err)+"\x1b[0m\n")
			return
		}
		_, _ = io.WriteString(out, "\n\x1b[32m"+i18n.T(lang, "Done.")+"\x1b[0m\n")
	}()
	return map[string]string{"console": term.ID}, nil
}

// crlf writes lines to a terminal: each \n a \r\n.
type crlf struct{ w io.Writer }

func (c crlf) Write(b []byte) (int, error) {
	if _, err := c.w.Write(bytes.ReplaceAll(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")), []byte("\n"), []byte("\r\n"))); err != nil {
		return 0, err
	}
	return len(b), nil
}
