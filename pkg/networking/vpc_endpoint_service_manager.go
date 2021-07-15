package networking

import (
	"context"

	ec2sdk "github.com/aws/aws-sdk-go/service/ec2"
	"github.com/go-logr/logr"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/aws/services"
)

type FetchVPCESInfoOptions struct {
	// whether to ignore cache and reload Endpoint Service Info from AWS directly.
	ReloadIgnoringCache bool
}

// Apply FetchVPCESInfoOption options
func (opts *FetchVPCESInfoOptions) ApplyOptions(options ...FetchVPCESInfoOption) {
	for _, option := range options {
		option(opts)
	}
}

type FetchVPCESInfoOption func(opts *FetchVPCESInfoOptions)

// WithReloadIgnoringCache is a option that sets the ReloadIgnoringCache to true.
func WithVPCESReloadIgnoringCache() FetchVPCESInfoOption {
	return func(opts *FetchVPCESInfoOptions) {
		opts.ReloadIgnoringCache = true
	}
}

// VPCEndpointServiceManager is an abstraction around EC2's VPC Endpoint Service API.
type VPCEndpointServiceManager interface {
	// FetchVPCESInfosByID will fetch VPCEndpointServiceInfo with EndpointService IDs.
	FetchVPCESInfosByID(ctx context.Context, esIDs []string, opts ...FetchVPCESInfoOption) (map[string]VPCEndpointServiceInfo, error)

	// FetchVPCESInfosByRequest will fetch VPCEndpointServiceInfo with raw DescribeVpcEndpointServiceConfigurationsInput request.
	FetchVPCESInfosByRequest(ctx context.Context, req *ec2sdk.DescribeVpcEndpointServiceConfigurationsInput) (map[string]VPCEndpointServiceInfo, error)
}

// NewDefaultVPCEndpointServiceManager constructs new defaultVPCEndpointServiceManager.
func NewDefaultVPCEndpointServiceManager(ec2Client services.EC2, logger logr.Logger) *defaultVPCEndpointServiceManager {
	return &defaultVPCEndpointServiceManager{
		ec2Client: ec2Client,
		logger:    logger,
	}
}

var _ VPCEndpointServiceManager = &defaultVPCEndpointServiceManager{}

// default implementation for VPCEndpointServiceManager
type defaultVPCEndpointServiceManager struct {
	ec2Client services.EC2
	logger    logr.Logger
}

func (m *defaultVPCEndpointServiceManager) FetchVPCESInfosByID(ctx context.Context, esIDs []string, opts ...FetchVPCESInfoOption) (map[string]VPCEndpointServiceInfo, error) {
	return nil, nil
}

func (m *defaultVPCEndpointServiceManager) FetchVPCESInfosByRequest(ctx context.Context, req *ec2sdk.DescribeVpcEndpointServiceConfigurationsInput) (map[string]VPCEndpointServiceInfo, error) {
	return nil, nil
}
