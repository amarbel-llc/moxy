package hook

import (
	"context"
	"net"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"code.linenisgreat.com/moxy/pkg/perms"
	"code.linenisgreat.com/moxy/pkg/policybroker"
	"code.linenisgreat.com/moxy/pkg/policybroker/fakebroker"
)

// The hook is a fresh process per tool call, so its broker cost is a new
// client's dial, hello wait and one check. These benchmarks run exactly that
// (a new hookGate per iteration) against a real unix socket and report p50 and
// p99 in milliseconds; run them with `just debug-bench-hook-dial` (moxy#493).
// Process start and moxin discovery are not included.

func benchHookGate(b *testing.B, addr string) {
	b.Setenv(policybroker.EnvAddr, addr)
	b.Setenv(policybroker.EnvSessionKey, "")
	res := perms.Resource{Kind: perms.KindFile, URI: "file:///outside/tree"}
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g := brokerGate()
		start := time.Now()
		g.Check(context.Background(), perms.Read, res)
		durations = append(durations, time.Since(start))
		b.StopTimer()
		g.close()
		b.StartTimer()
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	ms := func(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
	b.ReportMetric(ms(durations[len(durations)/2]), "p50-ms")
	b.ReportMetric(ms(durations[len(durations)*99/100]), "p99-ms")
}

func BenchmarkHookGate_Connected(b *testing.B) {
	f := fakebroker.New(b, "sess")
	benchHookGate(b, f.Addr())
}

// ENOENT: the socket path does not exist.
func BenchmarkHookGate_NoSocketFile(b *testing.B) {
	benchHookGate(b, filepath.Join(b.TempDir(), "absent.sock"))
}

// Refused: the socket file exists but nothing listens.
func BenchmarkHookGate_Refused(b *testing.B) {
	addr := filepath.Join(b.TempDir(), "dead.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: addr, Net: "unix"})
	if err != nil {
		b.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	_ = ln.Close()
	benchHookGate(b, addr)
}

// Hung: the broker accepts but never says hello.
func BenchmarkHookGate_NoHello(b *testing.B) {
	f := fakebroker.New(b, "sess")
	f.Hello(func(policybroker.Message) *policybroker.Message { return nil })
	benchHookGate(b, f.Addr())
}

// HungCheck: hello completes but the broker never answers the check, so the
// 100 ms deadline is the cost.
func BenchmarkHookGate_NoAnswer(b *testing.B) {
	f := fakebroker.New(b, "sess")
	f.Answer(func(policybroker.Message) fakebroker.Answer { return fakebroker.Answer{} })
	benchHookGate(b, f.Addr())
}
