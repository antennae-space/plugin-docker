package main

import (
	"context"
	"strconv"
	"sync"
	"time"

	"antennae.space/plugin-sdk/backend"
)

// The log streams the views follow: lines sent to the windows of the project as docker.log
// events {stream, lines}, in batches (every 100 ms or 500 lines), then docker.logEnd when Docker
// stops sending. A view stops its stream (followStop); a window gone without stopping it leaves
// it running until the backend stops, so at most maxStreams run: a new one stops the oldest.
const (
	logBatch   = 500
	maxStreams = 8
)

var (
	streamMu  sync.Mutex
	streams   = map[string]context.CancelFunc{}
	order     []string
	streamSeq int
)

func follow(id string, profiles []string, tail int) string {
	ctx, cancel := context.WithCancel(base)
	streamMu.Lock()
	streamSeq++
	sid := "l" + strconv.Itoa(streamSeq)
	streams[sid] = cancel
	order = append(order, sid)
	var oldest string
	if len(order) > maxStreams {
		oldest = order[0]
	}
	streamMu.Unlock()
	if oldest != "" {
		stopStream(oldest)
	}
	go pump(ctx, sid, id, profiles, tail)
	return sid
}

func stopStream(sid string) {
	streamMu.Lock()
	cancel := streams[sid]
	delete(streams, sid)
	for i, s := range order {
		if s == sid {
			order = append(order[:i], order[i+1:]...)
			break
		}
	}
	streamMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func stopStreams() {
	streamMu.Lock()
	ids := append([]string(nil), order...)
	streamMu.Unlock()
	for _, sid := range ids {
		stopStream(sid)
	}
}

func pump(ctx context.Context, sid, id string, profiles []string, tail int) {
	lines := make(chan string, logBatch)
	go func() {
		defer close(lines)
		_ = docker.Logs(ctx, id, profiles, tail, true, func(l string) {
			select {
			case lines <- l:
			case <-ctx.Done():
			}
		})
	}()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	var batch []string
	flush := func() {
		if len(batch) > 0 {
			_ = backend.Emit(base, "docker.log", map[string]any{"stream": sid, "lines": batch})
			batch = nil
		}
	}
	defer func() {
		stopStream(sid)
		_ = backend.Emit(base, "docker.logEnd", map[string]string{"stream": sid})
	}()
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				flush()
				return
			}
			batch = append(batch, l)
			if len(batch) >= logBatch {
				flush()
			}
		case <-tick.C:
			flush()
		}
	}
}
