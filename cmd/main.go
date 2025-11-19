/*
Copyright 2025 Lumina Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Main entrypoint for the Lumina controller manager.
// This file is generated scaffolding from kubebuilder and will be
// tested through E2E tests, not unit tests.
//
// Coverage: Excluded - main entrypoints are tested via E2E tests

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/nextdoor/lumina/internal/cache"
	"github.com/nextdoor/lumina/internal/controller"
	"github.com/nextdoor/lumina/pkg/aws"
	"github.com/nextdoor/lumina/pkg/config"
	"github.com/nextdoor/lumina/pkg/cost"
	"github.com/nextdoor/lumina/pkg/metrics"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

// reconcilers holds all initialized reconcilers.
// This struct reduces code duplication between standalone and Kubernetes modes.
type reconcilers struct {
	RISP    *controller.RISPReconciler
	EC2     *controller.EC2Reconciler
	Pricing *controller.PricingReconciler
	SPRates *controller.SPRatesReconciler
	Cost    *controller.CostReconciler
	ReadyCh chan struct{} // Channel for RISP->SPRates coordination
}

// initializeReconcilers creates all reconciler instances with their dependencies.
// This function is shared between standalone and Kubernetes modes to reduce duplication.
func initializeReconcilers(
	awsClient aws.Client,
	cfg *config.Config,
	rispCache *cache.RISPCache,
	ec2Cache *cache.EC2Cache,
	pricingCache *cache.PricingCache,
	luminaMetrics *metrics.Metrics,
	costCalculator *cost.Calculator,
) *reconcilers {
	// Create channel for RISP -> SPRates coordination
	// RISP closes this after initial run, SPRates waits on it before starting
	rispReadyCh := make(chan struct{})

	return &reconcilers{
		RISP: &controller.RISPReconciler{
			AWSClient: awsClient,
			Config:    cfg,
			Cache:     rispCache,
			Metrics:   luminaMetrics,
			Log:       ctrl.Log.WithName("risp-reconciler"),
			Regions:   cfg.Regions,
			ReadyChan: rispReadyCh,
		},
		EC2: &controller.EC2Reconciler{
			AWSClient: awsClient,
			Config:    cfg,
			Cache:     ec2Cache,
			Metrics:   luminaMetrics,
			Log:       ctrl.Log.WithName("ec2-reconciler"),
			Regions:   cfg.Regions,
		},
		Pricing: &controller.PricingReconciler{
			AWSClient:        awsClient,
			Config:           cfg,
			Cache:            pricingCache,
			Metrics:          luminaMetrics,
			Log:              ctrl.Log.WithName("pricing-reconciler"),
			Regions:          cfg.Regions,
			OperatingSystems: []string{"Linux", "Windows"},
		},
		SPRates: &controller.SPRatesReconciler{
			AWSClient:     awsClient,
			Config:        cfg,
			RISPCache:     rispCache,
			PricingCache:  pricingCache,
			Metrics:       luminaMetrics,
			Log:           ctrl.Log.WithName("sp-rates-reconciler"),
			RISPReadyChan: rispReadyCh,
		},
		Cost: &controller.CostReconciler{
			Calculator:   costCalculator,
			Config:       cfg,
			EC2Cache:     ec2Cache,
			RISPCache:    rispCache,
			PricingCache: pricingCache,
			Metrics:      luminaMetrics,
			Log:          ctrl.Log.WithName("cost-reconciler"),
		},
		ReadyCh: rispReadyCh,
	}
}

// runStandalone runs the controller in standalone mode without Kubernetes integration.
//
// This mode is designed for local development and testing, enabling developers to run
// the controller on their local machine without requiring a Kubernetes cluster. It provides
// the same AWS data collection and metrics exposure as the full Kubernetes mode, but
// uses simple HTTP servers instead of the controller-runtime manager.
//
// Key differences from Kubernetes mode:
//   - No controller-runtime manager - uses standard http.Server instead
//   - No RBAC or authentication on metrics endpoint (local development only)
//   - RISP reconciler runs on a simple hourly timer instead of K8s reconciliation
//   - Metrics served on configurable HTTP port (default :8080)
//   - Health checks on configurable HTTP port (default :8081)
//
// This mode is activated by the --no-kubernetes command-line flag and is ideal for:
//   - Local development and debugging
//   - Testing AWS API integration without Kubernetes
//   - Quick validation of configuration changes
//   - Exploring metrics output during development
//
// coverage:ignore - standalone mode, tested manually or via E2E
func runStandalone(
	cfg *config.Config,
	metricsAddr string,
	probeAddr string,
	secureMetrics bool,
	metricsCertPath, metricsCertName, metricsCertKey string,
	tlsOpts []func(*tls.Config),
) error {
	setupLog.Info("starting in standalone mode (no Kubernetes integration)")

	// Get default account for non-account-specific AWS calls (pricing, etc)
	defaultAccount := cfg.GetDefaultAccount()

	// Create AWS client
	awsClient, err := aws.NewClient(aws.ClientConfig{
		DefaultRegion: cfg.DefaultRegion,
		DefaultAccount: aws.AccountConfig{
			AccountID:     defaultAccount.AccountID,
			AssumeRoleARN: defaultAccount.AssumeRoleARN,
			Region:        defaultAccount.Region,
		},
	})
	if err != nil {
		return err
	}
	setupLog.Info("created AWS client", "defaultAccount", defaultAccount.Name)

	// Initialize Prometheus metrics without controller-runtime manager
	// We'll create our own HTTP server for metrics
	metricsRegistry := ctrlmetrics.Registry
	luminaMetrics := metrics.NewMetrics(metricsRegistry)
	luminaMetrics.ControllerRunning.Set(1)
	setupLog.Info("metrics initialized")

	// Initialize RI/SP cache
	rispCache := cache.NewRISPCache()
	setupLog.Info("initialized RI/SP cache")

	// Initialize EC2 cache
	ec2Cache := cache.NewEC2Cache()
	setupLog.Info("initialized EC2 cache")

	// Initialize pricing cache
	pricingCache := cache.NewPricingCache()
	setupLog.Info("initialized pricing cache")

	// Create cost calculator (needed before initializing reconcilers)
	costCalculator := cost.NewCalculator(pricingCache, cfg)

	// Initialize all reconcilers using the helper function
	// This reduces code duplication between standalone and Kubernetes modes
	recs := initializeReconcilers(awsClient, cfg, rispCache, ec2Cache, pricingCache, luminaMetrics, costCalculator)

	// Start reconcilers in background goroutines
	ctx := ctrl.SetupSignalHandler()

	// Start pricing reconciler FIRST (blocking initial load)
	// This ensures pricing cache is populated before other reconcilers need it
	go func() {
		if err := recs.Pricing.RunStandalone(ctx); err != nil {
			setupLog.Error(err, "pricing reconciler stopped with error")
		}
	}()
	setupLog.Info("started pricing reconciler in standalone mode (initial load blocking)")

	// Start RISP reconciler - it will signal via channel when ready
	go func() {
		if err := recs.RISP.RunStandalone(ctx); err != nil {
			setupLog.Error(err, "RISP reconciler stopped with error")
		}
	}()
	setupLog.Info("started RISP reconciler in standalone mode")

	// Start SP rates reconciler - waits for RISP to be ready via channel
	go func() {
		if err := recs.SPRates.RunStandalone(ctx); err != nil {
			setupLog.Error(err, "SP rates reconciler stopped with error")
		}
	}()
	setupLog.Info("started SP rates reconciler in standalone mode")

	// Start EC2 reconciler
	go func() {
		if err := recs.EC2.RunStandalone(ctx); err != nil {
			setupLog.Error(err, "EC2 reconciler stopped with error")
		}
	}()
	setupLog.Info("started EC2 reconciler in standalone mode")

	// Create debouncer that triggers cost calculation 1 second after last cache update
	// This prevents "thundering herd" when EC2, RISP, and Pricing caches all update simultaneously
	recs.Cost.Debouncer = cache.NewDebouncer(1*time.Second, func() {
		if _, err := recs.Cost.Reconcile(ctx, ctrl.Request{}); err != nil {
			setupLog.Error(err, "cost calculation triggered by cache update failed")
		}
	})

	// Register cost reconciler with all caches
	// Any cache update will trigger the debouncer, which triggers cost recalculation
	ec2Cache.RegisterUpdateNotifier(recs.Cost.Debouncer.Trigger)
	rispCache.RegisterUpdateNotifier(recs.Cost.Debouncer.Trigger)
	pricingCache.RegisterUpdateNotifier(recs.Cost.Debouncer.Trigger)

	go func() {
		if err := recs.Cost.RunStandalone(ctx); err != nil {
			setupLog.Error(err, "cost reconciler stopped with error")
		}
	}()
	setupLog.Info("started cost reconciler in event-driven mode (1s debounce)")

	// Create credential monitor for AWS health checks
	// The monitor runs background checks at the configured interval instead of on every healthz probe,
	// reducing AWS API calls from ~42/min to ~0.7/min (for 7 accounts with 10m interval).
	validator := aws.NewAccountValidator(awsClient)
	checkInterval := cfg.GetAccountValidationInterval()
	credMonitor := aws.NewCredentialMonitor(validator, cfg.AWSAccounts, checkInterval)
	credMonitor.Start()
	setupLog.Info("started AWS credential monitor",
		"accounts", len(cfg.AWSAccounts),
		"checkInterval", checkInterval)

	// Setup metrics server using standard http package
	// In standalone mode, we serve Prometheus metrics directly without authentication
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.HandlerFor(metricsRegistry, promhttp.HandlerOpts{}))

	var metricsServer *http.Server
	if secureMetrics {
		setupLog.Info("metrics server running with TLS but no authentication (standalone mode)")
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		for _, opt := range tlsOpts {
			opt(tlsConfig)
		}

		if len(metricsCertPath) > 0 {
			certFile := filepath.Join(metricsCertPath, metricsCertName)
			keyFile := filepath.Join(metricsCertPath, metricsCertKey)
			metricsServer = &http.Server{
				Addr:      metricsAddr,
				Handler:   metricsMux,
				TLSConfig: tlsConfig,
			}
			go func() {
				setupLog.Info("starting metrics server with TLS", "address", metricsAddr)
				if err := metricsServer.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
					setupLog.Error(err, "metrics server stopped with error")
				}
			}()
		} else {
			setupLog.Info("TLS requested but no certificates provided, using HTTP instead")
			metricsServer = &http.Server{
				Addr:    metricsAddr,
				Handler: metricsMux,
			}
			go func() {
				setupLog.Info("starting metrics server", "address", metricsAddr)
				if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					setupLog.Error(err, "metrics server stopped with error")
				}
			}()
		}
	} else {
		metricsServer = &http.Server{
			Addr:    metricsAddr,
			Handler: metricsMux,
		}
		go func() {
			setupLog.Info("starting metrics server", "address", metricsAddr)
			if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				setupLog.Error(err, "metrics server stopped with error")
			}
		}()
	}
	setupLog.Info("metrics server ready")

	// Setup health check server
	// Health checks use the credential monitor's cached status instead of making AWS API calls
	awsHealthChecker := aws.NewHealthChecker(credMonitor)
	healthHandler := &healthz.Handler{
		Checks: map[string]healthz.Checker{
			"healthz": healthz.Ping,
			"readyz":  awsHealthChecker.Check,
		},
	}

	healthMux := http.NewServeMux()
	healthMux.Handle("/healthz", http.StripPrefix("/healthz", healthHandler))
	healthMux.Handle("/readyz", http.StripPrefix("/readyz", healthHandler))

	healthServer := &http.Server{
		Addr:    probeAddr,
		Handler: healthMux,
	}

	go func() {
		setupLog.Info("starting health server", "address", probeAddr)
		if err := healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			setupLog.Error(err, "health server stopped with error")
		}
	}()
	setupLog.Info("health server ready")

	// Wait for shutdown signal
	<-ctx.Done()
	setupLog.Info("shutting down standalone mode")
	return nil
}

// coverage:ignore - initialization code, tested via E2E
func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

// nolint:gocyclo
// coverage:ignore - main entrypoint, tested via E2E
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var metricsAuth bool
	var enableHTTP2 bool
	var configFile string
	var noKubernetes bool
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&configFile, "config", "/etc/lumina/config.yaml",
		"Path to the controller configuration file. Can be overridden with LUMINA_CONFIG_PATH environment variable.")
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.BoolVar(&metricsAuth, "metrics-auth", false,
		"If set, the metrics endpoint requires authentication. "+
			"Use --metrics-auth=true to enable Kubernetes RBAC authentication.")
	flag.BoolVar(&noKubernetes, "no-kubernetes", false,
		"Run in standalone mode without Kubernetes integration. "+
			"Only AWS data collection and metrics will be available.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Allow environment variable to override config file path
	if envConfigPath := os.Getenv("LUMINA_CONFIG_PATH"); envConfigPath != "" {
		configFile = envConfigPath
	}

	// Load controller configuration
	// If config file doesn't exist, use empty config with defaults (for E2E tests)
	cfg, err := config.Load(configFile)
	if err != nil {
		if _, statErr := os.Stat(configFile); os.IsNotExist(statErr) {
			setupLog.Info("config file not found, using defaults", "config-file", configFile)
			cfg = &config.Config{}
		} else {
			setupLog.Error(err, "failed to load configuration", "config-file", configFile)
			os.Exit(1)
		}
	} else {
		setupLog.Info("loaded configuration",
			"accounts", len(cfg.AWSAccounts),
			"default-region", cfg.DefaultRegion,
			"log-level", cfg.LogLevel)
	}

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// If running in standalone mode, skip Kubernetes manager setup
	if noKubernetes {
		if err := runStandalone(cfg, metricsAddr, probeAddr, secureMetrics,
			metricsCertPath, metricsCertName, metricsCertKey, tlsOpts); err != nil {
			setupLog.Error(err, "standalone mode failed")
			os.Exit(1)
		}
		return
	}

	// Normal Kubernetes mode continues below...
	setupLog.Info("starting in Kubernetes mode")

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts
	webhookServerOptions := webhook.Options{
		TLSOpts: webhookTLSOpts,
	}

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		webhookServerOptions.CertDir = webhookCertPath
		webhookServerOptions.CertName = webhookCertName
		webhookServerOptions.KeyName = webhookCertKey
	}

	webhookServer := webhook.NewServer(webhookServerOptions)

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if metricsAuth {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "136aaa64.lumina.io",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Initialize Prometheus metrics
	// Metrics are registered with the controller-runtime registry and exposed
	// via the /metrics endpoint configured above. The ControllerRunning metric
	// is set to 1 to indicate the controller has successfully started.
	luminaMetrics := metrics.NewMetrics(ctrlmetrics.Registry)
	luminaMetrics.ControllerRunning.Set(1)
	setupLog.Info("metrics initialized and controller running metric set")

	// Get default account for non-account-specific AWS calls (pricing, etc)
	defaultAccount := cfg.GetDefaultAccount()

	// Create AWS client for controllers and health checks
	// This client handles credential management and AssumeRole operations
	awsClient, err := aws.NewClient(aws.ClientConfig{
		DefaultRegion: cfg.DefaultRegion,
		DefaultAccount: aws.AccountConfig{
			AccountID:     defaultAccount.AccountID,
			AssumeRoleARN: defaultAccount.AssumeRoleARN,
			Region:        defaultAccount.Region,
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to create AWS client")
		os.Exit(1)
	}
	setupLog.Info("created AWS client", "defaultAccount", defaultAccount.Name)

	if err := (&controller.NodeReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Node")
		os.Exit(1)
	}

	// Initialize RI/SP cache for Phase 2 data collection
	rispCache := cache.NewRISPCache()
	setupLog.Info("initialized RI/SP cache")

	// Initialize EC2 cache for Phase 4 data collection
	ec2Cache := cache.NewEC2Cache()
	setupLog.Info("initialized EC2 cache")

	// Initialize pricing cache
	pricingCache := cache.NewPricingCache()
	setupLog.Info("initialized pricing cache")

	// Create cost calculator (needed before initializing reconcilers)
	costCalculator := cost.NewCalculator(pricingCache, cfg)

	// Initialize all reconcilers using the helper function
	// This reduces code duplication between standalone and Kubernetes modes
	recs := initializeReconcilers(awsClient, cfg, rispCache, ec2Cache, pricingCache, luminaMetrics, costCalculator)

	// Start timer-based reconcilers as background goroutines
	// These don't benefit from controller-runtime's event-driven machinery
	// Using goroutines is simpler and cleaner than the ConfigMap workaround
	ctx := context.Background() // Manager handles lifecycle

	// Start pricing reconciler
	go func() {
		if err := recs.Pricing.RunStandalone(ctx); err != nil {
			setupLog.Error(err, "pricing reconciler stopped with error")
		}
	}()
	setupLog.Info("started pricing reconciler (goroutine)")

	// Start RISP reconciler - signals via channel when ready
	go func() {
		if err := recs.RISP.RunStandalone(ctx); err != nil {
			setupLog.Error(err, "RISP reconciler stopped with error")
		}
	}()
	setupLog.Info("started RISP reconciler (goroutine)")

	// Start SP rates reconciler - waits for RISP via channel
	go func() {
		if err := recs.SPRates.RunStandalone(ctx); err != nil {
			setupLog.Error(err, "SP rates reconciler stopped with error")
		}
	}()
	setupLog.Info("started SP rates reconciler (goroutine)")

	// Setup EC2 reconciler as event-driven controller
	// This watches Node resources and reconciles on changes
	if err := recs.EC2.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "EC2")
		os.Exit(1)
	}
	setupLog.Info("registered EC2 reconciler (event-driven)")

	// Create a context for the debouncer callbacks
	// We use context.Background() because the debouncer runs in the manager's lifecycle
	debouncerCtx := context.Background()

	// Create debouncer that triggers cost calculation 1 second after last cache update
	// This prevents "thundering herd" when EC2, RISP, and Pricing caches all update simultaneously
	recs.Cost.Debouncer = cache.NewDebouncer(1*time.Second, func() {
		if _, err := recs.Cost.Reconcile(debouncerCtx, ctrl.Request{}); err != nil {
			setupLog.Error(err, "cost calculation triggered by cache update failed")
		}
	})

	// Register cost reconciler with all caches
	// Any cache update will trigger the debouncer, which triggers cost recalculation
	ec2Cache.RegisterUpdateNotifier(recs.Cost.Debouncer.Trigger)
	rispCache.RegisterUpdateNotifier(recs.Cost.Debouncer.Trigger)
	pricingCache.RegisterUpdateNotifier(recs.Cost.Debouncer.Trigger)

	if err := recs.Cost.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Cost")
		os.Exit(1)
	}
	setupLog.Info("registered cost reconciler in event-driven mode (1s debounce)")

	// +kubebuilder:scaffold:builder

	// Setup health checks
	// The liveness probe (healthz) uses a simple ping check - if the process is running, it's alive.
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	// Create credential monitor for AWS health checks
	// The monitor runs background checks at the configured interval instead of on every healthz probe,
	// reducing AWS API calls from ~42/min to ~0.7/min (for 7 accounts with 10m interval) while still
	// detecting credential issues within the configured check interval.
	validator := aws.NewAccountValidator(awsClient)
	checkInterval := cfg.GetAccountValidationInterval()
	credMonitor := aws.NewCredentialMonitor(validator, cfg.AWSAccounts, checkInterval)
	credMonitor.Start()
	setupLog.Info("started AWS credential monitor",
		"accounts", len(cfg.AWSAccounts),
		"checkInterval", checkInterval)

	// The readiness probe (readyz) validates AWS account access using the credential monitor.
	// This ensures the controller doesn't receive traffic until all configured AWS accounts
	// are accessible. The health check reads from the monitor's cache, avoiding AWS API calls
	// on every probe (typically every 10 seconds).
	awsHealthChecker := aws.NewHealthChecker(credMonitor)
	if err := mgr.AddReadyzCheck("readyz", awsHealthChecker.Check); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
