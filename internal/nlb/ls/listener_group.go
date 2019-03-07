package ls

import (
	"context"

	"github.com/kubernetes-sigs/aws-alb-ingress-controller/internal/albctx"

	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/kubernetes-sigs/aws-alb-ingress-controller/internal/aws"
	"github.com/kubernetes-sigs/aws-alb-ingress-controller/internal/ingress/controller/store"
	"github.com/kubernetes-sigs/aws-alb-ingress-controller/internal/k8s"
	"github.com/kubernetes-sigs/aws-alb-ingress-controller/internal/nlb/tg"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

type GroupController interface {
	// Reconcile ensures listeners exists in LB to satisfy ingress requirements.
	Reconcile(ctx context.Context, lbArn string, service *corev1.Service, tgGroup tg.TargetGroupGroup) error

	// Delete ensures all listeners are deleted
	Delete(ctx context.Context, lbArn string) error
}

func NewGroupController(store store.Storer, cloud aws.CloudAPI) GroupController {
	lsController := NewController(cloud)
	return &defaultGroupController{
		cloud:        cloud,
		store:        store,
		lsController: lsController,
	}
}

type defaultGroupController struct {
	cloud aws.CloudAPI
	store store.Storer

	lsController Controller
}

func (controller *defaultGroupController) Reconcile(ctx context.Context, lbArn string, service *corev1.Service, tgGroup tg.TargetGroupGroup) error {
	serviceAnnos, err := controller.store.GetServiceAnnotations(k8s.MetaNamespaceKey(service), nil)
	if err != nil {
		return err
	}
	instancesByPort, err := controller.loadListenerInstances(ctx, lbArn)
	if err != nil {
		return err
	}

	portsInUse := sets.NewInt64()
	for _, port := range serviceAnnos.LoadBalancer.Ports {
		portsInUse.Insert(port.Port)
		instance := instancesByPort[port.Port]
		if err := controller.lsController.Reconcile(ctx, ReconcileOptions{
			LBArn:        lbArn,
			Service:      service,
			ServiceAnnos: serviceAnnos,
			Port:         port,
			TGGroup:      tgGroup,
			Instance:     instance,
		}); err != nil {
			return err
		}
	}
	portsUnsed := sets.Int64KeySet(instancesByPort).Difference(portsInUse)
	for port := range portsUnsed {
		instance := instancesByPort[port]
		albctx.GetLogger(ctx).Infof("deleting listener %v, arn: %v", aws.Int64Value(instance.Port), aws.StringValue(instance.ListenerArn))
		if err := controller.cloud.DeleteListenersByArn(ctx, aws.StringValue(instance.ListenerArn)); err != nil {
			return err
		}
	}
	return nil
}

func (controller *defaultGroupController) Delete(ctx context.Context, lbArn string) error {
	instancesByPort, err := controller.loadListenerInstances(ctx, lbArn)
	if err != nil {
		return err
	}
	for _, instance := range instancesByPort {
		albctx.GetLogger(ctx).Infof("deleting listener %v, arn: %v", aws.Int64Value(instance.Port), aws.StringValue(instance.ListenerArn))
		if err := controller.cloud.DeleteListenersByArn(ctx, aws.StringValue(instance.ListenerArn)); err != nil {
			return err
		}
	}
	return nil
}

func (controller *defaultGroupController) loadListenerInstances(ctx context.Context, lbArn string) (map[int64]*elbv2.Listener, error) {
	instances, err := controller.cloud.ListListenersByLoadBalancer(ctx, lbArn)
	if err != nil {
		return nil, err
	}
	instanceByPort := make(map[int64]*elbv2.Listener)
	for _, instance := range instances {
		instanceByPort[aws.Int64Value(instance.Port)] = instance
	}
	return instanceByPort, nil
}
