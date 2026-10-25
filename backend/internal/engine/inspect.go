package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/docker/go-units"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type Mount struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"readOnly"`
}

type Network struct {
	Name    string   `json:"name"`
	IP      string   `json:"ip"`
	Aliases []string `json:"aliases"`
}

type Details struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Image    string    `json:"image"`
	State    string    `json:"state"`
	Health   string    `json:"health,omitempty"`
	ExitCode int       `json:"exitCode"`
	Command  string    `json:"command"`
	Created  string    `json:"created"`
	Started  string    `json:"started,omitempty"`
	Ports    []Port    `json:"ports"`
	Mounts   []Mount   `json:"mounts"`
	Networks []Network `json:"networks"`
}

func (e *Engine) Inspect(ctx context.Context, id string) (*Details, error) {
	c, err := e.Client()
	if err != nil {
		return nil, err
	}
	res, err := c.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return nil, err
	}
	in := res.Container
	det := &Details{ID: short(in.ID), Name: strings.TrimPrefix(in.Name, "/"), Command: join(append([]string{in.Path}, in.Args...)),
		Created: in.Created, Ports: []Port{}, Mounts: []Mount{}, Networks: []Network{}}
	if in.Config != nil {
		det.Image = in.Config.Image
	}
	if in.State != nil {
		det.State, det.ExitCode = string(in.State.Status), in.State.ExitCode
		if in.State.Running {
			det.Started = in.State.StartedAt
		}
		if in.State.Health != nil {
			det.Health = string(in.State.Health.Status)
		}
	}
	seen := map[Port]bool{}
	add := func(p Port) {
		if !seen[p] {
			seen[p] = true
			det.Ports = append(det.Ports, p)
		}
	}
	bound := map[string]bool{}
	if in.NetworkSettings != nil {
		for k, bindings := range in.NetworkSettings.Ports {
			for _, b := range bindings {
				ip := ""
				if b.HostIP.IsValid() && !b.HostIP.IsUnspecified() {
					ip = b.HostIP.String()
				}
				n, _ := strconv.Atoi(b.HostPort)
				add(Port{IP: ip, Published: n, Target: int(k.Num()), Protocol: string(k.Proto())})
				bound[k.String()] = true
			}
		}
	}
	if in.Config != nil {
		for k := range in.Config.ExposedPorts {
			if !bound[k.String()] {
				add(Port{Target: int(k.Num()), Protocol: string(k.Proto())})
			}
		}
	}
	sort.Slice(det.Ports, func(i, j int) bool {
		if det.Ports[i].Target != det.Ports[j].Target {
			return det.Ports[i].Target < det.Ports[j].Target
		}
		return det.Ports[i].Published < det.Ports[j].Published
	})
	for _, m := range in.Mounts {
		det.Mounts = append(det.Mounts, Mount{Type: string(m.Type), Name: m.Name, Source: m.Source, Destination: m.Destination, ReadOnly: !m.RW})
	}
	if in.NetworkSettings != nil {
		for name, n := range in.NetworkSettings.Networks {
			aliases := n.DNSNames
			if len(aliases) == 0 {
				aliases = n.Aliases
			}
			ip := ""
			if n.IPAddress.IsValid() {
				ip = n.IPAddress.String()
			}
			det.Networks = append(det.Networks, Network{Name: name, IP: ip, Aliases: append([]string{}, aliases...)})
		}
	}
	sort.Slice(det.Networks, func(i, j int) bool { return det.Networks[i].Name < det.Networks[j].Name })
	return det, nil
}

// join quotes the words of a command as a shell would read them.
func join(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		if a != "" && strings.Trim(a, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./:=@,+-") == "" {
			out[i] = a
		} else {
			out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(out, " ")
}

// Stats are the instant values of a container, formatted as docker stats does.
type Stats struct {
	CPU     string `json:"cpu"`
	Mem     string `json:"mem"`
	MemPerc string `json:"memPerc"`
	Net     string `json:"net"`
	Block   string `json:"block"`
	PIDs    string `json:"pids"`
}

// Stats returns the values of running containers by short id (12 characters).
func (e *Engine) Stats(ctx context.Context, ids []string) (map[string]Stats, error) {
	res := map[string]Stats{}
	c, err := e.Client()
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var first error
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := stats(ctx, c, id)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if first == nil {
					first = err
				}
				return
			}
			res[short(id)] = s
		}()
	}
	wg.Wait()
	if len(res) == 0 && first != nil {
		return nil, first
	}
	return res, nil
}

func stats(ctx context.Context, c *client.Client, id string) (Stats, error) {
	r, err := c.ContainerStats(ctx, id, client.ContainerStatsOptions{IncludePreviousSample: true})
	if err != nil {
		return Stats{}, err
	}
	defer r.Body.Close()
	var v container.StatsResponse
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		return Stats{}, err
	}
	return format(v), nil
}

// format computes the values as the docker command does (cache left out of the memory).
func format(v container.StatsResponse) Stats {
	cpu := 0.0
	cpuDelta := float64(v.CPUStats.CPUUsage.TotalUsage) - float64(v.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(v.CPUStats.SystemUsage) - float64(v.PreCPUStats.SystemUsage)
	online := float64(v.CPUStats.OnlineCPUs)
	if online == 0 {
		online = float64(len(v.CPUStats.CPUUsage.PercpuUsage))
	}
	if sysDelta > 0 && cpuDelta > 0 {
		cpu = cpuDelta / sysDelta * online * 100
	}
	mem := float64(v.MemoryStats.Usage)
	if x, ok := v.MemoryStats.Stats["total_inactive_file"]; ok && x < v.MemoryStats.Usage {
		mem = float64(v.MemoryStats.Usage - x)
	} else if x := v.MemoryStats.Stats["inactive_file"]; x < v.MemoryStats.Usage {
		mem = float64(v.MemoryStats.Usage - x)
	}
	limit := float64(v.MemoryStats.Limit)
	memPerc := 0.0
	if limit != 0 {
		memPerc = mem / limit * 100
	}
	var rx, tx float64
	for _, n := range v.Networks {
		rx += float64(n.RxBytes)
		tx += float64(n.TxBytes)
	}
	var rd, wr float64
	for _, b := range v.BlkioStats.IoServiceBytesRecursive {
		switch strings.ToLower(b.Op) {
		case "read":
			rd += float64(b.Value)
		case "write":
			wr += float64(b.Value)
		}
	}
	h := func(f float64) string { return units.HumanSizeWithPrecision(f, 3) }
	return Stats{
		CPU: fmt.Sprintf("%.2f%%", cpu), Mem: units.BytesSize(mem) + " / " + units.BytesSize(limit), MemPerc: fmt.Sprintf("%.2f%%", memPerc),
		Net: h(rx) + " / " + h(tx), Block: h(rd) + " / " + h(wr), PIDs: strconv.FormatUint(v.PidsStats.Current, 10),
	}
}
