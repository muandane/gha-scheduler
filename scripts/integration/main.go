// Integration dry-run: mint GH App installation token, call generate-jit-config, then delete the transient runner.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/muandane/gha-scheduler/internal/ghapp"
	"github.com/muandane/gha-scheduler/internal/ghclient"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	appID := mustInt64("GHA_APP_ID")
	installID := mustInt64("GHA_INSTALLATION_ID")
	keyPEM := mustBytes("GHA_APP_PRIVATE_KEY", "GHA_APP_PRIVATE_KEY_FILE")
	owner := envOr("GHA_TEST_OWNER", "org")
	repo := envOr("GHA_TEST_REPO", "repo")
	apiBase := envOr("GHA_API_BASE", "https://api.github.com")

	ts, err := ghapp.NewTokenSource(appID, installID, keyPEM, apiBase, nil)
	if err != nil {
		fatal("token source", err)
	}

	client := ghclient.New(apiBase, ghclient.WithTokenFunc(ts.Token))

	runnerName := fmt.Sprintf("ghs-integration-%d", time.Now().Unix())
	resp, err := client.GenerateJITConfig(ctx, owner, repo, ghclient.JITConfigRequest{
		Name:          runnerName,
		RunnerGroupID: 1,
		Labels:        []string{"self-hosted", "linux", "x64"},
	})
	if err != nil {
		fatal("generate-jit-config", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if resp.RunnerID != 0 {
			if err := client.DeleteRunner(cleanupCtx, owner, repo, resp.RunnerID); err != nil {
				fmt.Fprintf(os.Stderr, "warning: delete runner %d: %v\n", resp.RunnerID, err)
			}
		}
	}()

	if resp.EncodedJITConfig == "" {
		fmt.Fprintln(os.Stderr, "empty encoded_jit_config")
		os.Exit(1)
	}
	fmt.Printf("ok: generate-jit-config owner=%s repo=%s runner=%s runner_id=%d encoded_len=%d\n",
		owner, repo, resp.RunnerName, resp.RunnerID, len(resp.EncodedJITConfig))
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func mustInt64(key string) int64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		fmt.Fprintf(os.Stderr, "missing %s\n", key)
		os.Exit(1)
	}
	var n int64
	for _, c := range v {
		if c < '0' || c > '9' {
			fmt.Fprintf(os.Stderr, "invalid %s: %q\n", key, v)
			os.Exit(1)
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

func mustBytes(envKey, fileKey string) []byte {
	if path := strings.TrimSpace(os.Getenv(fileKey)); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			fatal(fileKey, err)
		}
		return b
	}
	v := os.Getenv(envKey)
	if v == "" {
		fmt.Fprintf(os.Stderr, "missing %s or %s\n", envKey, fileKey)
		os.Exit(1)
	}
	return []byte(v)
}

func fatal(msg string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", msg, err)
	os.Exit(1)
}
