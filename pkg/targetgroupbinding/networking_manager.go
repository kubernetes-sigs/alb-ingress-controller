package targetgroupbinding

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"

	awssdk "github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	ec2sdk "github.com/aws/aws-sdk-go/service/ec2"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	elbv2api "sigs.k8s.io/aws-load-balancer-controller/apis/elbv2/v1beta1"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/backend"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/k8s"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/networking"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	tgbNetworkingIPPermissionLabelKey   = "elbv2.k8s.aws/targetGroupBinding"
	tgbNetworkingIPPermissionLabelValue = "shared"
)

// NetworkingManager manages the networking for targetGroupBindings.
type NetworkingManager interface {
	// ReconcileForPodEndpoints reconcile network settings for TargetGroupBindings with podEndpoints.
	ReconcileForPodEndpoints(ctx context.Context, tgb *elbv2api.TargetGroupBinding, endpoints []backend.PodEndpoint) error

	// ReconcileForNodePortEndpoints reconcile network settings for TargetGroupBindings with nodePortEndpoints.
	ReconcileForNodePortEndpoints(ctx context.Context, tgb *elbv2api.TargetGroupBinding, endpoints []backend.NodePortEndpoint) error

	// Cleanup reconcile network settings for TargetGroupBindings with zero endpoints.
	Cleanup(ctx context.Context, tgb *elbv2api.TargetGroupBinding) error

	// VpcID returns the ID of the VPC the ELB is attached to.
	VpcID() string
}

// NewDefaultNetworkingManager constructs defaultNetworkingManager.
func NewDefaultNetworkingManager(k8sClient client.Client, podENIResolver networking.PodENIInfoResolver, nodeENIResolver networking.NodeENIInfoResolver,
	sgManager networking.SecurityGroupManager, sgReconciler networking.SecurityGroupReconciler, vpcID string, clusterName string, logger logr.Logger) *defaultNetworkingManager {

	return &defaultNetworkingManager{
		k8sClient:       k8sClient,
		podENIResolver:  podENIResolver,
		nodeENIResolver: nodeENIResolver,
		sgManager:       sgManager,
		sgReconciler:    sgReconciler,
		vpcID:           vpcID,
		clusterName:     clusterName,
		logger:          logger,

		mutex:                         sync.Mutex{},
		ingressPermissionsPerSGByTGB:  make(map[types.NamespacedName]map[string][]networking.IPPermissionInfo),
		trackedEndpointSGs:            sets.NewString(),
		trackedEndpointSGsInitialized: false,
	}
}

// default implementation for NetworkingManager.
type defaultNetworkingManager struct {
	k8sClient       client.Client
	podENIResolver  networking.PodENIInfoResolver
	nodeENIResolver networking.NodeENIInfoResolver
	sgManager       networking.SecurityGroupManager
	sgReconciler    networking.SecurityGroupReconciler
	vpcID           string
	clusterName     string
	logger          logr.Logger

	// mutex will serialize our TargetGroup's networking reconcile requests.
	mutex sync.Mutex
	// ingressPermissionsPerSGByTGB are calculated ingress permissions per SecurityGroup needed by each TargetGroupBindings.
	ingressPermissionsPerSGByTGB map[types.NamespacedName]map[string][]networking.IPPermissionInfo
	// trackedEndpointSGs are the full set of endpoint securityGroups that we have managed inbound rules to satisfying
	// targetGroupBinding's network requirements.
	// we'll GC inbound rules from these securityGroups if it's no longer needed by TargetGroupBindings.
	trackedEndpointSGs sets.String
	// whether we have initialized trackedEndpointSGs from AWS.
	// we discovery endpointSGs from VPC using clusterTags once, so we can still GC rules if some SGs are no longer referenced.
	// a SG/nodeGroup might be removed from cluster while this controller is not running.
	trackedEndpointSGsInitialized bool
}

