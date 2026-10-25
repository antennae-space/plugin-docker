package engine

import (
	"context"
	"io"

	"github.com/docker/compose/v5/pkg/api"

	"github.com/antennae-space/plugin-docker/backend/internal/i18n"
)

func errUnknown(action string) error { return i18n.Errorf("unknown action %s", action) }

// narrow are the services an action is on: one, or all of the profiles shown.
func narrow(service string) []string {
	if service == "" {
		return nil
	}
	return []string{service}
}

// Compose runs a short Compose action (start, stop, restart) on the stack or one service.
func (e *Engine) Compose(ctx context.Context, profiles []string, action, service string) error {
	switch action {
	case "start", "stop", "restart":
	default:
		return errUnknown(action)
	}
	return e.Task(ctx, nil, profiles, action, service)
}

// Task runs a Compose action, its progress written to out as `docker compose` prints it:
// start (up -d), stop, restart, recreate (up -d --force-recreate), rebuild (up -d --build),
// pull (pull, then up -d), down, downVolumes (down -v).
func (e *Engine) Task(ctx context.Context, out io.Writer, profiles []string, action, service string) error {
	p, err := e.Project(ctx, profiles, narrow(service)...)
	if err != nil {
		return err
	}
	svc, err := e.compose(out)
	if err != nil {
		return err
	}
	services := narrow(service)
	up := func(recreate string, build *api.BuildOptions) error {
		return svc.Up(ctx, p, api.UpOptions{
			Create: api.CreateOptions{Services: services, Recreate: recreate, RecreateDependencies: api.RecreateDiverged, Inherit: true, Build: build},
			Start:  api.StartOptions{Project: p, Services: services},
		})
	}
	switch action {
	case "start":
		return up(api.RecreateDiverged, nil)
	case "stop":
		return svc.Stop(ctx, p.Name, api.StopOptions{Project: p, Services: services})
	case "restart":
		return svc.Restart(ctx, p.Name, api.RestartOptions{Project: p, Services: services})
	case "recreate":
		return up(api.RecreateForce, nil)
	case "rebuild":
		return up(api.RecreateDiverged, &api.BuildOptions{Services: services, Deps: false})
	case "pull":
		if err := svc.Pull(ctx, p, api.PullOptions{IgnoreBuildable: true}); err != nil {
			return err
		}
		return up(api.RecreateDiverged, nil)
	case "down", "downVolumes":
		return svc.Down(ctx, p.Name, api.DownOptions{Project: p, Volumes: action == "downVolumes"})
	}
	return errUnknown(action)
}
