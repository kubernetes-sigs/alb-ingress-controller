package deploy

import (
	"context"
	"github.com/go-logr/logr"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/aws"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/config"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/deploy/ec2"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/deploy/elbv2"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/deploy/shield"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/deploy/tracking"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/deploy/wafregional"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/deploy/wafv2"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/model/core"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/networking"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// StackDeployer will deploy a resource stack into AWS and K8S.
type StackDeployer interface {
	// Deploy a resource stack.
	Deploy(ctx context.Context, stack core.Stack) error
}

// NewDefaultStackDeployer constructs new defaultStackDeployer.
func NewDefaultStackDeployer(cloud aws.Cloud, k8sClient client.Client,
	networkingSGManager networking.SecurityGroupManager, networkingSGReconciler networking.SecurityGroupReconciler,
	config config.ControllerConfig, tagPrefix string, logger logr.Logger) *defaultStackDeployer {

	trackingProvider := tracking.NewDefaultProvider(tagPrefix, config.ClusterName)
	ec2TaggingManager := ec2.NewDefaultTaggingManager(cloud.EC2(), networkingSGManager, cloud.VpcID(), logger)
	elbv2TaggingManager := elbv2.NewDefaultTaggingManager(cloud.ELBV2(), logger)

	return &defaultStackDeployer{
		cloud:                               cloud,
		k8sClient:                           k8sClient,
		addonsConfig:                        config.AddonsConfig,
		trackingProvider:                    trackingProvider,
		ec2ESManager:                        ec2.NewDefaultEndpointServiceManager(cloud.EC2(), cloud.VpcID(), logger),
		ec2TaggingManager:                   ec2TaggingManager,
		ec2SGManager:                        ec2.NewDefaultSecurityGroupManager(cloud.EC2(), trackingProvider, ec2TaggingManager, networkingSGReconciler, cloud.VpcID(), config.ExternalManagedTags, logger),
		elbv2TaggingManager:                 elbv2TaggingManager,
		elbv2LBManager:                      elbv2.NewDefaultLoadBalancerManager(cloud.ELBV2(), trackingProvider, elbv2TaggingManager, config.ExternalManagedTags, logger),
		elbv2LSManager:                      elbv2.NewDefaultListenerManager(cloud.ELBV2(), trackingProvider, elbv2TaggingManager, config.ExternalManagedTags, logger),
		elbv2LRManager:                      elbv2.NewDefaultListenerRuleManager(cloud.ELBV2(), trackingProvider, elbv2TaggingManager, config.ExternalManagedTags, logger),
		elbv2TGManager:                      elbv2.NewDefaultTargetGroupManager(cloud.ELBV2(), trackingProvider, elbv2TaggingManager, cloud.VpcID(), config.ExternalManagedTags, logger),
		elbv2TGBManager:                     elbv2.NewDefaultTargetGroupBindingManager(k8sClient, trackingProvider, logger),
		wafv2WebACLAssociationManager:       wafv2.NewDefaultWebACLAssociationManager(cloud.WAFv2(), logger),
		wafRegionalWebACLAssociationManager: wafregional.NewDefaultWebACLAssociationManager(cloud.WAFRegional(), logger),
		shieldProtectionManager:             shield.NewDefaultProtectionManager(cloud.Shield(), logger),
		vpcID:                               cloud.VpcID(),
		logger:                              logger,
	}
}

var _ StackDeployer = &defaultStackDeployer{}

// defaultStackDeployer is the default implementation for StackDeployer
type defaultStackDeployer struct {
	cloud                               aws.Cloud
	k8sClient                           client.Client
	addonsConfig                        config.AddonsConfig
	trackingProvider                    tracking.Provider
	ec2ESManager                        ec2.EndpointServiceManager
	ec2TaggingManager                   ec2.TaggingManager
	ec2SGManager                        ec2.SecurityGroupManager
	elbv2TaggingManager                 elbv2.TaggingManager
	elbv2LBManager                      elbv2.LoadBalancerManager
	elbv2LSManager                      elbv2.ListenerManager
	elbv2LRManager                      elbv2.ListenerRuleManager
	elbv2TGManager                      elbv2.TargetGroupManager
	elbv2TGBManager                     elbv2.TargetGroupBindingManager
	wafv2WebACLAssociationManager       wafv2.WebACLAssociationManager
	wafRegionalWebACLAssociationManager wafregional.WebACLAssociationManager
	shieldProtectionManager             shield.ProtectionManager
	vpcID                               string

	logger logr.Logger
}

type ResourceSynthesizer interface {
	Synthesize(ctx context.Context) error
	PostSynthesize(ctx context.Context) error
}

// Deploy a resource stack.
func (d *defaultStackDeployer) Deploy(ctx context.Context, stack core.Stack) error {
	synthesizers := []ResourceSynthesizer{
		ec2.NewSecurityGroupSynthesizer(d.cloud.EC2(), d.trackingProvider, d.ec2TaggingManager, d.ec2SGManager, d.vpcID, d.logger, stack),
		elbv2.NewTargetGroupSynthesizer(d.cloud.ELBV2(), d.trackingProvider, d.elbv2TaggingManager, d.elbv2TGManager, d.logger, stack),
		elbv2.NewLoadBalancerSynthesizer(d.cloud.ELBV2(), d.trackingProvider, d.elbv2TaggingManager, d.elbv2LBManager, d.logger, stack),
		elbv2.NewListenerSynthesizer(d.cloud.ELBV2(), d.elbv2TaggingManager, d.elbv2LSManager, d.logger, stack),
		elbv2.NewListenerRuleSynthesizer(d.cloud.ELBV2(), d.elbv2TaggingManager, d.elbv2LRManager, d.logger, stack),
		elbv2.NewTargetGroupBindingSynthesizer(d.k8sClient, d.trackingProvider, d.elbv2TGBManager, d.logger, stack),
	}

	if d.addonsConfig.WAFV2Enabled {
		synthesizers = append(synthesizers, wafv2.NewWebACLAssociationSynthesizer(d.wafv2WebACLAssociationManager, d.logger, stack))
	}
	if d.addonsConfig.WAFEnabled && d.cloud.WAFRegional().Available() {
		synthesizers = append(synthesizers, wafregional.NewWebACLAssociationSynthesizer(d.wafRegionalWebACLAssociationManager, d.logger, stack))
	}
	shieldNeeded := false
	if d.addonsConfig.ShieldEnabled {
		shieldNeeded, _ = d.cloud.Shield().Available()
	}
	if shieldNeeded {
		synthesizers = append(synthesizers, shield.NewProtectionSynthesizer(d.shieldProtectionManager, d.logger, stack))
	}
	if d.addonsConfig.EndpointServiceEnabled {
		synthesizers = append(synthesizers, ec2.NewEndpointServiceSynthesizer(d.cloud.EC2(), d.trackingProvider, d.ec2TaggingManager, d.ec2ESManager, d.vpcID, d.logger, stack))
	}

	for _, synthesizer := range synthesizers {
		if err := synthesizer.Synthesize(ctx); err != nil {
			return err
		}
	}
	for i := len(synthesizers) - 1; i >= 0; i-- {
		if err := synthesizers[i].PostSynthesize(ctx); err != nil {
			return err
		}
	}

	return nil
}