func (m *defaultNetworkingManager) ReconcileForPodEndpoints(ctx context.Context, tgb *elbv2api.TargetGroupBinding, endpoints []backend.PodEndpoint) error {
	var ingressPermissionsPerSG map[string][]networking.IPPermissionInfo
	if tgb.Spec.Networking != nil {
		var err error
		ingressPermissionsPerSG, err = m.computeIngressPermissionsPerSGWithPodEndpoints(ctx, *tgb.Spec.Networking, endpoints)
		if err != nil {
			return err
		}
	}
	return m.reconcileWithIngressPermissionsPerSG(ctx, tgb, ingressPermissionsPerSG)
}

func (m *defaultNetworkingManager) ReconcileForNodePortEndpoints(ctx context.Context, tgb *elbv2api.TargetGroupBinding, endpoints []backend.NodePortEndpoint) error {
	var ingressPermissionsPerSG map[string][]networking.IPPermissionInfo
	if tgb.Spec.Networking != nil {
		var err error
		ingressPermissionsPerSG, err = m.computeIngressPermissionsPerSGWithNodePortEndpoints(ctx, *tgb.Spec.Networking, endpoints)
		if err != nil {
			return err
		}
	}
	return m.reconcileWithIngressPermissionsPerSG(ctx, tgb, ingressPermissionsPerSG)
}

func (m *defaultNetworkingManager) Cleanup(ctx context.Context, tgb *elbv2api.TargetGroupBinding) error {
	return m.reconcileWithIngressPermissionsPerSG(ctx, tgb, nil)
}

func (m *defaultNetworkingManager) VpcID() string {
	return m.vpcID
}

func (m *defaultNetworkingManager) computeIngressPermissionsPerSGWithPodEndpoints(ctx context.Context, tgbNetworking elbv2api.TargetGroupBindingNetworking, endpoints []backend.PodEndpoint) (map[string][]networking.IPPermissionInfo, error) {
	pods := make([]k8s.PodInfo, 0, len(endpoints))
	podByPodKey := make(map[types.NamespacedName]k8s.PodInfo, len(endpoints))
	for _, endpoint := range endpoints {
		pods = append(pods, endpoint.Pod)
		podByPodKey[endpoint.Pod.Key] = endpoint.Pod
	}
	eniInfoByPodKey, err := m.podENIResolver.Resolve(ctx, pods)
	if err != nil {
		return nil, err
	}

	podsBySG := make(map[string][]k8s.PodInfo)
	for podKey, eniInfo := range eniInfoByPodKey {
		sgID, err := m.resolveEndpointSGForENI(ctx, eniInfo)
		if err != nil {
			return nil, err
		}
		pod := podByPodKey[podKey]
		podsBySG[sgID] = append(podsBySG[sgID], pod)
	}

	permissionsPerSG := make(map[string][]networking.IPPermissionInfo, len(podsBySG))
	for sgID, pods := range podsBySG {
		permissions, err := m.computeIngressPermissionsForTGBNetworking(ctx, tgbNetworking, pods)
		if err != nil {
			return nil, err
		}
		permissionsPerSG[sgID] = permissions
	}
	return permissionsPerSG, nil
}

func (m *defaultNetworkingManager) computeIngressPermissionsPerSGWithNodePortEndpoints(ctx context.Context, tgbNetworking elbv2api.TargetGroupBindingNetworking, endpoints []backend.NodePortEndpoint) (map[string][]networking.IPPermissionInfo, error) {
	nodes := make([]*corev1.Node, 0, len(endpoints))
	for _, endpoint := range endpoints {
		nodes = append(nodes, endpoint.Node)
	}
	eniInfoByNodeKey, err := m.nodeENIResolver.Resolve(ctx, nodes)
	if err != nil {
		return nil, err
	}
	sgIDs := sets.NewString()
	for _, eniInfo := range eniInfoByNodeKey {
		sgID, err := m.resolveEndpointSGForENI(ctx, eniInfo)
		if err != nil {
			return nil, err
		}
		sgIDs.Insert(sgID)
	}
	permissions, err := m.computeIngressPermissionsForTGBNetworking(ctx, tgbNetworking, nil)
	if err != nil {
		return nil, err
	}

	permissionsPerSG := make(map[string][]networking.IPPermissionInfo, len(sgIDs))
	for sgID := range sgIDs {
		permissionsPerSG[sgID] = permissions
	}
	return permissionsPerSG, nil
}

