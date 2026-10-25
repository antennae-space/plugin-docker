package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"antennae.space/plugin-sdk/backend"

	"github.com/antennae-space/plugin-docker/backend/internal/engine"
	"github.com/antennae-space/plugin-docker/backend/internal/i18n"
)

// The tools of the assistant and the MCP (contributes.toolSchemas), read-only: the state of the
// Compose services of the project and the last lines of the logs of a service or container.

// toolArgs are the arguments of a call; the profiles chosen in the tool when it names none.
type toolArgs struct {
	Service, Container, Filter string
	Lines                      int
	Profiles                   []string
}

func args(raw json.RawMessage) (toolArgs, error) {
	var a toolArgs
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &a); err != nil {
			return a, fmt.Errorf("invalid JSON arguments: %.200s", raw)
		}
	}
	if a.Profiles == nil {
		a.Profiles = savedProfiles()
	}
	return a, nil
}

func tools() map[string]backend.Tool {
	return map[string]backend.Tool{
		"docker_ps": func(ctx context.Context, raw json.RawMessage) (backend.Result, error) {
			a, err := args(raw)
			if err != nil {
				return backend.Result{}, err
			}
			return ps(ctx, a)
		},
		"docker_logs": func(ctx context.Context, raw json.RawMessage) (backend.Result, error) {
			a, err := args(raw)
			if err != nil {
				return backend.Result{}, err
			}
			return logs(ctx, a)
		},
	}
}

func plural(n int, one, other string) string {
	if n == 1 {
		return fmt.Sprintf(one, n)
	}
	return fmt.Sprintf(other, n)
}

func ps(ctx context.Context, a toolArgs) (backend.Result, error) {
	st := docker.Status(ctx)
	if !st.Available {
		msg := i18n.Translate("en", st.Err)
		return backend.Result{Content: "Docker is not reachable: " + msg, Summary: msg, Error: true}, nil
	}
	if st.ComposeFile == "" {
		list, err := docker.Containers(ctx)
		if err != nil {
			return backend.Result{}, err
		}
		var rows []string
		for _, c := range list {
			rows = append(rows, containerRow(c))
		}
		text := strings.Join(rows, "\n")
		if text == "" {
			text = "(none)"
		}
		return backend.Result{Content: "No Compose file at the project root. Containers of the host:\n" + text, Summary: plural(len(list), "%d container", "%d containers")}, nil
	}
	var active []string
	for _, p := range a.Profiles {
		for _, d := range st.Profiles {
			if p == d {
				active = append(active, p)
			}
		}
	}
	stack, err := docker.Stack(ctx, active)
	if err != nil {
		return backend.Result{}, err
	}
	running := 0
	lines := []string{}
	for _, c := range stack.Containers {
		if c.State == "running" {
			running++
		}
		lines = append(lines, containerRow(c))
	}
	for _, svc := range stack.Services {
		found := false
		for _, c := range stack.Containers {
			found = found || c.Service == svc
		}
		if !found {
			lines = append(lines, "- "+svc+": not created")
		}
	}
	head := fmt.Sprintf("Compose project \"%s\" (%s), %d/%d services running.", stack.Name, st.ComposeFile, running, len(stack.Services))
	lines = append([]string{head}, lines...)
	if len(st.Profiles) > 0 {
		act := strings.Join(active, ", ")
		if act == "" {
			act = "none"
		}
		lines = append(lines, fmt.Sprintf("Profiles declared: %s; active: %s.", strings.Join(st.Profiles, ", "), act))
	}
	return backend.Result{Content: strings.Join(lines, "\n"), Summary: fmt.Sprintf("%d/%d running", running, len(stack.Services))}, nil
}

// ansi are the escape sequences of a terminal, left out of what the model reads.
var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-_]`)

func logs(ctx context.Context, a toolArgs) (backend.Result, error) {
	service, container := strings.TrimSpace(a.Service), strings.TrimSpace(a.Container)
	if service == "" && container == "" {
		return backend.Result{Content: "service or container is missing", Error: true}, nil
	}
	lines := a.Lines
	if lines <= 0 {
		lines = 200
	}
	lines = min(lines, 2000)
	if container != "" {
		service = ""
	}
	out, err := docker.LogsTail(ctx, container, service, a.Profiles, lines)
	if err != nil {
		return backend.Result{}, err
	}
	rows := strings.Split(strings.TrimRight(ansi.ReplaceAllString(out, ""), "\n "), "\n")
	if filter := strings.ToLower(a.Filter); filter != "" {
		var kept []string
		for _, l := range rows {
			if strings.Contains(strings.ToLower(l), filter) {
				kept = append(kept, l)
			}
		}
		rows = kept
	}
	if len(rows) == 1 && rows[0] == "" {
		rows = nil
	}
	what := container
	if what == "" {
		what = service
	}
	text := strings.Join(rows, "\n")
	if text == "" {
		text = "(no output)"
	}
	f := ""
	if a.Filter != "" {
		f = fmt.Sprintf(", filtered on \"%s\"", a.Filter)
	}
	return backend.Result{Content: fmt.Sprintf("Logs of %s (last %d lines%s):\n%s", what, lines, f, text), Summary: what + " · " + plural(len(rows), "%d line", "%d lines")}, nil
}

func portLabel(p engine.Port) string {
	if p.Published == 0 {
		return fmt.Sprintf("%d/%s", p.Target, p.Protocol)
	}
	ip := ""
	if p.IP != "" {
		ip = p.IP + ":"
	}
	proto := ""
	if p.Protocol != "tcp" {
		proto = "/" + p.Protocol
	}
	return fmt.Sprintf("%s%d→%d%s", ip, p.Published, p.Target, proto)
}

func containerRow(c engine.Container) string {
	var ports []string
	for _, p := range c.Ports {
		if p.Published != 0 {
			ports = append(ports, portLabel(p))
		}
	}
	name := c.Name
	if c.Service != "" {
		name = c.Service + " (" + c.Name + ")"
	}
	state := c.Status
	if state == "" {
		state = c.State
	}
	if c.Health != "" {
		state += ", " + c.Health
	}
	row := "- " + name + ": " + state + " · " + c.Image
	if len(ports) > 0 {
		row += " · ports " + strings.Join(ports, ", ")
	}
	return row
}
