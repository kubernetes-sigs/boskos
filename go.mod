module sigs.k8s.io/boskos

go 1.14

replace (
	// Bazel is seemingly broken with newer versions of this package
	cloud.google.com/go => cloud.google.com/go v0.44.3
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v12.2.0+incompatible
	// Pin all k8s.io staging repositories to kubernetes v0.17.3 to match kubernetes/test-infra.
	k8s.io/api => k8s.io/api v0.17.3
	k8s.io/apimachinery => k8s.io/apimachinery v0.17.3
	k8s.io/client-go => k8s.io/client-go v0.17.3
	k8s.io/code-generator => k8s.io/code-generator v0.17.3
)

require (
	github.com/aws/aws-sdk-go v1.31.12
	github.com/fsnotify/fsnotify v1.4.7
	github.com/go-test/deep v1.0.4
	github.com/google/go-cmp v0.4.0
	github.com/google/uuid v1.1.1
	github.com/hashicorp/go-multierror v1.0.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.5.0
	github.com/sirupsen/logrus v1.6.0
	github.com/spf13/cobra v0.0.6
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.6.2
	k8s.io/api v0.17.3
	k8s.io/apimachinery v0.17.3
	k8s.io/client-go v9.0.0+incompatible
	k8s.io/test-infra v0.0.0-20200617221206-ea73eaeab7ff
	sigs.k8s.io/controller-runtime v0.5.4
	sigs.k8s.io/yaml v1.2.0
)