func (m *defaultNetworkingManager) reconcileWithIngressPermissionsPerSG(ctx context.Context, tgb *elbv2api.TargetGroupBinding, ingressPermissionsPerSG map[string][]networking.IPPermissionInfo) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	tgbKey := k8s.NamespacedName(tgb)
	m.ingressPermissionsPerSGByTGB[tgbKey] = ingressPermissionsPerSG
	endpointSGs := sets.StringKeySet(ingressPermissionsPerSG).List()
	m.trackEndpointSGs(ctx, endpointSGs...)

	tgbsWithNetworking, err := m.fetchTGBsWithNetworking(ctx)
	if err != nil {
		return err
	}
	computedForAllTGBs := m.consolidateIngressPermissionsPerSGByTGB(ctx, tgbsWithNetworking)
	aggregatedIngressPermissionsPerSG := m.computeAggregatedIngressPermissionsPerSG(ctx)

	permissionSelector := labels.SelectorFromSet(labels.Set{tgbNetworkingIPPermissionLabelKey: tgbNetworkingIPPermissionLabelValue})
	for sgID, permissions := range aggregatedIngressPermissionsPerSG {
		if err := m.sgReconciler.ReconcileIngress(ctx, sgID, permissions,
			networking.WithPermissionSelector(permissionSelector),
			networking.WithAuthorizeOnly(!computedForAllTGBs)); err != nil {
			return err
		}
	}

	if computedForAllTGBs {
		if err := m.gcIngressPermissionsFromUnusedEndpointSGs(ctx, aggregatedIngressPermissionsPerSG); err != nil {
			return err
		}
	}

	return nil
}

// consolidateIngressPermissionsPerSGByTGB will consolidate the ingressPermissionsPerSGByTGB based on all tgbs with networking rules in cluster.
// returns whether we have all these TargetGroupBinding's ingressPermissionsPerSG computed.
func (m *defaultNetworkingManager) consolidateIngressPermissionsPerSGByTGB(_ context.Context, tgbsWithNetworking map[types.NamespacedName]*elbv2api.TargetGroupBinding) bool {
	for tgbKey := range m.ingressPermissionsPerSGByTGB {
		_, exists := tgbsWithNetworking[tgbKey]
		if !exists {
			delete(m.ingressPermissionsPerSGByTGB, tgbKey)
			continue
		}
	}
	computedForAllTGBs := len(m.ingressPermissionsPerSGByTGB) == len(tgbsWithNetworking)
	return computedForAllTGBs
}

// computeAggregatedIngressPermissionsPerSG will aggregate ingress permissions by SG across all TGBs.
func (m *defaultNetworkingManager) computeAggregatedIngressPermissionsPerSG(_ context.Context) map[string][]networking.IPPermissionInfo {
	permByHashCodePerSG := make(map[string]map[string]networking.IPPermissionInfo)
	for _, ingressPermissionsPerSG := range m.ingressPermissionsPerSGByTGB {
		for sgID, permissions := range ingressPermissionsPerSG {
			if _, ok := permByHashCodePerSG[sgID]; !ok {
				permByHashCodePerSG[sgID] = make(map[string]networking.IPPermissionInfo)
			}
			for _, permission := range permissions {
				permByHashCodePerSG[sgID][permission.HashCode()] = permission
			}
		}
	}
	aggregatedPermsPerSG := make(map[string][]networking.IPPermissionInfo)
	for sgID, permByHashCode := range permByHashCodePerSG {
		aggregatedPerms := make([]networking.IPPermissionInfo, 0, len(permByHashCode))
		for _, hashCode := range sets.StringKeySet(permByHashCode).List() {
			aggregatedPerms = append(aggregatedPerms, permByHashCode[hashCode])
		}
		aggregatedPermsPerSG[sgID] = aggregatedPerms
	}
	return aggregatedPermsPerSG
}

