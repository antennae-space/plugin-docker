package engine

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"

	"github.com/antennae-space/plugin-docker/backend/internal/i18n"
)

// ComposeFiles are the names Compose looks for, in its order.
var ComposeFiles = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// Files reads the files of the project: on the disk of the pod for a local project, through
// the IDE (fs.read) for a project over SSH.
type Files interface {
	// Read returns the content of a file of the project, or os.ErrNotExist.
	Read(ctx context.Context, p string) ([]byte, error)
	// Local tells whether the paths are the pod's own (Compose reads them itself).
	Local() bool
}

// LocalFiles are the files of a local project.
type LocalFiles struct{}

func (LocalFiles) Read(_ context.Context, p string) ([]byte, error) { return os.ReadFile(p) }
func (LocalFiles) Local() bool                                      { return true }

// ComposeFile is the Compose file at the root of the project ("" for none) and its content.
func (e *Engine) ComposeFile(ctx context.Context) (string, []byte) {
	for _, f := range ComposeFiles {
		if data, err := e.Files.Read(ctx, path.Join(e.Root, f)); err == nil {
			return f, data
		}
	}
	return "", nil
}

// ErrNoCompose: the project has no Compose file at its root.
var ErrNoCompose = i18n.New("no Compose file at the root of the project")

// Project loads the Compose project of the root with these profiles, narrowed to services
// (and their dependencies) when given. A local project is loaded
// as `docker compose` does it (.env, env_file, include, extends); over SSH, the Compose file
// and its .env are read through the IDE, and the files they name are not reached.
func (e *Engine) Project(ctx context.Context, profiles []string, services ...string) (*types.Project, error) {
	name, data := e.ComposeFile(ctx)
	if name == "" {
		return nil, ErrNoCompose
	}
	file := path.Join(e.Root, name)
	opts := api.ProjectLoadOptions{WorkingDir: e.Root, ConfigPaths: []string{file}, Profiles: profiles, Services: services, Offline: true}
	if !e.Files.Local() {
		env := map[string]string{}
		if dot, err := e.Files.Read(ctx, path.Join(e.Root, ".env")); err == nil {
			env, _ = parseDotEnv(string(dot))
		}
		opts.ProjectOptionsFns = []cli.ProjectOptionsFn{
			cli.WithEnv(envList(env)),
			func(o *cli.ProjectOptions) error {
				o.ResourceLoaders = append([]loader.ResourceLoader{remoteFiles{e, file, data}}, o.ResourceLoaders...)
				return nil
			},
		}
	}
	svc, err := e.compose(nil)
	if err != nil {
		return nil, err
	}
	return svc.LoadProject(ctx, opts)
}

// remoteFiles gives Compose the file of an SSH project it was read from, and refuses the
// others (include, extends of another file).
type remoteFiles struct {
	e    *Engine
	file string
	data []byte
}

func (r remoteFiles) Accept(string) bool { return true }

func (r remoteFiles) Load(_ context.Context, p string) (string, error) {
	if p != r.file && p != filepath.Base(r.file) {
		return "", i18n.Errorf("%s: on a project over SSH, the Compose file cannot include or extend another file", p)
	}
	tmp, err := os.CreateTemp("", "compose-*.yaml")
	if err != nil {
		return "", err
	}
	defer tmp.Close()
	_, err = tmp.Write(r.data)
	return tmp.Name(), err
}

func (r remoteFiles) Dir(p string) string { return path.Dir(p) }

func envList(env map[string]string) []string {
	var l []string
	for k, v := range env {
		l = append(l, k+"="+v)
	}
	return l
}

// parseDotEnv reads KEY=value lines (# comments, optional quotes).
func parseDotEnv(s string) (map[string]string, error) {
	env := map[string]string{}
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		k, v, ok := strings.Cut(strings.TrimPrefix(l, "export "), "=")
		if !ok {
			return env, errors.New("invalid line in .env: " + l)
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		env[strings.TrimSpace(k)] = v
	}
	return env, nil
}
