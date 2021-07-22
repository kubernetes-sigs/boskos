module sigs.k8s.io/boskos

go 1.15

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v14.2.0+incompatible
	github.com/googleapis/gnostic => github.com/googleapis/gnostic v0.4.1
	// Pin all k8s.io staging repositories to kubernetes v0.19.3 to match kubernetes/test-infra.
	k8s.io/api => k8s.io/api v0.19.3
	k8s.io/apimachinery => k8s.io/apimachinery v0.19.3
	k8s.io/client-go => k8s.io/client-go v0.19.3
	k8s.io/code-generator => k8s.io/code-generator v0.19.3
)

require (
	github.com/aws/aws-sdk-go v1.37.22
	github.com/fsnotify/fsnotify v1.4.9
	github.com/go-test/deep v1.0.7
	github.com/google/go-cmp v0.5.4
	github.com/google/uuid v1.2.0
	github.com/hashicorp/go-multierror v1.1.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.9.0
	github.com/sirupsen/logrus v1.8.0
	github.com/spf13/cobra v1.1.3
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.7.1
	k8s.io/api v0.19.3
	k8s.io/apimachinery v0.19.3
	k8s.io/client-go v11.0.1-0.20190805182717-6502b5e7b1b5+incompatible
	k8s.io/test-infra v0.0.0-20201214190528-57362ae63e51
	sigs.k8s.io/controller-runtime v0.7.0-alpha.6.0.20201106193838-8d0107636985
	sigs.k8s.io/yaml v1.2.0
)
