package config

import "github.com/spf13/pflag"

const (
	flagWAFEnabled             = "enable-waf"
	flagWAFV2Enabled           = "enable-wafv2"
	flagShieldEnabled          = "enable-shield"
	flagEndpointServiceEnabled = "enable-endpoint-service"
	defaultEnabled             = true
)

// AddonsConfig contains configuration for the addon features
type AddonsConfig struct {
	// WAF addon for ALB
	WAFEnabled bool
	// WAFV2 addon for ALB
	WAFV2Enabled bool
	// Shield addon for ALB
	ShieldEnabled bool
	// Endpoint Service addon for NLB
	EndpointServiceEnabled bool
}

// BindFlags binds the command line flags to the fields in the config object
func (f *AddonsConfig) BindFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&f.WAFEnabled, flagWAFEnabled, defaultEnabled, "Enable WAF addon for ALB")
	fs.BoolVar(&f.WAFV2Enabled, flagWAFV2Enabled, defaultEnabled, "Enable WAF V2 addon for ALB")
	fs.BoolVar(&f.ShieldEnabled, flagShieldEnabled, defaultEnabled, "Enable Shield addon for ALB")
	fs.BoolVar(&f.EndpointServiceEnabled, flagEndpointServiceEnabled, defaultEnabled, "Enable VPC Endpoint Service addon for NLB")
}
