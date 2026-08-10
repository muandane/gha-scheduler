package k8sjob

import (
	"fmt"
	"strconv"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/muandane/gha-scheduler/internal/labelquery"
)

const (
	LabelRunID     = "gha-scheduler.run_id"
	LabelJobID     = "gha-scheduler.job_id"
	LabelGHJob     = "gha-scheduler.gh_job_id"
	LabelOwnerRepo = "gha-scheduler.owner_repo"

	// Official image WORKDIR; run.sh lives here (ghcr.io/actions/actions-runner).
	runnerHome    = "/home/runner"
	runnerWorkDir = "/home/runner/_work"
)

// Config holds static job-building parameters from control-plane config.
type Config struct {
	Namespace           string
	RunnerImage         string
	CacheImage          string
	CachePort           int32
	JITSecretName       string
	ArchNodeLabel       string
	PoolNodeLabel       string
	MemPerCPU           string
	CacheBackend        string
	S3Endpoint          string
	S3Bucket            string
	S3Region            string
	S3SecretName        string
	SpotTolerationKey   string
	SpotTolerationValue string
	RunnerName          string
	JobName             string
	OwnerRepo           string
	RunID               string
	JobID               string
	MaxRuntimeSeconds   int64
}

// BuildJob constructs a batch Job for the given RunnerSpec.
func BuildJob(cfg Config, spec labelquery.RunnerSpec) *batchv1.Job {
	image := cfg.RunnerImage
	if spec.Image != "" {
		image = spec.Image
	}

	mem := memoryForSpec(spec, cfg.MemPerCPU)
	cpuQty := resource.MustParse(strconv.Itoa(spec.CPU))

	nodeSelector := map[string]string{}
	if cfg.ArchNodeLabel != "" && spec.Arch != "" {
		nodeSelector[cfg.ArchNodeLabel] = archToNodeSelectorValue(spec.Arch)
	}
	if cfg.PoolNodeLabel != "" && spec.Pool != "" {
		nodeSelector[cfg.PoolNodeLabel] = spec.Pool
	}

	runner := corev1.Container{
		Name:  "runner",
		Image: image,
		Env: []corev1.EnvVar{
			{Name: "RUNNER_EPHEMERAL", Value: "true"},
			{Name: "DISABLE_RUNNER_UPDATE", Value: "true"},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot: boolPtr(true),
			RunAsUser:    int64Ptr(1001), // ghcr.io/actions/actions-runner USER runner
		},
		Command: []string{
			"/bin/bash",
			"-c",
			fmt.Sprintf("./run.sh --jitconfig \"$(cat /jit/config)\" --name %q", cfg.RunnerName),
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    cpuQty,
				corev1.ResourceMemory: mem,
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    cpuQty,
				corev1.ResourceMemory: mem,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "jit-config", MountPath: "/jit"},
			{Name: "runner-work", MountPath: runnerWorkDir},
		},
		WorkingDir: runnerHome,
	}

	containers := []corev1.Container{runner}

	if spec.Cache {
		cacheURL := fmt.Sprintf("http://127.0.0.1:%d/", cfg.CachePort)
		containers[0].Env = []corev1.EnvVar{
			{Name: "ACTIONS_CACHE_URL", Value: cacheURL},
			{Name: "ACTIONS_RESULTS_URL", Value: cacheURL},
			{Name: "ACTIONS_CACHE_SERVICE_V2", Value: "true"},
		}
		containers = append(containers, corev1.Container{
			Name:  "cache-sidecar",
			Image: cfg.CacheImage,
			Env:   cacheSidecarEnv(cfg),
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
			},
			StartupProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/healthz",
						Port: intstr.FromInt32(cfg.CachePort),
					},
				},
				PeriodSeconds:    1,
				FailureThreshold: 5,
				TimeoutSeconds:   1,
			},
		})
	}

	labels := jobLabels(cfg)
	podSpec := corev1.PodSpec{
		NodeSelector: nodeSelector,
		Containers:   containers,
		Volumes: []corev1.Volume{
			{
				Name: "jit-config",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: cfg.JITSecretName,
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
	}
	if spec.Pool == "spot" && cfg.SpotTolerationKey != "" {
		podSpec.Tolerations = []corev1.Toleration{{
			Key:      cfg.SpotTolerationKey,
			Operator: corev1.TolerationOpEqual,
			Value:    cfg.SpotTolerationValue,
			Effect:   corev1.TaintEffectNoSchedule,
		}}
	}

	jobSpec := batchv1.JobSpec{
		BackoffLimit:            new(int32(0)),
		TTLSecondsAfterFinished: new(int32(60)),
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: labels,
			},
			Spec: podSpec,
		},
	}
	if cfg.MaxRuntimeSeconds > 0 {
		jobSpec.ActiveDeadlineSeconds = int64Ptr(cfg.MaxRuntimeSeconds)
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.JobName,
			Namespace: cfg.Namespace,
			Labels:    labels,
		},
		Spec: jobSpec,
	}
}

func archToNodeSelectorValue(arch string) string {
	if arch == "x64" {
		return "amd64"
	}
	return arch
}

func memoryForSpec(spec labelquery.RunnerSpec, memPerCPU string) resource.Quantity {
	if spec.Mem != "" {
		return resource.MustParse(spec.Mem)
	}
	perCPU := resource.MustParse(memPerCPU)
	total := perCPU.DeepCopy()
	total.SetScaled(total.Value()*int64(spec.CPU), 0)
	return total
}

//go:fix inline
func int32Ptr(v int32) *int32 { return new(v) }

func int64Ptr(v int64) *int64 { return new(v) }

func boolPtr(v bool) *bool { return new(v) }

func jobLabels(cfg Config) map[string]string {
	labels := map[string]string{
		LabelRunID: cfg.RunID,
		LabelJobID: cfg.JobID,
		LabelGHJob: cfg.JobID,
	}
	if cfg.OwnerRepo != "" {
		labels[LabelOwnerRepo] = cfg.OwnerRepo
	}
	return labels
}

func cacheSidecarEnv(cfg Config) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: "CACHE_PORT", Value: strconv.FormatInt(int64(cfg.CachePort), 10)},
		{Name: "CACHE_PREFIX", Value: cfg.OwnerRepo},
	}
	backend := cfg.CacheBackend
	if backend == "" {
		backend = "memory"
	}
	env = append(env, corev1.EnvVar{Name: "CACHE_BACKEND", Value: backend})
	if backend == "s3" {
		region := cfg.S3Region
		if region == "" {
			region = "us-east-1"
		}
		env = append(env,
			corev1.EnvVar{Name: "S3_ENDPOINT", Value: cfg.S3Endpoint},
			corev1.EnvVar{Name: "S3_BUCKET", Value: cfg.S3Bucket},
			corev1.EnvVar{Name: "S3_REGION", Value: region},
		)
		if cfg.S3SecretName != "" {
			env = append(env,
				corev1.EnvVar{
					Name: "S3_ACCESS_KEY",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: cfg.S3SecretName},
							Key:                  "access-key",
						},
					},
				},
				corev1.EnvVar{
					Name: "S3_SECRET_KEY",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: cfg.S3SecretName},
							Key:                  "secret-key",
						},
					},
				},
			)
		}
	}
	return env
}
