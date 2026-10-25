// The backend of the Docker tool: the IDE starts it confined, one process per project, and
// speaks JSON-RPC to it (antennae.space/plugin-sdk/backend). It reaches Docker only through the
// socket the IDE opens for it (backend.Socket, the grant socket:docker: as much as root on the
// host), runs Compose as a library, and feeds consoles of the IDE (backend.OpenTerminal) with
// the shells of containers and the progress of long Compose actions.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"

	"antennae.space/plugin-sdk/backend"

	"github.com/antennae-space/plugin-docker/backend/internal/engine"
	"github.com/antennae-space/plugin-docker/backend/internal/i18n"
)

var (
	docker *engine.Engine
	// base is the context of initialize: it reaches the IDE for the life of the process (the
	// shells, the log streams, the tasks outlive the call that started them).
	base context.Context
	// project keys the data of the project (its profiles): the main project of a worktree.
	project string
	home    string
)

func main() {
	backend.Serve(backend.Plugin{
		Init:     start,
		Methods:  methods(),
		Tools:    tools(),
		Shutdown: stopStreams,
	})
}

func start(ctx context.Context, info backend.Info) error {
	if exe, err := os.Executable(); err == nil {
		i18n.Load(filepath.Join(filepath.Dir(exe), "..", "i18n"))
	}
	var err error
	if home, err = os.UserHomeDir(); err != nil {
		return err
	}
	base = ctx
	project = info.Project.Main
	if project == "" {
		project = info.Project.ID
	}
	var files engine.Files = engine.LocalFiles{}
	if !info.Project.Local {
		files = remoteFiles{}
	}
	docker = &engine.Engine{
		Dial:  func(context.Context) (net.Conn, error) { return backend.Socket(base, "docker") },
		Root:  info.Project.Root,
		Files: files,
	}
	return nil
}

// remoteFiles reads the files of a project over SSH through the IDE (core:fs.read); a file
// it cannot read is one Compose does not find.
type remoteFiles struct{}

func (remoteFiles) Local() bool { return false }

func (remoteFiles) Read(ctx context.Context, p string) ([]byte, error) {
	var r struct {
		Content string `json:"content"`
		Binary  bool   `json:"binary"`
	}
	if err := backend.Core(base, "fs.read", map[string]string{"path": p}, &r); err != nil {
		return nil, err
	}
	if r.Binary {
		return nil, errors.New(p + ": not a text file")
	}
	return []byte(r.Content), nil
}

// method makes a method of the plugin: its arguments decoded into A, its errors in the
// language of the window.
func method[A any](f func(ctx context.Context, a A) (any, error)) backend.Method {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var a A
		if len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &a); err != nil {
				return nil, err
			}
		}
		res, err := f(ctx, a)
		if err == nil {
			return res, nil
		}
		if backend.CodeOf(err) != "" {
			return nil, err
		}
		return nil, errors.New(i18n.Translate(backend.LangOf(ctx), err))
	}
}

type none struct{}

func methods() map[string]backend.Method {
	type profilesArg struct {
		Profiles []string `json:"profiles"`
	}
	type idArg struct {
		ID string `json:"id"`
	}
	return map[string]backend.Method{
		"docker.status": method(func(ctx context.Context, _ none) (any, error) {
			st := docker.Status(ctx)
			if st.Err != nil {
				st.Error = i18n.Translate(backend.LangOf(ctx), st.Err)
			}
			return st, nil
		}),
		"docker.stack": method(func(ctx context.Context, a profilesArg) (any, error) {
			return docker.Stack(ctx, a.Profiles)
		}),
		"docker.containers": method(func(ctx context.Context, _ none) (any, error) {
			return docker.Containers(ctx)
		}),
		"docker.inspect": method(func(ctx context.Context, a idArg) (any, error) {
			return docker.Inspect(ctx, a.ID)
		}),
		"docker.stats": method(func(ctx context.Context, a struct {
			IDs []string `json:"ids"`
		}) (any, error) {
			return docker.Stats(ctx, a.IDs)
		}),
		"docker.compose": method(func(ctx context.Context, a struct {
			Profiles []string `json:"profiles"`
			Action   string   `json:"action"`
			Service  string   `json:"service"`
		}) (any, error) {
			return nil, docker.Compose(ctx, a.Profiles, a.Action, a.Service)
		}),
		"docker.task": method(func(ctx context.Context, a struct {
			Profiles []string `json:"profiles"`
			Kind     string   `json:"kind"`
			Service  string   `json:"service"`
		}) (any, error) {
			return task(ctx, a.Profiles, a.Kind, a.Service)
		}),
		"docker.container": method(func(ctx context.Context, a struct {
			Action string `json:"action"`
			ID     string `json:"id"`
		}) (any, error) {
			return nil, docker.Container(ctx, a.Action, a.ID)
		}),
		"docker.shell": method(func(ctx context.Context, a struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Root  bool   `json:"root"`
		}) (any, error) {
			return shell(ctx, a.ID, a.Title, a.Root)
		}),
		"docker.follow": method(func(ctx context.Context, a struct {
			ID       string   `json:"id"`
			Profiles []string `json:"profiles"`
			Tail     int      `json:"tail"`
		}) (any, error) {
			if a.Tail <= 0 {
				a.Tail = 500
			}
			return map[string]string{"stream": follow(a.ID, a.Profiles, a.Tail)}, nil
		}),
		"docker.followStop": method(func(ctx context.Context, a struct {
			Stream string `json:"stream"`
		}) (any, error) {
			stopStream(a.Stream)
			return nil, nil
		}),
		"docker.logsTail": method(func(ctx context.Context, a struct {
			ID       string   `json:"id"`
			Service  string   `json:"service"`
			Profiles []string `json:"profiles"`
			Lines    int      `json:"lines"`
		}) (any, error) {
			if a.ID == "" && a.Service == "" {
				return nil, i18n.New("a container or a service is needed")
			}
			out, err := docker.LogsTail(ctx, a.ID, a.Service, a.Profiles, min(max(a.Lines, 1), 2000))
			return map[string]string{"output": out}, err
		}),
		"docker.disk": method(func(ctx context.Context, _ none) (any, error) {
			return docker.Disk(ctx)
		}),
		"docker.prune": method(func(ctx context.Context, a struct {
			What string `json:"what"`
		}) (any, error) {
			freed, err := docker.Prune(ctx, a.What)
			return map[string]string{"reclaimed": freed}, err
		}),
		"docker.remove": method(func(ctx context.Context, a struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		}) (any, error) {
			return nil, docker.RemoveObject(ctx, a.Kind, a.ID)
		}),
		"docker.profiles": method(func(ctx context.Context, _ none) (any, error) {
			return map[string][]string{"profiles": savedProfiles()}, nil
		}),
		"docker.setProfiles": method(func(ctx context.Context, a profilesArg) (any, error) {
			return nil, saveProfiles(a.Profiles)
		}),
	}
}