// computeIngressPermissionsForTGBNetworking computes the needed Inbound IPPermissions for specified TargetGroupBinding.
// an optional list of pods if provided if pod endpoints are used, and named ports will be resolved to the pod port.
func (m *defaultNetworkingManager) computeIngressPermissionsForTGBNetworking(ctx context.Context, tgbNetworking elbv2api.TargetGroupBindingNetworking, pods []k8s.PodInfo) ([]networking.IPPermissionInfo, error) {
	var permissions []networking.IPPermissionInfo
	protocolTCP := elbv2api.NetworkingProtocolTCP
	for _, rule := range tgbNetworking.Ingress {
		for _, rulePeer := range rule.From {
			for _, rulePort := range rule.Ports {
				permissionsForPeerPort, err := m.computePermissionsForPeerPort(ctx, rulePeer, rulePort, pods)
				if err != nil {
					return nil, err
				}
				permissions = append(permissions, permissionsForPeerPort...)
			}
			if len(rule.Ports) == 0 {
				allTCPPort := elbv2api.NetworkingPort{
					Protocol: &protocolTCP,
					Port:     nil,
				}
				permissions, err := m.computePermissionsForPeerPort(ctx, rulePeer, allTCPPort, pods)
				if err != nil {
					return nil, err
				}
				permissions = append(permissions, permissions...)
			}
		}
	}
	return permissions, nil
}

type sdkFromToPortPair struct {
	fromPort int64
	toPort   int64
}

// computePermissionsForPeerPort computes the needed Inbound IPPermissions for specified peer and port.
// an optional list of pods if provided if pod endpoints are used, and named ports will be resolved to the pod port.
func (m *defaultNetworkingManager) computePermissionsForPeerPort(ctx context.Context, peer elbv2api.NetworkingPeer, port elbv2api.NetworkingPort, pods []k8s.PodInfo) ([]networking.IPPermissionInfo, error) {
	sdkProtocol := "tcp"
	if port.Protocol != nil {
		switch *port.Protocol {
		case elbv2api.NetworkingProtocolTCP:
			sdkProtocol = "tcp"
		case elbv2api.NetworkingProtocolUDP:
			sdkProtocol = "udp"
		}
	}

	var sdkFromToPortPairs []sdkFromToPortPair
	if port.Port != nil {
		numericalPorts, err := m.computeNumericalPorts(ctx, *port.Port, pods)
		if err != nil {
			return nil, err
		}
		for _, numericalPort := range numericalPorts {
			sdkFromToPortPairs = append(sdkFromToPortPairs, sdkFromToPortPair{
				fromPort: numericalPort,
				toPort:   numericalPort,
			})
		}
	} else {
		sdkFromToPortPairs = append(sdkFromToPortPairs, sdkFromToPortPair{
			fromPort: 0,
			toPort:   65535,
		})
	}

	permissionLabels := map[string]string{tgbNetworkingIPPermissionLabelKey: tgbNetworkingIPPermissionLabelValue}
	if peer.SecurityGroup != nil {
		groupID := peer.SecurityGroup.GroupID
		permissions := make([]networking.IPPermissionInfo, 0, len(sdkFromToPortPairs))
		for _, portPair := range sdkFromToPortPairs {
			permission := networking.NewGroupIDIPPermission(sdkProtocol, awssdk.Int64(portPair.fromPort), awssdk.Int64(portPair.toPort), groupID, permissionLabels)
			permissions = append(permissions, permission)
		}
		return permissions, nil
	}

	if peer.IPBlock != nil {
		cidr := peer.IPBlock.CIDR
		_, _, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, err
		}
		permissions := make([]networking.IPPermissionInfo, 0, len(sdkFromToPortPairs))
		for _, portPair := range sdkFromToPortPairs {
			var permission networking.IPPermissionInfo
			if strings.Contains(cidr, ":") {
				permission = networking.NewCIDRv6IPPermission(sdkProtocol, awssdk.Int64(portPair.fromPort), awssdk.Int64(portPair.toPort), cidr, permissionLabels)
			} else {
				permission = networking.NewCIDRIPPermission(sdkProtocol, awssdk.Int64(portPair.fromPort), awssdk.Int64(portPair.toPort), cidr, permissionLabels)
			}
			permissions = append(permissions, permission)
		}
		return permissions, nil
	}

	return nil, errors.New("either ipBlock or securityGroup should be specified")
}

