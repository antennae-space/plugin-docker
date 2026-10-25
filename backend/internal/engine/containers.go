package engine

import (
	"context"
	"sort"
	"strings"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// Status tells what the tool can do: Docker reachable, the Compose file and its profiles.
// Compose is the plugin's own: always there when Docker is.
type Status struct {
	Available   bool     `json:"available"`
	Error       string   `json:"error,omitempty"`
	Err         error    `json:"-"` // translated into Error for the window
	Compose     bool     `json:"compose"`
	ComposeFile string   `json:"composeFile,omitempty"`
	Profiles    []string `json:"profiles,omitempty"`
	Version     string   `json:"version,omitempty"`
}

func (e *Engine) Status(ctx context.Context) Status {
	var st Status
	st.ComposeFile, _ = e.ComposeFile(ctx)
	c, err := e.Client()
	if err == nil {
		var v client.ServerVersionResult
		v, err = c.ServerVersion(ctx, client.ServerVersionOptions{})
		st.Version = v.Version
	}
	if err != nil {
		st.Err = err
		return st
	}
	st.Available, st.Compose = true, true
	if st.ComposeFile != "" {
		// Every profile declared: the project loaded with all of them.
		if p, err := e.Project(ctx, []string{"*"}); err == nil {
			for _, s := range p.Services {
				for _, pr := range s.Profiles {
					if !contains(st.Profiles, pr) {
						st.Profiles = append(st.Profiles, pr)
					}
				}
			}
			sort.Strings(st.Profiles)
		}
	}
	return st
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

type Port struct {
	IP        string `json:"ip,omitempty"`
	Published int    `json:"published,omitempty"`
	Target    int    `json:"target"`
	Protocol  string `json:"protocol"`
}

type Container struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"`  // created, running, paused, restarting, exited, dead
	Status  string `json:"status"` // "Up 2 minutes (healthy)"
	Health  string `json:"health,omitempty"`
	Project string `json:"project,omitempty"`
	Service string `json:"service,omitempty"`
	Ports   []Port `json:"ports"`
}

// Stack is the Compose project of the project root: declared services and their containers.
type Stack struct {
	Name       string      `json:"name"`
	Services   []string    `json:"services"`
	Containers []Container `json:"containers"`
}

func (e *Engine) Stack(ctx context.Context, profiles []string) (*Stack, error) {
	p, err := e.Project(ctx, profiles)
	if err != nil {
		return nil, err
	}
	st := &Stack{Name: p.Name, Services: p.ServiceNames(), Containers: []Container{}}
	sort.Strings(st.Services)
	list, err := e.list(ctx, client.Filters{}.Add("label", api.ProjectLabel+"="+p.Name))
	if err != nil {
		return nil, err
	}
	for _, c := range list {
		// The containers of the services of the profiles shown, one-off ones (run) aside.
		if contains(st.Services, c.Service) {
			st.Containers = append(st.Containers, c)
		}
	}
	sort.Slice(st.Containers, func(i, j int) bool { return st.Containers[i].Name < st.Containers[j].Name })
	return st, nil
}

// Containers lists every container of the host, by Compose project then name.
func (e *Engine) Containers(ctx context.Context) ([]Container, error) {
	list, err := e.list(ctx, nil)
	if err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Project != list[j].Project {
			return list[i].Project < list[j].Project
		}
		return list[i].Name < list[j].Name
	})
	return list, nil
}

func (e *Engine) list(ctx context.Context, f client.Filters) ([]Container, error) {
	c, err := e.Client()
	if err != nil {
		return nil, err
	}
	res, err := c.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: f})
	if err != nil {
		return nil, err
	}
	out := []Container{}
	for _, s := range res.Items {
		if s.Labels[api.OneoffLabel] == "True" {
			continue
		}
		out = append(out, summary(s))
	}
	return out, nil
}

func short(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func summary(s container.Summary) Container {
	name := ""
	if len(s.Names) > 0 {
		name = strings.TrimPrefix(s.Names[0], "/")
	}
	c := Container{ID: short(s.ID), Name: name, Image: s.Image, State: string(s.State), Status: s.Status,
		Project: s.Labels[api.ProjectLabel], Service: s.Labels[api.ServiceLabel], Ports: []Port{}}
	if s.Health != nil && s.Health.Status != container.NoHealthcheck {
		c.Health = string(s.Health.Status)
	}
	seen := map[Port]bool{}
	for _, p := range s.Ports {
		port := Port{Published: int(p.PublicPort), Target: int(p.PrivatePort), Protocol: p.Type}
		// IPv4 and IPv6 bindings of one port show once.
		if p.IP.IsValid() && !p.IP.IsUnspecified() {
			port.IP = p.IP.String()
		}
		if !seen[port] {
			seen[port] = true
			c.Ports = append(c.Ports, port)
		}
	}
	sort.Slice(c.Ports, func(i, j int) bool {
		if c.Ports[i].Target != c.Ports[j].Target {
			return c.Ports[i].Target < c.Ports[j].Target
		}
		return c.Ports[i].Published < c.Ports[j].Published
	})
	return c
}

// Container runs an action on one container of the host.
func (e *Engine) Container(ctx context.Context, action, id string) error {
	c, err := e.Client()
	if err != nil {
		return err
	}
	switch action {
	case "start":
		_, err = c.ContainerStart(ctx, id, client.ContainerStartOptions{})
	case "stop":
		_, err = c.ContainerStop(ctx, id, client.ContainerStopOptions{})
	case "restart":
		_, err = c.ContainerRestart(ctx, id, client.ContainerRestartOptions{})
	case "remove":
		_, err = c.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
	default:
		return errUnknown(action)
	}
	return err
}
