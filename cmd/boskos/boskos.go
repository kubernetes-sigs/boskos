/*
Copyright 2017 The Kubernetes Authors.

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

package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"k8s.io/test-infra/pkg/flagutil"
	"k8s.io/test-infra/prow/config"
	prowflagutil "k8s.io/test-infra/prow/flagutil"
	"k8s.io/test-infra/prow/interrupts"
	"k8s.io/test-infra/prow/logrusutil"
	prowmetrics "k8s.io/test-infra/prow/metrics"
	"k8s.io/test-infra/prow/pjutil"
	"sigs.k8s.io/boskos/common"
	"sigs.k8s.io/boskos/crds"
	"sigs.k8s.io/boskos/handlers"
	"sigs.k8s.io/boskos/metrics"
	"sigs.k8s.io/boskos/ranch"
)

const (
	defaultDynamicResourceUpdatePeriod = 10 * time.Minute
	defaultRequestTTL                  = 30 * time.Second
	defaultRequestGCPeriod             = time.Minute
)

var (
	configPath = flag.String("config", "config.yaml", "Path to init resource file")
	_          = flag.Duration("dynamic-resource-update-period", defaultDynamicResourceUpdatePeriod,
		"Legacy flag that does nothing but is kept for compatibility reasons")
	requestTTL = flag.Duration("request-ttl", defaultRequestTTL, "request TTL before losing priority in the queue")
	logLevel   = flag.String("log-level", "info", fmt.Sprintf("Log level is one of %v.", logrus.AllLevels))
	namespace  = flag.String("namespace", corev1.NamespaceDefault, "namespace to install on")

	httpRequestDuration = prowmetrics.HttpRequestDuration("boskos", 0.005, 1200)
	httpResponseSize    = prowmetrics.HttpResponseSize("boskos", 128, 65536)
	traceHandler        = prowmetrics.TraceHandler(handlers.NewBoskosSimplifier(), httpRequestDuration, httpResponseSize)

	kubeClientOptions      crds.KubernetesClientOptions
	instrumentationOptions prowflagutil.InstrumentationOptions
)

func init() {
	prometheus.MustRegister(httpRequestDuration)
	prometheus.MustRegister(httpResponseSize)
}

func main() {
	logrusutil.ComponentInit()
	for _, o := range []flagutil.OptionGroup{&kubeClientOptions, &instrumentationOptions} {
		o.AddFlags(flag.CommandLine)
	}
	flag.Parse()

	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		logrus.WithError(err).Fatal("invalid log level specified")
	}
	logrus.SetLevel(level)
	for _, o := range []flagutil.OptionGroup{&kubeClientOptions, &instrumentationOptions} {
		if err := o.Validate(false); err != nil {
			logrus.Fatalf("Invalid options: %v", err)
		}
	}

	// collect data on mutex holders and blocking profiles
	runtime.SetBlockProfileRate(1)
	runtime.SetMutexProfileFraction(1)

	defer interrupts.WaitForGracefulShutdown()
	pjutil.ServePProf(instrumentationOptions.PProfPort)
	prowmetrics.ExposeMetrics("boskos", config.PushGateway{}, instrumentationOptions.MetricsPort)
	// signal to the world that we are healthy
	// this needs to be in a separate port as we don't start the
	// main server with the main mux until we're ready
	health := pjutil.NewHealth()

	cfg, err := kubeClientOptions.Cfg()
	if err != nil {
		logrus.WithError(err).Fatal("Failed to get kubeconfig")
	}
	cfg.QPS = 100
	cfg.Burst = 200
	mgr, err := manager.New(cfg, manager.Options{
		LeaderElection:     false,
		Namespace:          *namespace,
		MetricsBindAddress: "0",
	})
	if err != nil {
		logrus.WithError(err).Fatal("Failed to construct mgr.")
	}
	interrupts.Run(func(ctx context.Context) {
		if err := mgr.Start(ctx.Done()); err != nil {
			logrus.WithError(err).Fatal("Mgr failed.")
		}
		logrus.Info("Mgr finished gracefully.")
	})

	storage := ranch.NewStorage(interrupts.Context(), mgr.GetClient(), *namespace)

	r, err := ranch.NewRanch(*configPath, storage, *requestTTL)
	if err != nil {
		logrus.WithError(err).Fatalf("failed to create ranch! Config: %v", *configPath)
	}

	boskos := &http.Server{
		Handler: traceHandler(handlers.NewBoskosHandler(r)),
		Addr:    ":8080",
	}

	// Viper defaults the configfile name to `config` and `SetConfigFile` only
	// has an effect when the configfile name is not an empty string, so we
	// just disable it entirely if there is no config.
	configChangeEventChan := make(chan event.GenericEvent)
	if *configPath != "" {
		v := viper.New()
		v.SetConfigFile(*configPath)
		v.SetConfigType("yaml")
		v.WatchConfig()
		v.OnConfigChange(func(in fsnotify.Event) {
			logrus.Info("Boskos config file changed, updating config.")
			configChangeEventChan <- event.GenericEvent{}
		})
	}

	syncConfig := func() error {
		return r.SyncConfig(*configPath)
	}
	if err := addConfigSyncReconcilerToManager(mgr, syncConfig, configChangeEventChan); err != nil {
		logrus.WithError(err).Fatal("Failed to set up config sync controller")
	}

	prometheus.MustRegister(metrics.NewResourcesCollector(r))
	r.StartRequestGC(defaultRequestGCPeriod)

	logrus.Info("Start Service")
	interrupts.ListenAndServe(boskos, 5*time.Second)

	// signal to the world that we're ready
	health.ServeReady()
}

type configSyncReconciler struct {
	sync func() error
}

func (r *configSyncReconciler) Reconcile(_ reconcile.Request) (reconcile.Result, error) {
	err := r.sync()
	if err != nil {
		logrus.WithError(err).Error("Config sync failed")
	}
	return reconcile.Result{}, err
}

func addConfigSyncReconcilerToManager(mgr manager.Manager, configSync func() error, configChangeEvent <-chan event.GenericEvent) error {
	ctrl, err := controller.New("bokos_config_reconciler", mgr, controller.Options{
		// We reconcile the whole config, hence this is not safe to run concurrently
		MaxConcurrentReconciles: 1,
		Reconciler: &configSyncReconciler{
			sync: configSync,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to construct controller: %w", err)
	}

	if err := ctrl.Watch(&source.Kind{Type: &crds.ResourceObject{}}, constHandler(), resourceUpdatePredicate()); err != nil {
		return fmt.Errorf("failed to watch boskos resources: %w", err)
	}
	if err := ctrl.Watch(&source.Kind{Type: &crds.DRLCObject{}}, constHandler()); err != nil {
		return fmt.Errorf("failed to watch boskos dyanmic resources: %w", err)
	}
	if err := ctrl.Watch(&source.Channel{Source: configChangeEvent}, constHandler()); err != nil {
		return fmt.Errorf("failed to create source channel for config change event: %w", err)
	}
	if err := mgr.Add(ctrl); err != nil {
		return fmt.Errorf("failed to add controller to manager: %w", err)
	}

	return nil
}

func constHandler() handler.EventHandler {
	return &handler.EnqueueRequestsFromMapFunc{
		ToRequests: handler.ToRequestsFunc(func(handler.MapObject) []reconcile.Request {
			return []reconcile.Request{{}}
		}),
	}
}

// resourceUpdatePredicate prevents the config reconciler from reacting to resource update events
// except if:
// * The new status is tombstone, because then we have to delete is
// * The new owner is empty, because then we have to delete it if it got deleted from the config but
//   was not deleted from the api to let the current owner finish its work.
func resourceUpdatePredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(_ event.CreateEvent) bool { return true },
		DeleteFunc: func(_ event.DeleteEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			resource, ok := e.ObjectNew.(*crds.ResourceObject)
			if !ok {
				panic(fmt.Sprintf("BUG: expected *crds.ResourceObject, got %T", e.ObjectNew))
			}

			return resource.Status.State == common.Tombstone || resource.Status.Owner == ""
		},
		GenericFunc: func(_ event.GenericEvent) bool { return true },
	}
}
