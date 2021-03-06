package elbv2

import (
	"context"
	"strings"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	elbv2api "sigs.k8s.io/aws-load-balancer-controller/apis/elbv2/v1beta1"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/k8s"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/webhook"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const apiPathValidateELBv2TargetGroupBinding = "/validate-elbv2-k8s-aws-v1beta1-targetgroupbinding"

// NewTargetGroupBindingValidator returns a mutator for TargetGroupBinding CRD.
func NewTargetGroupBindingValidator(k8sClient client.Client, logger logr.Logger) *targetGroupBindingValidator {
	return &targetGroupBindingValidator{
		k8sClient: k8sClient,
		logger:    logger,
	}
}

var _ webhook.Validator = &targetGroupBindingValidator{}

type targetGroupBindingValidator struct {
	k8sClient client.Client
	logger    logr.Logger
}

func (v *targetGroupBindingValidator) Prototype(_ admission.Request) (runtime.Object, error) {
	return &elbv2api.TargetGroupBinding{}, nil
}

func (v *targetGroupBindingValidator) ValidateCreate(ctx context.Context, obj runtime.Object) error {
	tgb := obj.(*elbv2api.TargetGroupBinding)
	if err := v.checkRequiredFields(tgb); err != nil {
		return err
	}
	if err := v.checkNodeSelector(tgb); err != nil {
		return err
	}
	if err := v.checkExistingTargetGroups(tgb); err != nil {
		return err
	}
	return nil
}

func (v *targetGroupBindingValidator) ValidateUpdate(ctx context.Context, obj runtime.Object, oldObj runtime.Object) error {
	tgb := obj.(*elbv2api.TargetGroupBinding)
	oldTgb := oldObj.(*elbv2api.TargetGroupBinding)
	if err := v.checkRequiredFields(tgb); err != nil {
		return err
	}
	if err := v.checkImmutableFields(tgb, oldTgb); err != nil {
		return err
	}
	if err := v.checkNodeSelector(tgb); err != nil {
		return err
	}
	return nil
}

func (v *targetGroupBindingValidator) ValidateDelete(ctx context.Context, obj runtime.Object) error {
	return nil
}

// checkRequiredFields will check required fields are not absent.
func (v *targetGroupBindingValidator) checkRequiredFields(tgb *elbv2api.TargetGroupBinding) error {
	var absentRequiredFields []string
	if tgb.Spec.TargetType == nil {
		absentRequiredFields = append(absentRequiredFields, "spec.targetType")
	}
	if len(absentRequiredFields) != 0 {
		return errors.Errorf("%s must specify these fields: %s", "TargetGroupBinding", strings.Join(absentRequiredFields, ","))
	}
	return nil
}

// checkImmutableFields will check immutable fields are not changed.
func (v *targetGroupBindingValidator) checkImmutableFields(tgb *elbv2api.TargetGroupBinding, oldTGB *elbv2api.TargetGroupBinding) error {
	var changedImmutableFields []string
	if tgb.Spec.TargetGroupARN != oldTGB.Spec.TargetGroupARN {
		changedImmutableFields = append(changedImmutableFields, "spec.targetGroupARN")
	}
	if (tgb.Spec.TargetType == nil) != (oldTGB.Spec.TargetType == nil) {
		changedImmutableFields = append(changedImmutableFields, "spec.targetType")
	}
	if tgb.Spec.TargetType != nil && oldTGB.Spec.TargetType != nil && (*tgb.Spec.TargetType) != (*oldTGB.Spec.TargetType) {
		changedImmutableFields = append(changedImmutableFields, "spec.targetType")
	}

	if len(changedImmutableFields) != 0 {
		return errors.Errorf("%s update may not change these fields: %s", "TargetGroupBinding", strings.Join(changedImmutableFields, ","))
	}
	return nil
}

// checkExistingTargetGroups will check for unique TargetGroup per TargetGroupBinding
func (v *targetGroupBindingValidator) checkExistingTargetGroups(tgb *elbv2api.TargetGroupBinding) error {
	ctx := context.Background()
	tgbList := elbv2api.TargetGroupBindingList{}
	if err := v.k8sClient.List(ctx, &tgbList); err != nil {
		return errors.Wrap(err, "failed to list TargetGroupBindings in the cluster")
	}
	for _, tgbObj := range tgbList.Items {
		if tgbObj.Spec.TargetGroupARN == tgb.Spec.TargetGroupARN {
			return errors.Errorf("TargetGroup %v is already bound to TargetGroupBinding %v", tgb.Spec.TargetGroupARN, k8s.NamespacedName(&tgbObj).String())
		}
	}
	return nil
}

//checkNodeSelector ensures that NodeSelector is only set when TargetType is ip
func (v *targetGroupBindingValidator) checkNodeSelector(tgb *elbv2api.TargetGroupBinding) error {
	if (*tgb.Spec.TargetType == elbv2api.TargetTypeIP) && (tgb.Spec.NodeSelector != nil) {
		return errors.Errorf("TargetGroupBinding cannot set NodeSelector when TargetType is ip")
	}
	return nil
}

// +kubebuilder:webhook:path=/validate-elbv2-k8s-aws-v1beta1-targetgroupbinding,mutating=false,failurePolicy=fail,groups=elbv2.k8s.aws,resources=targetgroupbindings,verbs=create;update,versions=v1beta1,name=vtargetgroupbinding.elbv2.k8s.aws,sideEffects=None,webhookVersions=v1,admissionReviewVersions=v1beta1

func (v *targetGroupBindingValidator) SetupWithManager(mgr ctrl.Manager) {
	mgr.GetWebhookServer().Register(apiPathValidateELBv2TargetGroupBinding, webhook.ValidatingWebhookForValidator(v))
}
