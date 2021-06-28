package targetgroupbinding

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	awssdk "github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	ec2sdk "github.com/aws/aws-sdk-go/service/ec2"
	elbv2sdk "github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/tools/record"
	elbv2api "sigs.k8s.io/aws-load-balancer-controller/apis/elbv2/v1beta1"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/aws/services"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/backend"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/k8s"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/networking"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const defaultTargetHealthRequeueDuration = 15 * time.Second

// ResourceManager manages the TargetGroupBinding resource.
type ResourceManager interface {
	Reconcile(ctx context.Context, tgb *elbv2api.TargetGroupBinding) error
	Cleanup(ctx context.Context, tgb *elbv2api.TargetGroupBinding) error
}

// NewDefaultResourceManager constructs new defaultResourceManager.
func NewDefaultResourceManager(k8sClient client.Client, elbv2Client services.ELBV2,
	podInfoRepo k8s.PodInfoRepo, podENIResolver networking.PodENIInfoResolver, nodeENIResolver networking.NodeENIInfoResolver,
	sgManager networking.SecurityGroupManager, sgReconciler networking.SecurityGroupReconciler,
	vpcID string, clusterName string, eventRecorder record.EventRecorder, logger logr.Logger, vpcInfoProvider networking.VPCInfoProvider) *defaultResourceManager {
	targetsManager := NewCachedTargetsManager(elbv2Client, logger)
	endpointResolver := backend.NewDefaultEndpointResolver(k8sClient, podInfoRepo, logger)
	networkingManager := NewDefaultNetworkingManager(k8sClient, podENIResolver, nodeENIResolver, sgManager, sgReconciler, vpcID, clusterName, logger)
	return &defaultResourceManager{
		k8sClient:         k8sClient,
		targetsManager:    targetsManager,
		endpointResolver:  endpointResolver,
		networkingManager: networkingManager,
		eventRecorder:     eventRecorder,
		logger:            logger,
		vpcInfoProvider:   vpcInfoProvider,

		targetHealthRequeueDuration: defaultTargetHealthRequeueDuration,
	}
}

var _ ResourceManager = &defaultResourceManager{}

// default implementation for ResourceManager.
type defaultResourceManager struct {
	k8sClient         client.Client
	targetsManager    TargetsManager
	endpointResolver  backend.EndpointResolver
	networkingManager NetworkingManager
	eventRecorder     record.EventRecorder
	logger            logr.Logger
	vpcInfoProvider   networking.VPCInfoProvider

	targetHealthRequeueDuration time.Duration
}

func (m *defaultResourceManager) Reconcile(ctx context.Context, tgb *elbv2api.TargetGroupBinding) error {
	if tgb.Spec.TargetType == nil {
		return errors.Errorf("targetType is not specified: %v", k8s.NamespacedName(tgb).String())
	}
	if *tgb.Spec.TargetType == elbv2api.TargetTypeIP {
		return m.reconcileWithIPTargetType(ctx, tgb)
	}
	return m.reconcileWithInstanceTargetType(ctx, tgb)
}

func (m *defaultResourceManager) Cleanup(ctx context.Context, tgb *elbv2api.TargetGroupBinding) error {
	if err := m.cleanupTargets(ctx, tgb); err != nil {
		return err
	}
	if err := m.networkingManager.Cleanup(ctx, tgb); err != nil {
		return err
	}
	return nil
}

