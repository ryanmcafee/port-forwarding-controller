package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/ryanmcafee/port-forwarding-controller/pkg/apis"
	"github.com/ryanmcafee/port-forwarding-controller/pkg/controller"
	"github.com/ryanmcafee/port-forwarding-controller/pkg/forwarding"
	"github.com/ryanmcafee/port-forwarding-controller/pkg/unifi"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// newHTTPClient creates an HTTP client with optional TLS verification skip.
// When skipTLSVerification is "true" or "1", certificate verification is disabled.
func newHTTPClient(skipTLSVerification string) *http.Client {
	client := &http.Client{}
	if shouldSkipTLSVerification(skipTLSVerification) {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	return client
}

// shouldSkipTLSVerification returns true if TLS verification should be skipped.
func shouldSkipTLSVerification(value string) bool {
	v := strings.ToLower(value)
	return v == "true" || v == "1"
}

func main() {
	fmt.Printf("port-forwarding-controller version=%s commit=%s date=%s\n", version, commit, date)
	logf.SetLogger(zap.New())
	log := logf.Log.WithName("entrypoint")

	// Get a config to talk to the apiserver
	log.Info("setting up client for manager")
	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "unable to set up client config")
		os.Exit(1)
	}

	// Create a new Cmd to provide shared dependencies and start components
	log.Info("setting up manager")
	mgr, err := manager.New(cfg, manager.Options{})
	if err != nil {
		log.Error(err, "unable to set up overall controller manager")
		os.Exit(1)
	}

	log.Info("Registering Components.")

	// Setup Scheme for all resources
	log.Info("setting up scheme")
	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "unable add APIs to scheme")
		os.Exit(1)
	}

	// Setup all Controllers
	log.Info("Setting up controller")

	// Configure HTTP client with optional TLS skip verification
	skipTLS := os.Getenv("ROUTER_SKIP_TLS_VERIFICATION")
	if shouldSkipTLSVerification(skipTLS) {
		log.Info("TLS certificate verification disabled")
	}
	httpClient := newHTTPClient(skipTLS)

	// API key authentication takes priority over username/password
	apiKey := os.Getenv("ROUTER_API_KEY")
	if apiKey != "" {
		log.Info("Using API key authentication")
	}

	routerClient := unifi.Client{
		HTTPClient:    httpClient,
		ControllerURL: os.Getenv("ROUTER_URL"),
		Username:      os.Getenv("ROUTER_USERNAME"),
		Password:      os.Getenv("ROUTER_PASSWORD"),
		APIKey:        apiKey,
	}
	forwardingReconciler := forwarding.Reconciler{
		RouterClient: routerClient,
		RulePrefix:   os.Getenv("ROUTER_RULE_PREFIX"),
		Logger:       log,
	}

	if err := controller.AddToManager(mgr, forwardingReconciler); err != nil {
		log.Error(err, "unable to register controllers to the manager")
		os.Exit(1)
	}

	// Start the Cmd
	log.Info("Starting the Cmd.")
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Error(err, "unable to run the manager")
		os.Exit(1)
	}
}
