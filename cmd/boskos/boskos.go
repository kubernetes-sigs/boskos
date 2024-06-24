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
	"k8s.io/apimachinery/pkg/labels"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"sigs.k8s.io/prow/pkg/config"
	prowflagutil "sigs.k8s.io/prow/pkg/flagutil"
	"sigs.k8s.io/prow/pkg/interrupts"
	"sigs.k8s.io/prow/pkg/logrusutil"
	prowmetrics "sigs.k8s.io/prow/pkg/metrics"
	"sigs.k8s.io/prow/pkg/pjutil"
	"sigs.k8s.io/prow/pkg/pjutil/pprof"

	"sigs.k8s.io/boskos/common"
	"sigs.k8s.io/boskos/crds"
	"sigs.k8s.io/boskos/handlers"
	"sigs.k8s.io/boskos/leaderlabelreconciler"
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
	requestTTL           = flag.Duration("request-ttl", defaultRequestTTL, "request TTL before losing priority in the queue")
	logLevel             = flag.String("log-level", "info", fmt.Sprintf("Log level is one of %v.", logrus.AllLevels))
	namespace            = flag.String("namespace", corev1.NamespaceDefault, "namespace to install on")
	port                 = flag.Int("port", 8080, "Port to serve on")
	enableLeaderElection = flag.Bool("enable-leader-election", false, "If leader election should be enabled. If enabled, it is possible to run Boskos with multiple replicas and only the one that is leader will receive traffic. This works through boskos managing a label so it is only set on the leader pod. The boskos service must select on this label and not one that matches all replicas.")
	boskosLabelSelector  = flag.String("boskos-label-selector", "", "Must be set when leader election is used. The label selector to identify boskos pods")
	podName              = flag.String("pod-name", "", "The name of the current pod. Required when --enable-leader-election is set.")
	leaderLabelKey       = flag.String("leader-label-key", "boskos-leader", "The key of the label that will be used to denote the replica that is leader. The value of the label is always `true`")

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
	for _, o := range []prowflagutil.OptionGroup{&kubeClientOptions, &instrumentationOptions} {
		o.AddFlags(flag.CommandLine)
	}
	flag.Parse()

	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		logrus.WithError(err).Fatal("invalid log level specified")
	}
	logrus.SetLevel(level)
	for _, o := range []prowflagutil.OptionGroup{&kubeClientOptions, &instrumentationOptions} {
		if err := o.Validate(false); err != nil {
			logrus.Fatalf("Invalid options: %v", err)
		}
	}

	// collect data on mutex holders and blocking profiles
	runtime.SetBlockProfileRate(1)
	runtime.SetMutexProfileFraction(1)

	defer interrupts.WaitForGracefulShutdown()
	pprof.Instrument(instrumentationOptions)
	prowmetrics.ExposeMetrics("boskos", config.PushGateway{}, instrumentationOptions.MetricsPort)
	// signal to the world that we are healthy
	// this needs to be in a separate port as we don't start the
	// main server with the main mux until we're ready
	health := pjutil.NewHealthOnPort(instrumentationOptions.HealthPort)

	mgr, err := kubeClientOptions.Manager(*namespace, *enableLeaderElection, &crds.ResourceObject{}, &crds.DRLCObject{})
	if err != nil {
		logrus.WithError(err).Fatal("Failed to get mgr")
	}

	if *enableLeaderElection {
		if *podName == "" {
			logrus.Fatal("--pod-name must be set when --enable-leader-election is set")
		}
		if *boskosLabelSelector == "" {
			logrus.Fatal("--boskos-label-selector must be set when --enable-leader-election is set")
		}
		if *leaderLabelKey == "" {
			logrus.Fatal("--leader-label-key must be non-empty when --enable-leader-election is set")
		}
		selector, err := labels.Parse(*boskosLabelSelector)
		if err != nil {
			logrus.WithError(err).Fatalf("failed to parse %s as label selector", *boskosLabelSelector)
		}
		if err := leaderlabelreconciler.AddToManager(mgr, selector, *leaderLabelKey, *podName); err != nil {
			logrus.WithError(err).Fatal("Failed to construct leader label reconciler")
		}
	}

	storage := ranch.NewStorage(interrupts.Context(), mgr.GetClient(), *namespace)

	r, err := ranch.NewRanch(*configPath, storage, *requestTTL)
	if err != nil {
		logrus.WithError(err).Fatalf("failed to create ranch! Config: %v", *configPath)
	}

	boskos := &http.Server{
		Handler: traceHandler(handlers.NewBoskosHandler(r)),
		Addr:    fmt.Sprintf(":%d", *port),
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

	// Make sure config is not broken by syncing at least once. Also
	// needed for in memory mode where the controller never gets triggered.
	if err := syncConfig(); err != nil {
		logrus.WithError(err).Fatal("Failed to sync config")
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

func (r *configSyncReconciler) Reconcile(_ context.Context, _ reconcile.Request) (reconcile.Result, error) {
	// TODO(alvaroaleman): figure out how to use the context in the sync
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

	if err := ctrl.Watch(source.Kind(mgr.GetCache(), &crds.ResourceObject{}), constHandler(), resourceUpdatePredicate()); err != nil {
		return fmt.Errorf("failed to watch boskos resources: %w", err)
	}
	if err := ctrl.Watch(source.Kind(mgr.GetCache(), &crds.DRLCObject{}), constHandler()); err != nil {
		return fmt.Errorf("failed to watch boskos dynamic resources: %w", err)
	}
	if err := ctrl.Watch(&source.Channel{Source: configChangeEvent}, constHandler()); err != nil {
		return fmt.Errorf("failed to create source channel for config change event: %w", err)
	}

	return nil
}

func constHandler() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(
		func(_ context.Context, object ctrlruntimeclient.Object) []reconcile.Request {
			return []reconcile.Request{{}}
		})
}

// resourceUpdatePredicate prevents the config reconciler from reacting to resource update events
// except if:
//   - The new status is tombstone, because then we have to delete is
//   - The new owner is empty, because then we have to delete it if it got deleted from the config but
//     was not deleted from the api to let the current owner finish its work.
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
