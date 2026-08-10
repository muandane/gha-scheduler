package dispatch

import (
	"context"
	"fmt"
	"log/slog"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/muandane/gha-scheduler/internal/ghclient"
	"github.com/muandane/gha-scheduler/internal/k8sjob"
	"github.com/muandane/gha-scheduler/internal/labelquery"
)

// GHClient generates JIT runner configuration and cleans up orphan registrations.
type GHClient interface {
	GenerateJITConfig(ctx context.Context, owner, repo string, req ghclient.JITConfigRequest) (ghclient.JITConfigResponse, error)
	DeleteRunner(ctx context.Context, owner, repo string, runnerID int64) error
}

// LabelDefaults are passed to labelquery.Parse.
type LabelDefaults struct {
	CPU  int
	Arch string
}

// Config holds dispatcher defaults.
type Config struct {
	Namespace           string
	RunnerImage         string
	CacheImage          string
	CachePort           int32
	MemPerCPU           string
	ArchNodeLabel       string
	PoolNodeLabel       string
	CacheBackend        string
	S3Endpoint          string
	S3Bucket            string
	S3Region            string
	S3SecretName        string
	SpotTolerationKey   string
	SpotTolerationValue string
	LockIdentity        string
	MaxAttempts         int
	RunnerGroupID       int64
	LabelWarn           labelquery.WarnFunc
	OnParsed            func(ctx context.Context, req Request, spec labelquery.RunnerSpec)
	OnJobCreated        func(jobID string)
	OnError             func(ctx context.Context, req Request, reason string)
}

// Request is one workflow_job queued dispatch.
type Request struct {
	Owner         string
	Repo          string
	RunID         string
	JobID         string
	Labels        []string
	LabelDefaults LabelDefaults
}

// Dispatcher wires label parsing, GH JIT config, and k8s Job creation.
type Dispatcher struct {
	cfg     Config
	k8s     kubernetes.Interface
	gh      GHClient
	lock    JobLocker
	jobMu   *jobMutexRegistry
	wait    waitFunc
	backoff *Backoff
}

// New creates a Dispatcher.
func New(cfg Config, k8s kubernetes.Interface, gh GHClient) *Dispatcher {
	if cfg.ArchNodeLabel == "" {
		cfg.ArchNodeLabel = "kubernetes.io/arch"
	}
	if cfg.PoolNodeLabel == "" {
		cfg.PoolNodeLabel = "pool"
	}
	if cfg.MemPerCPU == "" {
		cfg.MemPerCPU = "2Gi"
	}
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = defaultMaxAttempts
	}
	if cfg.RunnerGroupID == 0 {
		cfg.RunnerGroupID = 1
	}
	return &Dispatcher{
		cfg:     cfg,
		k8s:     k8s,
		gh:      gh,
		lock:    NewLeaseLocker(k8s, cfg.Namespace, cfg.LockIdentity, 0),
		jobMu:   newJobMutexRegistry(),
		backoff: newBackoff(),
	}
}

// SetLocker overrides the job lock implementation (tests).
func (d *Dispatcher) SetLocker(lock JobLocker) {
	d.lock = lock
}

// SetWait overrides retry backoff for tests.
func (d *Dispatcher) SetWait(wait waitFunc) {
	d.wait = wait
}

