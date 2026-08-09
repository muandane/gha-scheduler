package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/muandane/gha-scheduler/internal/dispatch"
	"github.com/muandane/gha-scheduler/internal/ghapp"
	"github.com/muandane/gha-scheduler/internal/ghclient"
	"github.com/muandane/gha-scheduler/internal/informer"
	"github.com/muandane/gha-scheduler/internal/labelquery"
	"github.com/muandane/gha-scheduler/internal/reconciler"
	"github.com/muandane/gha-scheduler/internal/tracing"
	"github.com/muandane/gha-scheduler/internal/webhook"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	tp, mp, err := setupTelemetry(ctx, cfg.ServiceName)
	if err != nil {
		slog.Error("telemetry", "err", err)
		os.Exit(1)
	}
	defer func() {
		_ = tp.Shutdown(context.Background())
		if mp != nil {
			_ = mp.Shutdown(context.Background())
		}
	}()

	k8s, err := newK8sClient()
	if err != nil {
		slog.Error("k8s client", "err", err)
		os.Exit(1)
	}

	gh, err := newGHClient(cfg)
	if err != nil {
		slog.Error("gh client", "err", err)
		os.Exit(1)
	}

	tracer := tp.Tracer("gha-scheduler")
	meter := mp.Meter(tracing.MeterName)
	jobMetrics, err := tracing.NewMetrics(meter)
	if err != nil {
		slog.Error("metrics", "err", err)
		os.Exit(1)
	}
	spanEmitter := tracing.NewSpanEmitter(tracer, nil, jobMetrics)

	labelDefaults := dispatch.LabelDefaults{CPU: cfg.DefaultCPU, Arch: cfg.DefaultArch}
	dispatcher := dispatch.New(dispatch.Config{
		Namespace:           cfg.Namespace,
		RunnerImage:         cfg.RunnerImage,
		CacheImage:          cfg.CacheImage,
		CachePort:           cfg.CachePort,
		MemPerCPU:           cfg.MemPerCPU,
		ArchNodeLabel:       cfg.ArchNodeLabel,
		PoolNodeLabel:       cfg.PoolNodeLabel,
		CacheBackend:        cfg.CacheBackend,
		S3Endpoint:          cfg.S3Endpoint,
		S3Bucket:            cfg.S3Bucket,
		S3Region:            cfg.S3Region,
		S3SecretName:        cfg.S3SecretName,
		SpotTolerationKey:   cfg.SpotTolerationKey,
		SpotTolerationValue: cfg.SpotTolerationValue,
		LockIdentity:        cfg.LockIdentity,
		LabelWarn: func(key, value string) {
			slog.Warn("unknown label key", "key", key, "value", value)
		},
		OnParsed: func(ctx context.Context, req dispatch.Request, spec labelquery.RunnerSpec) {
			spanEmitter.DispatchStarted(ctx, req.JobID, spec, req.Owner+"/"+req.Repo)
		},
		OnJobCreated: func(jobID string) {
			spanEmitter.JobCreated(jobID)
		},
		OnError: func(ctx context.Context, req dispatch.Request, reason string) {
			jobMetrics.RecordDispatchError(ctx, reason)
		},
	}, k8s, gh)

	wh := webhook.New(webhook.Config{
		Secret:        cfg.WebhookSecret,
		LabelDefaults: labelDefaults,
		OnQueued: func(ctx context.Context, req dispatch.Request) {
			spanEmitter.WebhookReceived(ctx, map[string]string{
				"repo":   req.Owner + "/" + req.Repo,
				"run_id": req.RunID,
				"job_id": req.JobID,
			})
		},
	}, dispatcher)

	mux := http.NewServeMux()
	mux.Handle("/webhook", wh)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{Addr: cfg.ListenAddr, Handler: mux}
	go func() {
		slog.Info("listening", "addr", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server", "err", err)
			os.Exit(1)
		}
	}()

	podWatcher := informer.NewPodWatcher(spanEmitter)
	startPodInformer(ctx, k8s, cfg.Namespace, podWatcher)

	rec := reconciler.New(reconciler.Config{
		Namespace:      cfg.Namespace,
		Repos:          cfg.Repos,
		Interval:       cfg.ReconcileInterval,
		StaleThreshold: cfg.StaleThreshold,
		LabelDefaults:  labelDefaults,
	}, gh, dispatcher, k8s)

	startLeaderElectedReconciler(ctx, k8s, cfg, rec)

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

