package ec2

import (
	"context"

	"github.com/go-logr/logr"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/aws/services"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/deploy/tracking"
	ec2model "sigs.k8s.io/aws-load-balancer-controller/pkg/model/ec2"
)

// abstraction around endpoint service operations for EC2.
type EndpointServiceManager interface {
	// ReconcileTags will reconcile tags on resources.
	ReconcileTags(ctx context.Context, resID string, desiredTags map[string]string, opts ...ReconcileTagsOption) error

	// ListEndpointServices returns VPC Endpoint Services that matches any of the tagging requirements.
	ListEndpointServices(ctx context.Context, tagFilters ...tracking.TagFilter) ([]ec2model.VPCEndpointService, error)
}

// NewdefaultEndpointServiceManager constructs new defaultEndpointServiceManager.
func NewDefaultEndpointServiceManager(ec2Client services.EC2, vpcID string, logger logr.Logger) *defaultEndpointServiceManager {
	return &defaultEndpointServiceManager{
		ec2Client: ec2Client,
		vpcID:     vpcID,
		logger:    logger,
	}
}

var _ EndpointServiceManager = &defaultEndpointServiceManager{}

// default implementation for EndpointServiceManager.
type defaultEndpointServiceManager struct {
	ec2Client services.EC2
	vpcID     string
	logger    logr.Logger
}

func (m *defaultEndpointServiceManager) ReconcileTags(ctx context.Context, resID string, desiredTags map[string]string, opts ...ReconcileTagsOption) error {
	return nil
}

func (m *defaultEndpointServiceManager) ListEndpointServices(ctx context.Context, tagFilters ...tracking.TagFilter) ([]ec2model.VPCEndpointService, error) {
	return nil, nil
}
