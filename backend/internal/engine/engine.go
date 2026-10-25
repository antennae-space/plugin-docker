// Package engine drives Docker for the plugin: the Engine API through the socket the IDE opens
// (the backend reaches no socket itself), and Compose through its Go library, not the docker
// command: the containers it makes carry the labels of `docker compose`, so both see the same
// stack.
package engine

import (
	"context"
	"io"
	"net"
	"sync"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/cmd/display"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/compose"
	"github.com/moby/moby/client"
)

// Engine is the Docker of one project: its host's, reached by Dial.
type Engine struct {
	// Dial opens a connection to the Engine API (backend.Socket "docker").
	Dial func(ctx context.Context) (net.Conn, error)
	// Root is the folder of the project on its host; Files reads its Compose files.
	Root  string
	Files Files

	once   sync.Once
	client *client.Client
	err    error
}

// Client is the Engine API client, made once.
func (e *Engine) Client() (*client.Client, error) {
	e.once.Do(func() {
		e.client, e.err = client.New(
			client.WithHost("http://docker"),
			client.WithDialContext(func(ctx context.Context, _, _ string) (net.Conn, error) { return e.Dial(ctx) }),
			client.WithAPIVersionNegotiation(),
		)
	})
	return e.client, e.err
}

// compose is a Compose service writing its progress to out (nil: nowhere).
func (e *Engine) compose(out io.Writer) (api.Compose, error) {
	c, err := e.Client()
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = io.Discard
	}
	cli, err := command.NewDockerCli(command.WithCombinedStreams(out), command.WithInputStream(eof{}))
	if err != nil {
		return nil, err
	}
	if err := cli.Initialize(flags.NewClientOptions(), command.WithAPIClient(c)); err != nil {
		return nil, err
	}
	return compose.NewComposeService(cli, compose.WithPrompt(compose.AlwaysOkPrompt()), compose.WithEventProcessor(display.Plain(out)))
}

// eof is an empty input: Compose never reads the user.
type eof struct{}

func (eof) Read([]byte) (int, error) { return 0, io.EOF }
func (eof) Close() error             { return nil }