// Dispatch creates a runner Job for the request unless one already exists.
func (d *Dispatcher) Dispatch(ctx context.Context, req Request) error {
	if !labelquery.Managed(req.Labels) {
		slog.Debug("dispatch: skip unmanaged labels", "job_id", req.JobID, "labels", req.Labels)
		return nil
	}

	defaults := labelquery.Defaults{
		CPU:  req.LabelDefaults.CPU,
		Arch: req.LabelDefaults.Arch,
	}
	spec, err := labelquery.Parse(req.Labels, defaults, d.cfg.LabelWarn)
	if err != nil {
		slog.Error("dispatch: parse labels failed", "labels", req.Labels, "err", err)
		if d.cfg.OnError != nil {
			d.cfg.OnError(ctx, req, "parse_labels")
		}
		return fmt.Errorf("dispatch: parse labels: %w", err)
	}

	if d.cfg.OnParsed != nil {
		d.cfg.OnParsed(ctx, req, spec)
	}

	if dup, err := d.isDuplicate(ctx, req.JobID); err != nil {
		return err
	} else if dup {
		return nil
	}

	unlockJob := d.jobMu.acquire(req.JobID)
	defer unlockJob()

	if dup, err := d.isDuplicate(ctx, req.JobID); err != nil {
		return err
	} else if dup {
		return nil
	}

	release, acquired, err := d.lock.TryAcquire(ctx, req.JobID)
	if err != nil {
		return err
	}
	if !acquired {
		return nil
	}
	defer release()

	if dup, err := d.isDuplicate(ctx, req.JobID); err != nil {
		return err
	} else if dup {
		return nil
	}

	runnerName := fmt.Sprintf("ghs-%s-%s", req.RunID, req.JobID)
	jobName := fmt.Sprintf("ghs-job-%s-%s", req.RunID, req.JobID)
	secretName := fmt.Sprintf("jit-%s-%s", req.RunID, req.JobID)

	var jit ghclient.JITConfigResponse
	retryKey := "jit-" + req.JobID
	err = withRetry(ctx, d.cfg.MaxAttempts, d.wait, d.backoff, retryKey, func() error {
		resp, err := d.gh.GenerateJITConfig(ctx, req.Owner, req.Repo, ghclient.JITConfigRequest{
			Name:          runnerName,
			RunnerGroupID: d.cfg.RunnerGroupID,
			Labels:        req.Labels,
		})
		if err != nil {
			return fmt.Errorf("dispatch: jit config: %w", err)
		}
		jit = resp
		return nil
	})
	if err != nil {
		if d.cfg.OnError != nil {
			d.cfg.OnError(ctx, req, "jit_config")
		}
		return err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: d.cfg.Namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"config": []byte(jit.EncodedJITConfig),
		},
	}

	err = withRetry(ctx, d.cfg.MaxAttempts, d.wait, d.backoff, "secret-"+req.JobID, func() error {
		if _, err := d.k8s.CoreV1().Secrets(d.cfg.Namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("dispatch: create secret: %w", err)
		}
		return nil
	})
	if err != nil {
		if d.cfg.OnError != nil {
			d.cfg.OnError(ctx, req, "create_secret")
		}
		d.cleanupJITRunner(ctx, req, jit)
		return err
	}

	job := k8sjob.BuildJob(k8sjob.Config{
		Namespace:           d.cfg.Namespace,
		RunnerImage:         d.cfg.RunnerImage,
		CacheImage:          d.cfg.CacheImage,
		CachePort:           d.cfg.CachePort,
		JITSecretName:       secretName,
		ArchNodeLabel:       d.cfg.ArchNodeLabel,
		PoolNodeLabel:       d.cfg.PoolNodeLabel,
		MemPerCPU:           d.cfg.MemPerCPU,
		CacheBackend:        d.cfg.CacheBackend,
		S3Endpoint:          d.cfg.S3Endpoint,
		S3Bucket:            d.cfg.S3Bucket,
		S3Region:            d.cfg.S3Region,
		S3SecretName:        d.cfg.S3SecretName,
		SpotTolerationKey:   d.cfg.SpotTolerationKey,
		SpotTolerationValue: d.cfg.SpotTolerationValue,
		RunnerName:          runnerName,
		JobName:             jobName,
		OwnerRepo:           fmt.Sprintf("%s/%s", req.Owner, req.Repo),
		RunID:               req.RunID,
		JobID:               req.JobID,
	}, spec)

	var createdJob *batchv1.Job
	err = withRetry(ctx, d.cfg.MaxAttempts, d.wait, d.backoff, "job-"+req.JobID, func() error {
		j, err := d.k8s.BatchV1().Jobs(d.cfg.Namespace).Create(ctx, job, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("dispatch: create job: %w", err)
		}
		createdJob = j
		return nil
	})
	if err != nil {
		_ = d.k8s.CoreV1().Secrets(d.cfg.Namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
		d.cleanupJITRunner(ctx, req, jit)
		if d.cfg.OnError != nil {
			d.cfg.OnError(ctx, req, "create_job")
		}
		return err
	}

	if d.cfg.OnJobCreated != nil {
		d.cfg.OnJobCreated(req.JobID)
	}

	secret.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: batchv1.SchemeGroupVersion.String(),
			Kind:       "Job",
			Name:       createdJob.Name,
			UID:        createdJob.UID,
		},
	}
	patchErr := withRetry(ctx, d.cfg.MaxAttempts, d.wait, d.backoff, "ownerref-"+req.JobID, func() error {
		if _, err := d.k8s.CoreV1().Secrets(d.cfg.Namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("dispatch: patch secret owner ref: %w", err)
		}
		return nil
	})
	if patchErr != nil {
		_ = d.k8s.CoreV1().Secrets(d.cfg.Namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
		return patchErr
	}

	return nil
}

func (d *Dispatcher) isDuplicate(ctx context.Context, jobID string) (bool, error) {
	jobs, err := d.k8s.BatchV1().Jobs(d.cfg.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", k8sjob.LabelGHJob, jobID),
	})
	if err != nil {
		return false, fmt.Errorf("dispatch: list jobs: %w", err)
	}
	return len(jobs.Items) > 0, nil
}

func (d *Dispatcher) cleanupJITRunner(ctx context.Context, req Request, jit ghclient.JITConfigResponse) {
	if jit.RunnerID == 0 {
		return
	}
	if err := d.gh.DeleteRunner(ctx, req.Owner, req.Repo, jit.RunnerID); err != nil {
		slog.Warn("dispatch: delete orphan gh runner failed",
			"runner_id", jit.RunnerID, "runner_name", jit.RunnerName, "err", err)
	}
}
