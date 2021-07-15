package ec2

import (
	"context"

	"github.com/go-logr/logr"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/aws/services"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/deploy/tracking"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/model/core"
)

// NewEndpointServiceSynthesizer constructs new endpointServiceSynthesizer.
func NewEndpointServiceSynthesizer(ec2Client services.EC2, trackingProvider tracking.Provider, taggingManager TaggingManager,
	esManager EndpointServiceManager, vpcID string, logger logr.Logger, stack core.Stack) *endpointServiceSynthesizer {
	return &endpointServiceSynthesizer{
		ec2Client:        ec2Client,
		trackingProvider: trackingProvider,
		taggingManager:   taggingManager,
		esManager:        esManager,
		vpcID:            vpcID,
		logger:           logger,
		stack:            stack,
	}
}

type endpointServiceSynthesizer struct {
	ec2Client        services.EC2
	trackingProvider tracking.Provider
	taggingManager   TaggingManager
	esManager        EndpointServiceManager
	vpcID            string
	logger           logr.Logger

	stack core.Stack
}

func (s *endpointServiceSynthesizer) Synthesize(ctx context.Context) error {
	return nil
}

func (s *endpointServiceSynthesizer) PostSynthesize(ctx context.Context) error {
	return nil
}