// computeNumericalPorts computes the numerical ports if a named is used.
// Note: multiple numerical ports can be returned since same named port might corresponding to different numerical ports on different pods.
func (m *defaultNetworkingManager) computeNumericalPorts(_ context.Context, port intstr.IntOrString, pods []k8s.PodInfo) ([]int64, error) {
	if port.Type == intstr.Int {
		return []int64{int64(port.IntVal)}, nil
	}
	if len(pods) == 0 {
		return nil, errors.Errorf("named ports can only be used with pod endpoints")
	}

	containerPorts := sets.NewInt64()
	for _, pod := range pods {
		containerPort, err := pod.LookupContainerPort(port)
		if err != nil {
			return nil, err
		}
		containerPorts.Insert(containerPort)
	}
	return containerPorts.List(), nil
}

// gcIngressPermissionsFromUnusedEndpointSGs will garbage collect ingress permissions from endpoint SecurityGroups that are no longer used.
func (m *defaultNetworkingManager) gcIngressPermissionsFromUnusedEndpointSGs(ctx context.Context, ingressPermissionsPerSG map[string][]networking.IPPermissionInfo) error {
	endpointSGs, err := m.fetchEndpointSGs(ctx)
	if err != nil {
		return err
	}
	usedEndpointSGs := sets.StringKeySet(ingressPermissionsPerSG)
	unusedEndpointSGs := endpointSGs.Difference(usedEndpointSGs)

	permissionSelector := labels.SelectorFromSet(labels.Set{tgbNetworkingIPPermissionLabelKey: tgbNetworkingIPPermissionLabelValue})
	for sgID := range unusedEndpointSGs {
		err := m.sgReconciler.ReconcileIngress(ctx, sgID, nil,
			networking.WithPermissionSelector(permissionSelector))
		if err != nil {
			if isEC2SecurityGroupNotFoundError(err) {
				m.unTrackEndpointSGs(ctx, sgID)
				continue
			}
			return err
		}
	}
	return nil
}

// fetchTGBsWithNetworking returns all targetGroupsBindings with networking rules in cluster.
func (m *defaultNetworkingManager) fetchTGBsWithNetworking(ctx context.Context) (map[types.NamespacedName]*elbv2api.TargetGroupBinding, error) {
	tgbList := &elbv2api.TargetGroupBindingList{}
	if err := m.k8sClient.List(ctx, tgbList); err != nil {
		return nil, err
	}
	tgbWithNetworkingByKey := make(map[types.NamespacedName]*elbv2api.TargetGroupBinding, len(tgbList.Items))
	for i := range tgbList.Items {
		tgb := &tgbList.Items[i]
		if tgb.Spec.Networking != nil {
			tgbWithNetworkingByKey[k8s.NamespacedName(tgb)] = tgb
		}
	}
	return tgbWithNetworkingByKey, nil
}

