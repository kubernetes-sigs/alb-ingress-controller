module sigs.k8s.io/aws-load-balancer-controller

go 1.13

require (
	github.com/aws/aws-sdk-go v1.35.18
	github.com/fatih/color v1.10.0 // indirect
	github.com/go-logr/logr v0.1.0
	github.com/golang/mock v1.5.0
	github.com/google/go-cmp v0.4.0
	github.com/onsi/ginkgo v1.14.2
	github.com/onsi/gomega v1.10.1
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.0.0
	github.com/spf13/cobra v1.1.1 // indirect
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.7.0
	go.uber.org/zap v1.10.0
	golang.org/x/crypto v0.0.0-20200622213623-75b288015ac9 // indirect
	golang.org/x/sys v0.0.0-20210124154548-22da62e12c0c // indirect
	golang.org/x/time v0.0.0-20190308202827-9d24e82272b4
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	gomodules.xyz/jsonpatch/v2 v2.0.1
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
	helm.sh/helm/v3 v3.2.0
	k8s.io/api v0.18.6
	k8s.io/apimachinery v0.18.6
	k8s.io/cli-runtime v0.18.6
	k8s.io/client-go v0.18.6
	rsc.io/letsencrypt v0.0.3 // indirect
	sigs.k8s.io/controller-runtime v0.6.3
)

replace golang.org/x/sys => golang.org/x/sys v0.0.0-20200826173525-f9321e4c35a6
