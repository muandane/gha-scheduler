package dispatch_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"k8s.io/client-go/kubernetes/fake"

	"github.com/muandane/gha-scheduler/internal/dispatch"
	"github.com/muandane/gha-scheduler/internal/ghclient"
)

func TestDispatchConcurrentOnlyOneJITCall(t *testing.T) {
	var jitCalls atomic.Int32
	gh := &countingGH{counter: &jitCalls, resp: ghclient.JITConfigResponse{
		EncodedJITConfig: "jit",
		RunnerName:       "ghs-1-2",
	}}
	k8s := fake.NewSimpleClientset()

	d := dispatch.New(dispatch.Config{
		Namespace:    "gha-runners",
		RunnerImage:  "img",
		LockIdentity: "test-locker",
	}, k8s, gh)

	req := dispatch.Request{
		Owner:         "org",
		Repo:          "repo",
		RunID:         "100",
		JobID:         "999",
		Labels:        []string{"runs-on=100", "cpu=2", "arch=x64"},
		LabelDefaults: dispatch.LabelDefaults{CPU: 2, Arch: "x64"},
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = d.Dispatch(context.Background(), req)
		}()
	}
	wg.Wait()

	if got := jitCalls.Load(); got != 1 {
		t.Fatalf("jit calls: got %d want 1", got)
	}
}

type countingGH struct {
	counter *atomic.Int32
	resp    ghclient.JITConfigResponse
}

func (c *countingGH) GenerateJITConfig(ctx context.Context, owner, repo string, req ghclient.JITConfigRequest) (ghclient.JITConfigResponse, error) {
	c.counter.Add(1)
	return c.resp, nil
}

func (c *countingGH) DeleteRunner(ctx context.Context, owner, repo string, runnerID int64) error {
	return nil
}