// resolveEndpointSGForENI will resolve the endpoint SecurityGroup for specific ENI.
// If there are only a single securityGroup attached, that one will be the endpoint SecurityGroup.
// If there are multiple securityGroup attached, we expect one and only one securityGroup is tagged with the cluster tag.
func (m *defaultNetworkingManager) resolveEndpointSGForENI(ctx context.Context, eniInfo networking.ENIInfo) (string, error) {
	sgIDs := eniInfo.SecurityGroups
	if len(sgIDs) == 1 {
		return sgIDs[0], nil
	}

	sgInfoByID, err := m.sgManager.FetchSGInfosByID(ctx, sgIDs)
	if err != nil {
		return "", err
	}
	clusterResourceTagKey := fmt.Sprintf("kubernetes.io/cluster/%s", m.clusterName)
	sgIDsWithClusterTag := sets.NewString()
	for sgID, sgInfo := range sgInfoByID {
		if _, ok := sgInfo.Tags[clusterResourceTagKey]; ok {
			sgIDsWithClusterTag.Insert(sgID)
		}
	}
	if len(sgIDsWithClusterTag) != 1 {
		return "", errors.Errorf("expect exactly one securityGroup tagged with %v for eni %v, got: %v",
			clusterResourceTagKey, eniInfo.NetworkInterfaceID, sgIDsWithClusterTag.List())
	}
	sgID, _ := sgIDsWithClusterTag.PopAny()
	return sgID, nil
}

// fetchEndpointSGs will return tracked endpoint SecurityGroups.
func (m *defaultNetworkingManager) fetchEndpointSGs(ctx context.Context) (sets.String, error) {
	if !m.trackedEndpointSGsInitialized {
		endpointSGs, err := m.fetchEndpointSGsFromAWS(ctx)
		if err != nil {
			return nil, err
		}
		m.trackEndpointSGs(ctx, endpointSGs...)
		m.trackedEndpointSGsInitialized = true
	}
	return m.trackedEndpointSGs, nil
}

// trackEndpointSGs will track these endpoint SecurityGroups.
func (m *defaultNetworkingManager) trackEndpointSGs(_ context.Context, sgIDs ...string) {
	m.trackedEndpointSGs.Insert(sgIDs...)
}

// unTrackEndpointSGs will unTrack these endpoint SecurityGroups.
func (m *defaultNetworkingManager) unTrackEndpointSGs(_ context.Context, sgIDs ...string) {
	m.trackedEndpointSGs.Delete(sgIDs...)
}

// fetchEndpointSGsFromAWS will return all endpoint securityGroups from AWS API.
// we consider a securityGroup as a endpoint securityGroup if it have the cluster tag.
// note: not all endpoint securityGroup have the cluster Tag(e.g. if a ENI only have a single securityGroup, it will still be used as endpoint securityGroup)
func (m *defaultNetworkingManager) fetchEndpointSGsFromAWS(ctx context.Context) ([]string, error) {
	clusterResourceTagKey := fmt.Sprintf("kubernetes.io/cluster/%s", m.clusterName)
	req := &ec2sdk.DescribeSecurityGroupsInput{
		Filters: []*ec2sdk.Filter{
			{
				Name:   awssdk.String("tag:" + clusterResourceTagKey),
				Values: awssdk.StringSlice([]string{"owned", "shared"}),
			},
			{
				Name:   awssdk.String("vpc-id"),
				Values: awssdk.StringSlice([]string{m.vpcID}),
			},
		},
	}
	sgInfoByID, err := m.sgManager.FetchSGInfosByRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	return sets.StringKeySet(sgInfoByID).List(), nil
}

func isEC2SecurityGroupNotFoundError(err error) bool {
	var awsErr awserr.Error
	if errors.As(err, &awsErr) {
		return awsErr.Code() == "InvalidGroup.NotFound"
	}
	return false
}