func (m *defaultResourceManager) reconcileWithIPTargetType(ctx context.Context, tgb *elbv2api.TargetGroupBinding) error {
	svcKey := buildServiceReferenceKey(tgb, tgb.Spec.ServiceRef)

	targetHealthCondType := BuildTargetHealthPodConditionType(tgb)
	resolveOpts := []backend.EndpointResolveOption{
		backend.WithPodReadinessGate(targetHealthCondType),
	}
	endpoints, containsPotentialReadyEndpoints, err := m.endpointResolver.ResolvePodEndpoints(ctx, svcKey, tgb.Spec.ServiceRef.Port, resolveOpts...)
	if err != nil {
		if errors.Is(err, backend.ErrNotFound) {
			m.eventRecorder.Event(tgb, corev1.EventTypeWarning, k8s.TargetGroupBindingEventReasonBackendNotFound, err.Error())
			return m.Cleanup(ctx, tgb)
		}
		return err
	}

	tgARN := tgb.Spec.TargetGroupARN
	targets, err := m.targetsManager.ListTargets(ctx, tgARN)
	if err != nil {
		return err
	}
	notDrainingTargets, drainingTargets := partitionTargetsByDrainingStatus(targets)
	matchedEndpointAndTargets, unmatchedEndpoints, unmatchedTargets := matchPodEndpointWithTargets(endpoints, notDrainingTargets)

	if err := m.networkingManager.ReconcileForPodEndpoints(ctx, tgb, endpoints); err != nil {
		return err
	}
	if err := m.deregisterTargets(ctx, tgARN, unmatchedTargets); err != nil {
		return err
	}
	if err := m.registerPodEndpoints(ctx, tgARN, unmatchedEndpoints); err != nil {
		return err
	}

	anyPodNeedFurtherProbe, err := m.updateTargetHealthPodCondition(ctx, targetHealthCondType, matchedEndpointAndTargets, unmatchedEndpoints)
	if err != nil {
		return err
	}

	if anyPodNeedFurtherProbe {
		if containsTargetsInInitialState(matchedEndpointAndTargets) || len(unmatchedEndpoints) != 0 {
			return runtime.NewRequeueNeededAfter("monitor targetHealth", m.targetHealthRequeueDuration)
		}
		return runtime.NewRequeueNeeded("monitor targetHealth")
	}

	if containsPotentialReadyEndpoints {
		return runtime.NewRequeueNeeded("monitor potential ready endpoints")
	}

	_ = drainingTargets
	return nil
}

func (m *defaultResourceManager) reconcileWithInstanceTargetType(ctx context.Context, tgb *elbv2api.TargetGroupBinding) error {
	svcKey := buildServiceReferenceKey(tgb, tgb.Spec.ServiceRef)
	nodeSelector, err := backend.GetTrafficProxyNodeSelector(tgb)
	if err != nil {
		return err
	}

	resolveOpts := []backend.EndpointResolveOption{backend.WithNodeSelector(nodeSelector)}
	endpoints, err := m.endpointResolver.ResolveNodePortEndpoints(ctx, svcKey, tgb.Spec.ServiceRef.Port, resolveOpts...)
	if err != nil {
		if errors.Is(err, backend.ErrNotFound) {
			m.eventRecorder.Event(tgb, corev1.EventTypeWarning, k8s.TargetGroupBindingEventReasonBackendNotFound, err.Error())
			return m.Cleanup(ctx, tgb)
		}
		return err
	}
	tgARN := tgb.Spec.TargetGroupARN
	targets, err := m.targetsManager.ListTargets(ctx, tgARN)
	if err != nil {
		return err
	}
	notDrainingTargets, drainingTargets := partitionTargetsByDrainingStatus(targets)
	_, unmatchedEndpoints, unmatchedTargets := matchNodePortEndpointWithTargets(endpoints, notDrainingTargets)

	if err := m.networkingManager.ReconcileForNodePortEndpoints(ctx, tgb, endpoints); err != nil {
		return err
	}
	if err := m.deregisterTargets(ctx, tgARN, unmatchedTargets); err != nil {
		return err
	}
	if err := m.registerNodePortEndpoints(ctx, tgARN, unmatchedEndpoints); err != nil {
		return err
	}
	_ = drainingTargets
	return nil
}

func (m *defaultResourceManager) cleanupTargets(ctx context.Context, tgb *elbv2api.TargetGroupBinding) error {
	targets, err := m.targetsManager.ListTargets(ctx, tgb.Spec.TargetGroupARN)
	if err != nil {
		if isELBV2TargetGroupNotFoundError(err) {
			return nil
		}
		return err
	}
	if err := m.deregisterTargets(ctx, tgb.Spec.TargetGroupARN, targets); err != nil {
		if isELBV2TargetGroupNotFoundError(err) {
			return nil
		}
		return err
	}
	return nil
}

