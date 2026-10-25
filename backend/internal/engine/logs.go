package engine

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strconv"
	"strings"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// Logs follows the logs of a container (id), or of the stack when id is empty (lines prefixed
// by the container of the service, "app-1 | "), timestamps included, stderr with stdout: each
// line to line, until ctx ends or Docker stops sending.
func (e *Engine) Logs(ctx context.Context, id string, profiles []string, tail int, follow bool, line func(string)) error {
	n := strconv.Itoa(tail)
	if id == "" {
		p, err := e.Project(ctx, profiles)
		if err != nil {
			return err
		}
		svc, err := e.compose(nil)
		if err != nil {
			return err
		}
		return svc.Logs(ctx, p.Name, consumer(line), api.LogOptions{Project: p, Tail: n, Follow: follow, Timestamps: true})
	}
	c, err := e.Client()
	if err != nil {
		return err
	}
	in, err := c.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return err
	}
	r, err := c.ContainerLogs(ctx, id, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Follow: follow, Tail: n, Timestamps: true})
	if err != nil {
		return err
	}
	defer r.Close()
	go func() {
		<-ctx.Done()
		r.Close()
	}()
	pr, pw := io.Pipe()
	go func() {
		var err error
		if in.Container.Config != nil && in.Container.Config.Tty {
			_, err = io.Copy(pw, r)
		} else {
			// Without a TTY, Docker multiplexes stdout and stderr: one stream of lines.
			_, err = stdcopy.StdCopy(pw, pw, r)
		}
		pw.CloseWithError(err)
	}()
	return lines(pr, line)
}

// LogsTail returns the last lines of the logs of a container, or of a service of the stack
// (service set), without following them.
func (e *Engine) LogsTail(ctx context.Context, id, service string, profiles []string, tail int) (string, error) {
	var b strings.Builder
	add := func(l string) { b.WriteString(l + "\n") }
	if service == "" {
		err := e.Logs(ctx, id, nil, tail, false, add)
		return b.String(), err
	}
	p, err := e.Project(ctx, profiles)
	if err != nil {
		return "", err
	}
	svc, err := e.compose(nil)
	if err != nil {
		return "", err
	}
	err = svc.Logs(ctx, p.Name, consumer(add).bare(), api.LogOptions{Project: p, Services: []string{service}, Tail: strconv.Itoa(tail), Timestamps: true})
	return b.String(), err
}

// lineMax caps a line of logs.
const lineMax = 16 << 10

func lines(r io.Reader, line func(string)) error {
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		l, err := br.ReadString('\n')
		if l != "" {
			l = strings.TrimRight(l, "\r\n")
			if len(l) > lineMax {
				l = l[:lineMax] + "…"
			}
			line(strings.ToValidUTF8(l, "�"))
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// consumer gives the lines of Compose, each prefixed by its container ("app-1 | ").
type consumer func(string)

func (f consumer) Log(name, msg string) { f.each(name, msg) }
func (f consumer) Err(name, msg string) { f.each(name, msg) }
func (consumer) Status(string, string)  {}

func (f consumer) each(name, msg string) {
	for _, l := range bytes.Split([]byte(strings.TrimRight(msg, "\r\n")), []byte("\n")) {
		s := strings.TrimRight(string(l), "\r")
		if len(s) > lineMax {
			s = s[:lineMax] + "…"
		}
		if name != "" {
			s = name + " | " + s
		}
		f(strings.ToValidUTF8(s, "�"))
	}
}

// bare is the consumer without the prefix (the logs of one service).
func (f consumer) bare() consumer {
	return func(s string) {
		if _, rest, ok := strings.Cut(s, " | "); ok {
			s = rest
		}
		f(s)
	}
}
