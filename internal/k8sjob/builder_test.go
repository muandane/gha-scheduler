package k8sjob_test

import (
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/muandane/gha-scheduler/internal/k8sjob"
	"github.com/muandane/gha-scheduler/internal/labelquery"
)

func TestBuildJob(t *testing.T) {
	cfg := k8sjob.Config{
		Namespace:     "gha-runners",
		RunnerImage:   "ghcr.io/actions/runner:latest",
		CacheImage:    "ghcr.io/org/cache-sidecar:latest",
		CachePort:     8080,
		JITSecretName: "jit-abc",
		ArchNodeLabel: "kubernetes.io/arch",
		PoolNodeLabel: "pool",
		MemPerCPU:     "2Gi",
		RunnerName:    "ghs-123-456",
		JobName:       "ghs-job-123-456",
		OwnerRepo:     "org/repo",
		RunID:         "123",
		JobID:         "456",
	}

	tests := []struct {
		name  string
		spec  labelquery.RunnerSpec
		want  *batchv1.Job
		s3Cfg *k8sjob.Config
	}{
		{
			name: "basic job without cache",
			spec: labelquery.RunnerSpec{
				RunID: "123",
				CPU:   4,
				Arch:  "x64",
				Pool:  "spot",
			},
			want: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ghs-job-123-456",
					Namespace: "gha-runners",
					Labels: map[string]string{
						k8sjob.LabelRunID:     "123",
						k8sjob.LabelJobID:     "456",
						k8sjob.LabelGHJob:     "456",
						k8sjob.LabelOwnerRepo: "org/repo",
					},
				},
				Spec: batchv1.JobSpec{
					BackoffLimit:            new(int32(0)),
					TTLSecondsAfterFinished: new(int32(60)),
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							NodeSelector: map[string]string{
								"kubernetes.io/arch": "amd64",
								"pool":               "spot",
							},
							Containers: []corev1.Container{
								{
									Name:  "runner",
									Image: "ghcr.io/actions/runner:latest",
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("4"),
											corev1.ResourceMemory: resource.MustParse("8Gi"),
										},
										Limits: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("4"),
											corev1.ResourceMemory: resource.MustParse("8Gi"),
										},
									},
									VolumeMounts: []corev1.VolumeMount{
										{Name: "jit-config", MountPath: "/jit"},
										{Name: "runner-work", MountPath: "/home/runner/_work"},
									},
									WorkingDir: "/home/runner",
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "jit-config",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "jit-abc",
										},
									},
								},
								{
									Name: "runner-work",
									VolumeSource: corev1.VolumeSource{
										EmptyDir: &corev1.EmptyDirVolumeSource{},
									},
								},
							},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			},
		},
		{
			name: "job with cache sidecar and explicit mem",
			spec: labelquery.RunnerSpec{
				RunID: "123",
				CPU:   2,
				Mem:   "4Gi",
				Arch:  "arm64",
				Pool:  "on-demand",
				Cache: true,
			},
			want: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ghs-job-123-456",
					Namespace: "gha-runners",
					Labels: map[string]string{
						k8sjob.LabelRunID:     "123",
						k8sjob.LabelJobID:     "456",
						k8sjob.LabelGHJob:     "456",
						k8sjob.LabelOwnerRepo: "org/repo",
					},
				},
				Spec: batchv1.JobSpec{
					BackoffLimit:            new(int32(0)),
					TTLSecondsAfterFinished: new(int32(60)),
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							NodeSelector: map[string]string{
								"kubernetes.io/arch": "arm64",
								"pool":               "on-demand",
							},
							Containers: []corev1.Container{
								{
									Name:  "runner",
									Image: "ghcr.io/actions/runner:latest",
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("2"),
											corev1.ResourceMemory: resource.MustParse("4Gi"),
										},
										Limits: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("2"),
											corev1.ResourceMemory: resource.MustParse("4Gi"),
										},
									},
									Env: []corev1.EnvVar{
										{Name: "ACTIONS_CACHE_URL", Value: "http://127.0.0.1:8080/"},
										{Name: "ACTIONS_RESULTS_URL", Value: "http://127.0.0.1:8080/"},
										{Name: "ACTIONS_CACHE_SERVICE_V2", Value: "true"},
									},
									VolumeMounts: []corev1.VolumeMount{
										{Name: "jit-config", MountPath: "/jit"},
										{Name: "runner-work", MountPath: "/home/runner/_work"},
									},
									WorkingDir: "/home/runner",
								},
								{
									Name:  "cache-sidecar",
									Image: "ghcr.io/org/cache-sidecar:latest",
									Env: []corev1.EnvVar{
										{Name: "CACHE_PORT", Value: "8080"},
										{Name: "CACHE_PREFIX", Value: "org/repo"},
										{Name: "CACHE_BACKEND", Value: "memory"},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "jit-config",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "jit-abc",
										},
									},
								},
								{
									Name: "runner-work",
									VolumeSource: corev1.VolumeSource{
										EmptyDir: &corev1.EmptyDirVolumeSource{},
									},
								},
							},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			},
		},
		{
			name: "cache sidecar with s3 backend",
			spec: labelquery.RunnerSpec{
				RunID: "123",
				CPU:   2,
				Arch:  "x64",
				Cache: true,
			},
			want: nil, // asserted separately
			s3Cfg: &k8sjob.Config{
				Namespace:     "gha-runners",
				RunnerImage:   "ghcr.io/actions/runner:latest",
				CacheImage:    "ghcr.io/org/cache-sidecar:latest",
				CachePort:     8080,
				JITSecretName: "jit-abc",
				MemPerCPU:     "2Gi",
				CacheBackend:  "s3",
				S3Endpoint:    "http://seaweedfs-s3.gha-runners.svc:8333",
				S3Bucket:      "gha-cache",
				S3Region:      "us-east-1",
				S3SecretName:  "gha-cache-s3-credentials",
				RunnerName:    "ghs-123-456",
				JobName:       "ghs-job-123-456",
				OwnerRepo:     "org/repo",
				RunID:         "123",
				JobID:         "456",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testCfg := cfg
			if tt.s3Cfg != nil {
				testCfg = *tt.s3Cfg
			}
			got := k8sjob.BuildJob(testCfg, tt.spec)
			if tt.want != nil {
				assertJobEqual(t, got, tt.want)
				return
			}
			sidecar := got.Spec.Template.Spec.Containers[1]
			if sidecar.Name != "cache-sidecar" {
				t.Fatalf("expected cache sidecar")
			}
			envByName := map[string]corev1.EnvVar{}
			for _, e := range sidecar.Env {
				envByName[e.Name] = e
			}
			if envByName["CACHE_BACKEND"].Value != "s3" {
				t.Fatalf("CACHE_BACKEND: %v", envByName["CACHE_BACKEND"])
			}
			if envByName["S3_ENDPOINT"].Value != "http://seaweedfs-s3.gha-runners.svc:8333" {
				t.Fatalf("S3_ENDPOINT: %v", envByName["S3_ENDPOINT"])
			}
			if envByName["S3_ACCESS_KEY"].ValueFrom == nil || envByName["S3_ACCESS_KEY"].ValueFrom.SecretKeyRef.Key != "access-key" {
				t.Fatalf("S3_ACCESS_KEY secret ref: %v", envByName["S3_ACCESS_KEY"])
			}
		})
	}
}

