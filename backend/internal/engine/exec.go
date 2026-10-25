package engine

import (
	"context"
	"io"

	"github.com/moby/moby/client"
)

// Terminal is where a shell runs: what the user types is read from it, the output written to
// it; Resize is told its new sizes.
type Terminal interface {
	io.ReadWriteCloser
	OnResize(func(cols, rows int))
}

// shell is bash when the container has it, else sh.
var shell = []string{"sh", "-c", "command -v bash >/dev/null && exec bash || exec sh"}

// Shell runs a shell in a running container (as root when asked), on a TTY fed by term, until
// the shell exits or the terminal closes; then the terminal is closed.
func (e *Engine) Shell(ctx context.Context, id string, root bool, term Terminal) error {
	defer term.Close()
	c, err := e.Client()
	if err != nil {
		return err
	}
	opts := client.ExecCreateOptions{TTY: true, AttachStdin: true, AttachStdout: true, AttachStderr: true, Cmd: shell,
		Env: []string{"TERM=xterm-256color"}}
	if root {
		opts.User = "root"
	}
	ex, err := c.ExecCreate(ctx, id, opts)
	if err != nil {
		return err
	}
	att, err := c.ExecAttach(ctx, ex.ID, client.ExecAttachOptions{TTY: true})
	if err != nil {
		return err
	}
	defer att.Close()
	term.OnResize(func(cols, rows int) {
		_, _ = c.ExecResize(context.WithoutCancel(ctx), ex.ID, client.ExecResizeOptions{Width: uint(cols), Height: uint(rows)})
	})
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(term, att.Reader)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(att.Conn, term)
		done <- struct{}{}
	}()
	<-done
	return nil
}
