module sigs.k8s.io/boskos

go 1.15

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v14.2.0+incompatible
	github.com/googleapis/gnostic => github.com/googleapis/gnostic v0.4.1
	k8s.io/client-go => k8s.io/client-go v0.21.1
	k8s.io/code-generator => k8s.io/code-generator v0.21.1
)

require (
	github.com/IBM-Cloud/power-go-client v1.1.4
	github.com/IBM/go-sdk-core/v5 v5.9.3
	github.com/IBM/platform-services-go-sdk v0.19.4
	github.com/aws/aws-sdk-go v1.37.22
	github.com/fsnotify/fsnotify v1.5.1
	github.com/go-test/deep v1.0.7
	github.com/google/go-cmp v0.5.7
	github.com/google/uuid v1.3.0
	github.com/hashicorp/go-multierror v1.1.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.11.0
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/cobra v1.4.0
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.8.1
	k8s.io/api v0.21.1
	k8s.io/apimachinery v0.21.1
	k8s.io/client-go v11.0.1-0.20190805182717-6502b5e7b1b5+incompatible
	k8s.io/test-infra v0.0.0-20210730160938-8ad9b8c53bd8
	sigs.k8s.io/controller-runtime v0.9.0
	sigs.k8s.io/yaml v1.3.0
)