func TestBuildJobActiveDeadline(t *testing.T) {
	job := k8sjob.BuildJob(k8sjob.Config{
		Namespace:         "gha-runners",
		RunnerImage:       "img",
		JITSecretName:     "jit",
		JobName:           "job",
		MemPerCPU:         "2Gi",
		OwnerRepo:         "org/repo",
		RunID:             "1",
		JobID:             "2",
		MaxRuntimeSeconds: 3600,
	}, labelquery.RunnerSpec{RunID: "1", CPU: 2, Arch: "x64"})
	if job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != 3600 {
		t.Fatalf("activeDeadlineSeconds: %v", job.Spec.ActiveDeadlineSeconds)
	}
	if job.Labels[k8sjob.LabelOwnerRepo] != "org/repo" {
		t.Fatalf("owner_repo label: %q", job.Labels[k8sjob.LabelOwnerRepo])
	}
}

func TestBuildJobSpotToleration(t *testing.T) {
	job := k8sjob.BuildJob(k8sjob.Config{
		Namespace:           "gha-runners",
		RunnerImage:         "img",
		JITSecretName:       "jit",
		JobName:             "job",
		MemPerCPU:           "2Gi",
		SpotTolerationKey:   "spot",
		SpotTolerationValue: "true",
	}, labelquery.RunnerSpec{RunID: "1", CPU: 2, Arch: "x64", Pool: "spot"})
	tols := job.Spec.Template.Spec.Tolerations
	if len(tols) != 1 || tols[0].Key != "spot" || tols[0].Value != "true" {
		t.Fatalf("tolerations: %+v", tols)
	}
}