// updateTargetHealthPodCondition will updates pod's targetHealth condition for matchedEndpointAndTargets and unmatchedEndpoints.
// returns whether further probe is needed or not
func (m *defaultResourceManager) updateTargetHealthPodCondition(ctx context.Context, targetHealthCondType corev1.PodConditionType,
	matchedEndpointAndTargets []podEndpointAndTargetPair, unmatchedEndpoints []backend.PodEndpoint) (bool, error) {
	anyPodNeedFurtherProbe := false

	for _, endpointAndTarget := range matchedEndpointAndTargets {
		pod := endpointAndTarget.endpoint.Pod
		targetHealth := endpointAndTarget.target.TargetHealth
		needFurtherProbe, err := m.updateTargetHealthPodConditionForPod(ctx, pod, targetHealth, targetHealthCondType)
		if err != nil {
			return false, err
		}
		if needFurtherProbe {
			anyPodNeedFurtherProbe = true
		}
	}

	for _, endpoint := range unmatchedEndpoints {
		pod := endpoint.Pod
		targetHealth := &elbv2sdk.TargetHealth{
			State:       awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
			Reason:      awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
			Description: awssdk.String("Target registration is in progress"),
		}
		needFurtherProbe, err := m.updateTargetHealthPodConditionForPod(ctx, pod, targetHealth, targetHealthCondType)
		if err != nil {
			return false, err
		}
		if needFurtherProbe {
			anyPodNeedFurtherProbe = true
		}
	}
	return anyPodNeedFurtherProbe, nil
}

// updateTargetHealthPodConditionForPod updates pod's targetHealth condition for a single pod and its matched target.
// returns whether further probe is needed or not.
func (m *defaultResourceManager) updateTargetHealthPodConditionForPod(ctx context.Context, pod k8s.PodInfo,
	targetHealth *elbv2sdk.TargetHealth, targetHealthCondType corev1.PodConditionType) (bool, error) {
	if !pod.HasAnyOfReadinessGates([]corev1.PodConditionType{targetHealthCondType}) {
		return false, nil
	}

	targetHealthCondStatus := corev1.ConditionUnknown
	var reason, message string
	if targetHealth != nil {
		if awssdk.StringValue(targetHealth.State) == elbv2sdk.TargetHealthStateEnumHealthy {
			targetHealthCondStatus = corev1.ConditionTrue
		} else {
			targetHealthCondStatus = corev1.ConditionFalse
		}

		reason = awssdk.StringValue(targetHealth.Reason)
		message = awssdk.StringValue(targetHealth.Description)
	}
	needFurtherProbe := targetHealthCondStatus != corev1.ConditionTrue

	existingTargetHealthCond, exists := pod.GetPodCondition(targetHealthCondType)
	// we skip patch pod if it matches current computed status/reason/message.
	if exists &&
		existingTargetHealthCond.Status == targetHealthCondStatus &&
		existingTargetHealthCond.Reason == reason &&
		existingTargetHealthCond.Message == message {
		return needFurtherProbe, nil
	}

	newTargetHealthCond := corev1.PodCondition{
		Type:    targetHealthCondType,
		Status:  targetHealthCondStatus,
		Reason:  reason,
		Message: message,
	}
	if !exists || existingTargetHealthCond.Status != targetHealthCondStatus {
		newTargetHealthCond.LastTransitionTime = metav1.Now()
	}

	patch, err := buildPodConditionPatch(pod, newTargetHealthCond)
	if err != nil {
		return false, err
	}
	k8sPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pod.Key.Namespace,
			Name:      pod.Key.Name,
			UID:       pod.UID,
		},
	}
	if err := m.k8sClient.Status().Patch(ctx, k8sPod, patch); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	return needFurtherProbe, nil
}

func (m *defaultResourceManager) deregisterTargets(ctx context.Context, tgARN string, targets []TargetInfo) error {
	sdkTargets := make([]elbv2sdk.TargetDescription, 0, len(targets))
	for _, target := range targets {
		sdkTargets = append(sdkTargets, target.Target)
	}
	return m.targetsManager.DeregisterTargets(ctx, tgARN, sdkTargets)
}

