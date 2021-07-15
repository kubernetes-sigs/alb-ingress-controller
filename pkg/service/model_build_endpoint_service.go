package service

import (
	"context"
)

func (t *defaultModelBuildTask) buildEndpointService(ctx context.Context) error {
	// return early if endpoint service annotations arent present

	// otherwise parse the annotations and create endpoint service via pkg/model/ec2.NewVPCEndpointService
	// which adds the resource to the core.stack
	return nil
}