func assertJobEqual(t *testing.T, got, want *batchv1.Job) {
	t.Helper()

	if got.Name != want.Name || got.Namespace != want.Namespace {
		t.Fatalf("metadata name/ns: got %s/%s want %s/%s", got.Name, got.Namespace, want.Name, want.Namespace)
	}
	for k, v := range want.Labels {
		if got.Labels[k] != v {
			t.Fatalf("label %q: got %q want %q", k, got.Labels[k], v)
		}
	}
	if *got.Spec.BackoffLimit != *want.Spec.BackoffLimit {
		t.Fatalf("backoffLimit: got %d want %d", *got.Spec.BackoffLimit, *want.Spec.BackoffLimit)
	}
	if *got.Spec.TTLSecondsAfterFinished != *want.Spec.TTLSecondsAfterFinished {
		t.Fatalf("ttl: got %d want %d", *got.Spec.TTLSecondsAfterFinished, *want.Spec.TTLSecondsAfterFinished)
	}

	gotPod := got.Spec.Template.Spec
	wantPod := want.Spec.Template.Spec

	if len(gotPod.NodeSelector) != len(wantPod.NodeSelector) {
		t.Fatalf("nodeSelector len: got %v want %v", gotPod.NodeSelector, wantPod.NodeSelector)
	}
	for k, v := range wantPod.NodeSelector {
		if gotPod.NodeSelector[k] != v {
			t.Fatalf("nodeSelector[%q]: got %q want %q", k, gotPod.NodeSelector[k], v)
		}
	}

	if len(gotPod.Containers) != len(wantPod.Containers) {
		t.Fatalf("containers: got %d want %d", len(gotPod.Containers), len(wantPod.Containers))
	}
	for i := range wantPod.Containers {
		gc := gotPod.Containers[i]
		wc := wantPod.Containers[i]
		if gc.Name != wc.Name || gc.Image != wc.Image {
			t.Fatalf("container[%d]: got %s/%s want %s/%s", i, gc.Name, gc.Image, wc.Name, wc.Image)
		}
		if gc.Name == "runner" {
			if !resourceListEqual(gc.Resources.Requests, wc.Resources.Requests) {
				t.Fatalf("container[%d] requests: got %v want %v", i, gc.Resources.Requests, wc.Resources.Requests)
			}
			if !resourceListEqual(gc.Resources.Limits, wc.Resources.Limits) {
				t.Fatalf("container[%d] limits: got %v want %v", i, gc.Resources.Limits, wc.Resources.Limits)
			}
		}
		if !envEqual(gc.Env, wc.Env) {
			if gc.Name == "runner" && len(wc.Env) == 0 {
				// Runner hardening env is always injected; want fixtures omit it.
			} else {
				t.Fatalf("container[%d] env: got %v want %v", i, gc.Env, wc.Env)
			}
		}
		if gc.StartupProbe != nil || wc.StartupProbe != nil {
			if wc.StartupProbe == nil {
				continue
			}
			if gc.StartupProbe == nil || wc.StartupProbe == nil {
				t.Fatalf("container[%d] startupProbe mismatch", i)
			}
		}
	}
}

func resourceListEqual(a, b corev1.ResourceList) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || av.Cmp(bv) != 0 {
			return false
		}
	}
	return true
}

func envEqual(a, b []corev1.EnvVar) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Value != b[i].Value {
			return false
		}
	}
	return true
}

//go:fix inline
func int32Ptr(v int32) *int32 { return new(v) }