func (m *defaultResourceManager) registerPodEndpoints(ctx context.Context, tgARN string, endpoints []backend.PodEndpoint) error {
	vpcID := m.networkingManager.VpcID()
	vpc, err := m.vpcInfoProvider.FetchVPCInfo(ctx, vpcID)
	if err != nil {
		return err
	}

	sdkTargets := make([]elbv2sdk.TargetDescription, 0, len(endpoints))
	for _, endpoint := range endpoints {
		target := elbv2sdk.TargetDescription{
			Id:   awssdk.String(endpoint.IP),
			Port: awssdk.Int64(endpoint.Port),
		}
		if !isELBV2TargetInELBVPC(endpoint.IP, vpc) {
			target.AvailabilityZone = awssdk.String("all")
		}
		sdkTargets = append(sdkTargets, target)
	}
	return m.targetsManager.RegisterTargets(ctx, tgARN, sdkTargets)
}

func (m *defaultResourceManager) registerNodePortEndpoints(ctx context.Context, tgARN string, endpoints []backend.NodePortEndpoint) error {
	sdkTargets := make([]elbv2sdk.TargetDescription, 0, len(endpoints))
	for _, endpoint := range endpoints {
		sdkTargets = append(sdkTargets, elbv2sdk.TargetDescription{
			Id:   awssdk.String(endpoint.InstanceID),
			Port: awssdk.Int64(endpoint.Port),
		})
	}
	return m.targetsManager.RegisterTargets(ctx, tgARN, sdkTargets)
}

type podEndpointAndTargetPair struct {
	endpoint backend.PodEndpoint
	target   TargetInfo
}

func partitionTargetsByDrainingStatus(targets []TargetInfo) ([]TargetInfo, []TargetInfo) {
	var notDrainingTargets []TargetInfo
	var drainingTargets []TargetInfo
	for _, target := range targets {
		if target.IsDraining() {
			drainingTargets = append(drainingTargets, target)
		} else {
			notDrainingTargets = append(notDrainingTargets, target)
		}
	}
	return notDrainingTargets, drainingTargets
}

func containsTargetsInInitialState(matchedEndpointAndTargets []podEndpointAndTargetPair) bool {
	for _, endpointAndTarget := range matchedEndpointAndTargets {
		if endpointAndTarget.target.IsInitial() {
			return true
		}
	}
	return false
}

func matchPodEndpointWithTargets(endpoints []backend.PodEndpoint, targets []TargetInfo) ([]podEndpointAndTargetPair, []backend.PodEndpoint, []TargetInfo) {
	var matchedEndpointAndTargets []podEndpointAndTargetPair
	var unmatchedEndpoints []backend.PodEndpoint
	var unmatchedTargets []TargetInfo

	endpointsByUID := make(map[string]backend.PodEndpoint, len(endpoints))
	for _, endpoint := range endpoints {
		endpointUID := fmt.Sprintf("%v:%v", endpoint.IP, endpoint.Port)
		endpointsByUID[endpointUID] = endpoint
	}
	targetsByUID := make(map[string]TargetInfo, len(targets))
	for _, target := range targets {
		targetUID := fmt.Sprintf("%v:%v", awssdk.StringValue(target.Target.Id), awssdk.Int64Value(target.Target.Port))
		targetsByUID[targetUID] = target
	}
	endpointUIDs := sets.StringKeySet(endpointsByUID)
	targetUIDs := sets.StringKeySet(targetsByUID)
	for _, uid := range endpointUIDs.Intersection(targetUIDs).List() {
		endpoint := endpointsByUID[uid]
		target := targetsByUID[uid]
		matchedEndpointAndTargets = append(matchedEndpointAndTargets, podEndpointAndTargetPair{
			endpoint: endpoint,
			target:   target,
		})
	}
	for _, uid := range endpointUIDs.Difference(targetUIDs).List() {
		unmatchedEndpoints = append(unmatchedEndpoints, endpointsByUID[uid])
	}
	for _, uid := range targetUIDs.Difference(endpointUIDs).List() {
		unmatchedTargets = append(unmatchedTargets, targetsByUID[uid])
	}
	return matchedEndpointAndTargets, unmatchedEndpoints, unmatchedTargets
}