type appConfig struct {
	ServiceName         string
	ListenAddr          string
	Namespace           string
	WebhookSecret       string
	RunnerImage         string
	CacheImage          string
	CachePort           int32
	MemPerCPU           string
	ArchNodeLabel       string
	PoolNodeLabel       string
	DefaultCPU          int
	DefaultArch         string
	GHAPIBase           string
	GHAppID             int64
	GHInstallationID    int64
	GHAppPrivateKey     []byte
	Repos               []string
	ReconcileInterval   time.Duration
	StaleThreshold      time.Duration
	LeaderElectionID    string
	CacheBackend        string
	S3Endpoint          string
	S3Bucket            string
	S3Region            string
	S3SecretName        string
	SpotTolerationKey   string
	SpotTolerationValue string
	LockIdentity        string
}

func loadConfig() (appConfig, error) {
	cfg := appConfig{
		ServiceName:       env("GHA_SERVICE_NAME", "gha-scheduler"),
		ListenAddr:        env("GHA_LISTEN_ADDR", ":8080"),
		Namespace:         env("GHA_NAMESPACE", "gha-runners"),
		WebhookSecret:     os.Getenv("GHA_WEBHOOK_SECRET"),
		RunnerImage:       env("GHA_RUNNER_IMAGE", "ghcr.io/actions/actions-runner:2.317.0"),
		CacheImage:        env("GHA_CACHE_IMAGE", "ghcr.io/muandane/gha-cache-sidecar:latest"),
		MemPerCPU:         env("GHA_MEM_PER_CPU", "2Gi"),
		ArchNodeLabel:     env("GHA_ARCH_NODE_LABEL", "kubernetes.io/arch"),
		PoolNodeLabel:     env("GHA_POOL_NODE_LABEL", "pool"),
		DefaultArch:       env("GHA_DEFAULT_ARCH", "x64"),
		GHAPIBase:         env("GHA_GITHUB_API_URL", "https://api.github.com"),
		LeaderElectionID:  env("GHA_LEADER_ELECTION_ID", "gha-scheduler"),
		ReconcileInterval: envDuration("GHA_RECONCILE_INTERVAL", 60*time.Second),
		StaleThreshold:    envDuration("GHA_STALE_THRESHOLD", 30*time.Second),
	}
	cpu, err := strconv.Atoi(env("GHA_DEFAULT_CPU", "2"))
	if err != nil || cpu <= 0 {
		return cfg, fmt.Errorf("invalid GHA_DEFAULT_CPU")
	}
	cfg.DefaultCPU = cpu

	port, err := strconv.Atoi(env("GHA_CACHE_PORT", "8080"))
	if err != nil {
		return cfg, fmt.Errorf("invalid GHA_CACHE_PORT")
	}
	cfg.CachePort = int32(port)

	cfg.CacheBackend = env("GHA_CACHE_BACKEND", "s3")
	cfg.S3Endpoint = env("GHA_S3_ENDPOINT", "http://seaweedfs-s3.gha-runners.svc:8333")
	cfg.S3Bucket = env("GHA_S3_BUCKET", "gha-cache")
	cfg.S3Region = env("GHA_S3_REGION", "us-east-1")
	cfg.S3SecretName = env("GHA_S3_SECRET_NAME", "gha-cache-s3-credentials")
	cfg.SpotTolerationKey = env("GHA_SPOT_TOLERATION_KEY", "spot")
	cfg.SpotTolerationValue = env("GHA_SPOT_TOLERATION_VALUE", "true")
	cfg.LockIdentity = fmt.Sprintf("%s-%d", hostname(), os.Getpid())

	if cfg.WebhookSecret == "" {
		return cfg, fmt.Errorf("GHA_WEBHOOK_SECRET is required")
	}

	if v := os.Getenv("GHA_APP_ID"); v != "" {
		cfg.GHAppID, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			return cfg, fmt.Errorf("invalid GHA_APP_ID")
		}
	}
	if v := os.Getenv("GHA_INSTALLATION_ID"); v != "" {
		cfg.GHInstallationID, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			return cfg, fmt.Errorf("invalid GHA_INSTALLATION_ID")
		}
	}
	cfg.GHAppPrivateKey = []byte(os.Getenv("GHA_APP_PRIVATE_KEY"))
	if cfg.GHAppID == 0 || cfg.GHInstallationID == 0 || len(cfg.GHAppPrivateKey) == 0 {
		return cfg, fmt.Errorf("GHA_APP_ID, GHA_INSTALLATION_ID, and GHA_APP_PRIVATE_KEY are required")
	}

	if repos := os.Getenv("GHA_REPOS"); repos != "" {
		cfg.Repos = strings.Split(repos, ",")
	}

	return cfg, nil
}

