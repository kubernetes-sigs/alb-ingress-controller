package config

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"
	"net"
	"regexp"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/aws"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/inject"
)

const (
	flagLogLevel                                  = "log-level"
	flagK8sClusterName                            = "cluster-name"
	flagDefaultTags                               = "default-tags"
	flagServiceMaxConcurrentReconciles            = "service-max-concurrent-reconciles"
	flagTargetGroupBindingMaxConcurrentReconciles = "targetgroupbinding-max-concurrent-reconciles"
	flagDefaultSSLPolicy                          = "default-ssl-policy"
	flagWatchIPBlocks                             = "watch-ip-blocks"
	flagWatchInstanceFilters                      = "watch-instance-filters"
	defaultLogLevel                               = "info"
	defaultMaxConcurrentReconciles                = 3
	defaultSSLPolicy                              = "ELBSecurityPolicy-2016-08"
)

// ControllerConfig contains the controller configuration
type ControllerConfig struct {
	// Log level for the controller logs
	LogLevel string
	// Name of the Kubernetes cluster
	ClusterName string
	// Configurations for AWS.
	AWSConfig aws.CloudConfig
	// Configurations for the Controller Runtime
	RuntimeConfig RuntimeConfig
	// Configurations for Pod inject webhook
	PodWebhookConfig inject.Config
	// Configurations for the Ingress controller
	IngressConfig IngressConfig
	// Configurations for Addons feature
	AddonsConfig AddonsConfig

	// Default AWS Tags that will be applied to all AWS resources managed by this controller.
	DefaultTags map[string]string

	// Default SSL Policy that will be applied to all ingresses or services that do not have
	// the SSL Policy annotation.
	DefaultSSLPolicy string

	// Max concurrent reconcile loops for Service objects
	ServiceMaxConcurrentReconciles int
	// Max concurrent reconcile loops for TargetGroupBinding objects
	TargetGroupBindingMaxConcurrentReconciles int
	// IP blocks in CIDR notation
	WatchIPBlocks []string
	// AWS Filters to filter instances
	WatchInstanceFilters []string
}

// BindFlags binds the command line flags to the fields in the config object
func (cfg *ControllerConfig) BindFlags(fs *pflag.FlagSet) {
	fs.StringVar(&cfg.LogLevel, flagLogLevel, defaultLogLevel,
		"Set the controller log level - info(default), debug")
	fs.StringVar(&cfg.ClusterName, flagK8sClusterName, "", "Kubernetes cluster name")
	fs.StringToStringVar(&cfg.DefaultTags, flagDefaultTags, nil,
		"Default AWS Tags that will be applied to all AWS resources managed by this controller")
	fs.IntVar(&cfg.ServiceMaxConcurrentReconciles, flagServiceMaxConcurrentReconciles, defaultMaxConcurrentReconciles,
		"Maximum number of concurrently running reconcile loops for service")
	fs.IntVar(&cfg.TargetGroupBindingMaxConcurrentReconciles, flagTargetGroupBindingMaxConcurrentReconciles, defaultMaxConcurrentReconciles,
		"Maximum number of concurrently running reconcile loops for targetGroupBinding")
	fs.StringVar(&cfg.DefaultSSLPolicy, flagDefaultSSLPolicy, defaultSSLPolicy,
		"Default SSL policy for load balancers listeners")
	fs.StringSliceVar(&cfg.WatchIPBlocks, flagWatchIPBlocks, nil,
		"When using TargetType: ip, you can specify IP blocks in CIDR notation to only list (from AWS) ip "+
			"targets that fall within their ranges.")
	fs.StringSliceVar(&cfg.WatchInstanceFilters, flagWatchInstanceFilters, nil,
		"When using TargetType: instance, you can specify filters to only list (from AWS) instance targets that "+
			"match the specified filters.")

	cfg.AWSConfig.BindFlags(fs)
	cfg.RuntimeConfig.BindFlags(fs)

	cfg.PodWebhookConfig.BindFlags(fs)
	cfg.IngressConfig.BindFlags(fs)
	cfg.AddonsConfig.BindFlags(fs)
}

// Validate the controller configuration
func (cfg *ControllerConfig) Validate() error {
	if len(cfg.ClusterName) == 0 {
		return errors.New("kubernetes cluster name must be specified")
	}
	if len(cfg.WatchIPBlocks) > 0 {
		for _, cidr := range cfg.WatchIPBlocks {
			if _, _, err := net.ParseCIDR(cidr); err != nil {
				parseError := err.(*net.ParseError)
				return errors.New(fmt.Sprintf("CIDR provider is invalid: %s", parseError.Text))
			}
		}
	}
	if len(cfg.WatchInstanceFilters) > 0 {
		for _, filter := range cfg.WatchInstanceFilters {
			if match, err := regexp.MatchString("^\\w+=\\w+$", filter); !match || err != nil {
				return errors.New(fmt.Sprintf("Filter is invalid: %s", filter))
			}
		}
	}

	return nil
}
