package engine

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
)

func TestParseDotEnv(t *testing.T) {
	env, err := parseDotEnv("# comment\nA=1\nexport B = \"two words\"\nC='x'\n\n")
	if err != nil || !reflect.DeepEqual(env, map[string]string{"A": "1", "B": "two words", "C": "x"}) {
		t.Fatalf("env = %v, %v", env, err)
	}
	if _, err := parseDotEnv("nope"); err == nil {
		t.Fatal("a line without = is refused")
	}
}

func TestConsumer(t *testing.T) {
	var got []string
	c := consumer(func(s string) { got = append(got, s) })
	c.Log("app-1", "one\ntwo\n")
	c.bare().Err("db-1", "three")
	if !reflect.DeepEqual(got, []string{"app-1 | one", "app-1 | two", "three"}) {
		t.Fatalf("lines = %q", got)
	}
}

func TestFormat(t *testing.T) {
	var v container.StatsResponse
	v.CPUStats.CPUUsage.TotalUsage, v.PreCPUStats.CPUUsage.TotalUsage = 300, 100
	v.CPUStats.SystemUsage, v.PreCPUStats.SystemUsage = 2000, 1000
	v.CPUStats.OnlineCPUs = 2
	v.MemoryStats.Usage, v.MemoryStats.Limit = 300<<20, 1<<30
	v.MemoryStats.Stats = map[string]uint64{"inactive_file": 100 << 20}
	v.Networks = map[string]container.NetworkStats{"eth0": {RxBytes: 1500, TxBytes: 0}}
	v.PidsStats.Current = 7
	s := format(v)
	want := Stats{CPU: "40.00%", Mem: "200MiB / 1GiB", MemPerc: "19.53%", Net: "1.5kB / 0B", Block: "0B / 0B", PIDs: "7"}
	if s != want {
		t.Fatalf("stats = %+v", s)
	}
}

func TestJoin(t *testing.T) {
	if got := join([]string{"sh", "-c", "echo 'hi' $X"}); got != `sh -c 'echo '\''hi'\'' $X'` {
		t.Fatalf("join = %s", got)
	}
}

// TestDocker runs a stack of the local Docker (skipped without it or without busybox): Compose
// up, its containers listed with the labels of docker compose, logs, a shell, down.
func TestDocker(t *testing.T) {
	sock := "/var/run/docker.sock"
	if _, err := os.Stat(sock); err != nil {
		t.Skip("no Docker")
	}
	e := &Engine{Dial: func(ctx context.Context) (net.Conn, error) { return (&net.Dialer{}).DialContext(ctx, "unix", sock) }, Files: LocalFiles{}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c, err := e.Client()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ImageInspect(ctx, "busybox:latest"); err != nil {
		t.Skip("no busybox image")
	}
	e.Root = t.TempDir()
	name := fmt.Sprintf("plugin-docker-test-%d", os.Getpid())
	compose := "name: " + name + "\nservices:\n  app:\n    image: busybox:latest\n    command: [\"sh\", \"-c\", \"echo hello; echo oops >&2; sleep 600\"]\n  extra:\n    image: busybox:latest\n    profiles: [debug]\n"
	if err := os.WriteFile(filepath.Join(e.Root, "compose.yaml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Task(context.Background(), nil, nil, "downVolumes", "") }()

	st := e.Status(ctx)
	if !st.Available || st.ComposeFile != "compose.yaml" || !reflect.DeepEqual(st.Profiles, []string{"debug"}) {
		t.Fatalf("status = %+v", st)
	}
	var out strings.Builder
	if err := e.Task(ctx, &out, nil, "start", ""); err != nil {
		t.Fatal(err, out.String())
	}
	stack, err := e.Stack(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stack.Name != name || !reflect.DeepEqual(stack.Services, []string{"app"}) || len(stack.Containers) != 1 || stack.Containers[0].State != "running" {
		t.Fatalf("stack = %+v", stack)
	}
	id := stack.Containers[0].ID
	time.Sleep(500 * time.Millisecond)
	logs, err := e.LogsTail(ctx, id, "", nil, 10)
	if err != nil || !strings.Contains(logs, "hello") || !strings.Contains(logs, "oops") {
		t.Fatalf("logs = %q, %v", logs, err)
	}
	logs, err = e.LogsTail(ctx, "", "app", nil, 10)
	if err != nil || !strings.Contains(logs, "hello") || strings.Contains(logs, " | ") {
		t.Fatalf("service logs = %q, %v", logs, err)
	}
	if err := e.Compose(ctx, nil, "stop", "app"); err != nil {
		t.Fatal(err)
	}
	if d, err := e.Inspect(ctx, id); err != nil || d.State != "exited" {
		t.Fatalf("after stop = %+v, %v", d, err)
	}
}