func newK8sClient() (kubernetes.Interface, error) {
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		restCfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
		return kubernetes.NewForConfig(restCfg)
	}
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(restCfg)
}

func newGHClient(cfg appConfig) (*ghclient.Client, error) {
	ts, err := ghapp.NewTokenSource(cfg.GHAppID, cfg.GHInstallationID, cfg.GHAppPrivateKey, cfg.GHAPIBase, http.DefaultClient)
	if err != nil {
		return nil, err
	}
	return ghclient.New(cfg.GHAPIBase, ghclient.WithTokenFunc(ts.Token)), nil
}

func setupTelemetry(ctx context.Context, serviceName string) (*sdktrace.TracerProvider, *metric.MeterProvider, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		tp := sdktrace.NewTracerProvider()
		mp := metric.NewMeterProvider()
		otel.SetTracerProvider(tp)
		otel.SetMeterProvider(mp)
		return tp, mp, nil
	}

	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, nil, err
	}
	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, nil, err
	}
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			"",
			attribute.String("service.name", serviceName),
		),
	)
	if err != nil {
		return nil, nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	mp := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(metricExporter)),
		metric.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	return tp, mp, nil
}

func startPodInformer(ctx context.Context, k8s kubernetes.Interface, namespace string, watcher *informer.PodWatcher) {
	factory := informers.NewSharedInformerFactoryWithOptions(k8s, 30*time.Second, informers.WithNamespace(namespace))
	informer := factory.Core().V1().Pods().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj any) {
			oldPod, _ := oldObj.(*corev1.Pod)
			newPod, _ := newObj.(*corev1.Pod)
			watcher.OnUpdate(ctx, oldPod, newPod)
		},
	})
	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())
}

func startLeaderElectedReconciler(ctx context.Context, k8s kubernetes.Interface, cfg appConfig, rec *reconciler.Reconciler) {
	if len(cfg.Repos) == 0 {
		slog.Info("reconciler disabled: no GHA_REPOS configured")
		return
	}

	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      cfg.LeaderElectionID,
			Namespace: cfg.Namespace,
		},
		Client: k8s.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: hostname(),
		},
	}

	go func() {
		leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
			Lock:            lock,
			ReleaseOnCancel: true,
			LeaseDuration:   15 * time.Second,
			RenewDeadline:   10 * time.Second,
			RetryPeriod:     2 * time.Second,
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: func(ctx context.Context) {
					slog.Info("reconciler leader")
					if err := rec.Run(ctx); err != nil && ctx.Err() == nil {
						slog.Error("reconciler stopped", "err", err)
					}
				},
				OnStoppedLeading: func() {
					slog.Info("reconciler lost leadership")
				},
			},
		})
	}()
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "gha-scheduler"
	}
	return h
}