type nodePortEndpointAndTargetPair struct {
	endpoint backend.NodePortEndpoint
	target   TargetInfo
}

func matchNodePortEndpointWithTargets(endpoints []backend.NodePortEndpoint, targets []TargetInfo) ([]nodePortEndpointAndTargetPair, []backend.NodePortEndpoint, []TargetInfo) {
	var matchedEndpointAndTargets []nodePortEndpointAndTargetPair
	var unmatchedEndpoints []backend.NodePortEndpoint
	var unmatchedTargets []TargetInfo

	endpointsByUID := make(map[string]backend.NodePortEndpoint, len(endpoints))
	for _, endpoint := range endpoints {
		endpointUID := fmt.Sprintf("%v:%v", endpoint.InstanceID, endpoint.Port)
		endpointsByUID[endpointUID] = endpoint
	}
	targetsByUID := make(map[string]TargetInfo, len(targets))
	for _, target := range targets {
		targetUID := fmt.Sprintf("%v:%v", awssdk.StringValue(target.Target.Id), awssdk.Int64Value(target.Target.Port))
		targetsByUID[targetUID] = target
	}
	endpointUIDs := sets.StringKeySet(endpointsByUID)
	targetUIDs := sets.StringKeySet(targetsByUID)
	for _, uid := range endpointUIDs.Intersection(targetUIDs).List() {
		endpoint := endpointsByUID[uid]
		target := targetsByUID[uid]
		matchedEndpointAndTargets = append(matchedEndpointAndTargets, nodePortEndpointAndTargetPair{
			endpoint: endpoint,
			target:   target,
		})
	}
	for _, uid := range endpointUIDs.Difference(targetUIDs).List() {
		unmatchedEndpoints = append(unmatchedEndpoints, endpointsByUID[uid])
	}
	for _, uid := range targetUIDs.Difference(endpointUIDs).List() {
		unmatchedTargets = append(unmatchedTargets, targetsByUID[uid])
	}
	return matchedEndpointAndTargets, unmatchedEndpoints, unmatchedTargets
}

func buildPodConditionPatch(pod k8s.PodInfo, condition corev1.PodCondition) (client.Patch, error) {
	oldData, err := json.Marshal(corev1.Pod{
		Status: corev1.PodStatus{
			Conditions: nil,
		},
	})
	if err != nil {
		return nil, err
	}
	newData, err := json.Marshal(corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: pod.UID}, // only put the uid in the new object to ensure it appears in the patch as a precondition
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{condition},
		},
	})
	if err != nil {
		return nil, err
	}
	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, corev1.Pod{})
	if err != nil {
		return nil, err
	}
	return client.RawPatch(types.StrategicMergePatchType, patchBytes), nil
}

func isELBV2TargetGroupNotFoundError(err error) bool {
	var awsErr awserr.Error
	if errors.As(err, &awsErr) {
		return awsErr.Code() == "TargetGroupNotFound"
	}
	return false
}

func isELBV2TargetInELBVPC(podIP string, vpc *ec2sdk.Vpc) bool {
	// Check if the pod IP is in the primary VPC CIDR.
	if isIPinCIDR(podIP, *vpc.CidrBlock) {
		return true
	}

	// Check if the pod IP is from a secondary CIDR block.
	for _, v := range vpc.CidrBlockAssociationSet {
		if isIPinCIDR(podIP, *v.CidrBlock) {
			return true
		}
	}

	// Cannot find pod IP in a VPC CIDR.
	return false
}

func isIPinCIDR(ipAddr, cidrBlock string) bool {
	_, cidr, _ := net.ParseCIDR(cidrBlock)
	ip := net.ParseIP(ipAddr)
	return cidr.Contains(ip)
}
