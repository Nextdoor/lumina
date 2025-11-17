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
	"github.com/nextdoor/lumina/pkg/metrics"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

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

	// Create AWS client
	awsClient, err := aws.NewClient(aws.ClientConfig{
		DefaultRegion: cfg.DefaultRegion,
	})
	if err != nil {
		return err
	}
	setupLog.Info("created AWS client")

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

	// Create RISP reconciler for standalone mode
	// In standalone mode, we'll run it on a timer instead of K8s reconciliation
	// Regions will be read from cfg.Regions with account-specific overrides
	rispReconciler := &controller.RISPReconciler{
		AWSClient: awsClient,
		Config:    cfg,
		Cache:     rispCache,
		Metrics:   luminaMetrics,
		Log:       ctrl.Log.WithName("risp-reconciler"),
		Regions:   cfg.Regions,
	}

	// Create EC2 reconciler for standalone mode
	// EC2 reconciler runs every 5 minutes (more frequent than RI/SP hourly updates)
	ec2Reconciler := &controller.EC2Reconciler{
		AWSClient: awsClient,
		Config:    cfg,
		Cache:     ec2Cache,
		Metrics:   luminaMetrics,
		Log:       ctrl.Log.WithName("ec2-reconciler"),
		Regions:   cfg.Regions,
	}

	// Create pricing reconciler for standalone mode
	// Pricing reconciler runs every 24 hours (AWS pricing changes monthly)
	// IMPORTANT: This runs FIRST and BLOCKS until initial pricing data is loaded
	// Without pricing data, cost calculations will fail
	pricingReconciler := &controller.PricingReconciler{
		AWSClient:        awsClient,
		Config:           cfg,
		Cache:            pricingCache,
		Metrics:          luminaMetrics,
		Log:              ctrl.Log.WithName("pricing-reconciler"),
		Regions:          cfg.Regions,
		OperatingSystems: []string{"Linux", "Windows"},
	}

	// Start reconcilers in background goroutines
	ctx := ctrl.SetupSignalHandler()

	// Start pricing reconciler FIRST (blocking initial load)
	// This ensures pricing cache is populated before other reconcilers need it
	go func() {
		if err := pricingReconciler.RunStandalone(ctx); err != nil {
			setupLog.Error(err, "pricing reconciler stopped with error")
		}
	}()
	setupLog.Info("started pricing reconciler in standalone mode (initial load blocking)")

	go func() {
		if err := rispReconciler.RunStandalone(ctx); err != nil {
			setupLog.Error(err, "RISP reconciler stopped with error")
		}
	}()
	setupLog.Info("started RISP reconciler in standalone mode")

	go func() {
		if err := ec2Reconciler.RunStandalone(ctx); err != nil {
			setupLog.Error(err, "EC2 reconciler stopped with error")
		}
	}()
	setupLog.Info("started EC2 reconciler in standalone mode")

	// Create credential monitor for AWS health checks
	// The monitor runs background checks every 10 minutes instead of on every healthz probe,
	// reducing AWS API calls from ~42/min to ~0.7/min (for 7 accounts).
	validator := aws.NewAccountValidator(awsClient)
	credMonitor := aws.NewCredentialMonitor(validator, cfg.AWSAccounts, 10*time.Minute)
	credMonitor.Start()
	setupLog.Info("started AWS credential monitor",
		"accounts", len(cfg.AWSAccounts),
		"checkInterval", "10m")

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

	// Create AWS client for controllers and health checks
	// This client handles credential management and AssumeRole operations
	awsClient, err := aws.NewClient(aws.ClientConfig{
		DefaultRegion: cfg.DefaultRegion,
	})
	if err != nil {
		setupLog.Error(err, "unable to create AWS client")
		os.Exit(1)
	}
	setupLog.Info("created AWS client")

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

	// Setup RISP reconciler for hourly data collection
	// This reconciler queries AWS APIs for Reserved Instances and Savings Plans
	// and maintains an in-memory cache for cost calculation (future phases)
	// Regions will be read from cfg.Regions with account-specific overrides
	if err := (&controller.RISPReconciler{
		AWSClient: awsClient,
		Config:    cfg,
		Cache:     rispCache,
		Metrics:   luminaMetrics,
		Log:       ctrl.Log.WithName("risp-reconciler"),
		Regions:   cfg.Regions,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "RISP")
		os.Exit(1)
	}
	setupLog.Info("registered RISP reconciler for hourly data collection")

	// Setup EC2 reconciler for 5-minute data collection
	// This reconciler queries AWS APIs for EC2 instance inventory
	// and maintains an in-memory cache for cost calculation and metrics
	// Regions will be read from cfg.Regions with account-specific overrides
	if err := (&controller.EC2Reconciler{
		AWSClient: awsClient,
		Config:    cfg,
		Cache:     ec2Cache,
		Metrics:   luminaMetrics,
		Log:       ctrl.Log.WithName("ec2-reconciler"),
		Regions:   cfg.Regions,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "EC2")
		os.Exit(1)
	}
	setupLog.Info("registered EC2 reconciler for 5-minute data collection")

	// Setup pricing reconciler for 24-hour data collection
	// This reconciler bulk-loads all AWS EC2 pricing data and refreshes daily
	// Pricing data is required for cost calculations and changes infrequently (monthly)
	// Regions will be read from cfg.Regions
	if err := (&controller.PricingReconciler{
		AWSClient:        awsClient,
		Config:           cfg,
		Cache:            pricingCache,
		Metrics:          luminaMetrics,
		Log:              ctrl.Log.WithName("pricing-reconciler"),
		Regions:          cfg.Regions,
		OperatingSystems: []string{"Linux", "Windows"},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Pricing")
		os.Exit(1)
	}
	setupLog.Info("registered pricing reconciler for 24-hour data collection")

	// +kubebuilder:scaffold:builder

	// Setup health checks
	// The liveness probe (healthz) uses a simple ping check - if the process is running, it's alive.
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	// Create credential monitor for AWS health checks
	// The monitor runs background checks every 10 minutes instead of on every healthz probe,
	// reducing AWS API calls from ~42/min to ~0.7/min (for 7 accounts) while still detecting
	// credential issues within 10 minutes.
	validator := aws.NewAccountValidator(awsClient)
	credMonitor := aws.NewCredentialMonitor(validator, cfg.AWSAccounts, 10*time.Minute)
	credMonitor.Start()
	setupLog.Info("started AWS credential monitor",
		"accounts", len(cfg.AWSAccounts),
		"checkInterval", "10m")

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
