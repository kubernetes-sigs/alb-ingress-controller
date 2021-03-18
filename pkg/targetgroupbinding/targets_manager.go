package targetgroupbinding

import (
	"context"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	elbv2sdk "github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/cache"
	"k8s.io/apimachinery/pkg/util/sets"
	"net"
	elbv2api "sigs.k8s.io/aws-load-balancer-controller/apis/elbv2/v1beta1"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/aws/services"
	"strings"
	"sync"
	"time"
)

const (
	defaultTargetsCacheTTL            = 5 * time.Minute
	defaultRegisterTargetsChunkSize   = 200
	defaultDeregisterTargetsChunkSize = 200
)

// TargetsManager is an abstraction around ELBV2's targets API.
type TargetsManager interface {
	// Register Targets into TargetGroup.
	RegisterTargets(ctx context.Context, tgARN string, targets []elbv2sdk.TargetDescription) error

	// Deregister Targets from TargetGroup.
	DeregisterTargets(ctx context.Context, tgARN string, targets []elbv2sdk.TargetDescription) error

	// List Targets from TargetGroup.
	ListTargets(ctx context.Context, tgb *elbv2api.TargetGroupBinding) ([]TargetInfo, error)
}

// NewCachedTargetsManager constructs new cachedTargetsManager
func NewCachedTargetsManager(elbv2Client services.ELBV2, ec2Client services.EC2, watchIPBlocks []string, watchInstanceFilters []string, logger logr.Logger) *cachedTargetsManager {
	return &cachedTargetsManager{
		elbv2Client:                elbv2Client,
		ec2Client:                  ec2Client,
		targetsCache:               cache.NewExpiring(),
		targetsCacheTTL:            defaultTargetsCacheTTL,
		registerTargetsChunkSize:   defaultRegisterTargetsChunkSize,
		deregisterTargetsChunkSize: defaultDeregisterTargetsChunkSize,
		watchIPBlocks:              watchIPBlocks,
		watchInstanceFilters:       watchInstanceFilters,
		logger:                     logger,
	}
}

var _ TargetsManager = &cachedTargetsManager{}

// an cached implementation for TargetsManager.
// Targets for each TargetGroup will be refreshed per targetsCacheTTL.
// When list Targets with RefreshTargets list Option set,
// only targets with ongoing TargetHealth(unknown/initial/draining) TargetHealth will be refreshed.
type cachedTargetsManager struct {
	elbv2Client services.ELBV2
	ec2Client   services.EC2

	// cache of targets by targetGroupARN.
	// NOTE: since this cache implementation will automatically GC expired entries, we don't need to GC entries.
	// Otherwise, we'll need to GC entries according to all TargetGroupBindings in cluster to avoid cache grow indefinitely.
	targetsCache *cache.Expiring
	// TTL for each targetGroup's targets.
	targetsCacheTTL time.Duration
	// targetsCacheMutex protects targetsCache
	targetsCacheMutex sync.RWMutex

	// chunk size for registerTargets API call.
	registerTargetsChunkSize int
	// chunk size for deregisterTargets API call.
	deregisterTargetsChunkSize int

	watchIPBlocks        []string
	watchInstanceFilters []string

	logger logr.Logger
}

// cache entry for targetsCache
type targetsCacheItem struct {
	// mutex protects below fields
	mutex sync.RWMutex
	// targets is the targets for TargetGroup
	targets []TargetInfo
}

func (m *cachedTargetsManager) RegisterTargets(ctx context.Context, tgARN string, targets []elbv2sdk.TargetDescription) error {
	targetsChunks := chunkTargetDescriptions(targets, m.registerTargetsChunkSize)
	for _, targetsChunk := range targetsChunks {
		req := &elbv2sdk.RegisterTargetsInput{
			TargetGroupArn: aws.String(tgARN),
			Targets:        pointerizeTargetDescriptions(targetsChunk),
		}
		m.logger.Info("registering targets",
			"arn", tgARN,
			"targets", targetsChunk)
		_, err := m.elbv2Client.RegisterTargetsWithContext(ctx, req)
		if err != nil {
			return err
		}
		m.logger.Info("registered targets",
			"arn", tgARN)
		m.recordSuccessfulRegisterTargetsOperation(tgARN, targetsChunk)
	}
	return nil
}

func (m *cachedTargetsManager) DeregisterTargets(ctx context.Context, tgARN string, targets []elbv2sdk.TargetDescription) error {
	targetsChunks := chunkTargetDescriptions(targets, m.deregisterTargetsChunkSize)
	for _, targetsChunk := range targetsChunks {
		req := &elbv2sdk.DeregisterTargetsInput{
			TargetGroupArn: aws.String(tgARN),
			Targets:        pointerizeTargetDescriptions(targetsChunk),
		}
		m.logger.Info("deRegistering targets",
			"arn", tgARN,
			"targets", targetsChunk)
		_, err := m.elbv2Client.DeregisterTargetsWithContext(ctx, req)
		if err != nil {
			return err
		}
		m.logger.Info("deRegistered targets",
			"arn", tgARN)
		m.recordSuccessfulDeregisterTargetsOperation(tgARN, targetsChunk)
	}
	return nil
}

func (m *cachedTargetsManager) ListTargets(ctx context.Context, tgb *elbv2api.TargetGroupBinding) ([]TargetInfo, error) {
	m.targetsCacheMutex.Lock()
	defer m.targetsCacheMutex.Unlock()

	tgARN := tgb.Spec.TargetGroupARN
	if rawTargetsCacheItem, exists := m.targetsCache.Get(tgARN); exists {
		targetsCacheItem := rawTargetsCacheItem.(*targetsCacheItem)
		targetsCacheItem.mutex.Lock()
		defer targetsCacheItem.mutex.Unlock()
		refreshedTargets, err := m.refreshUnhealthyTargets(ctx, tgb, targetsCacheItem.targets)
		if err != nil {
			return nil, err
		}
		targetsCacheItem.targets = refreshedTargets
		return cloneTargetInfoSlice(refreshedTargets), nil
	}

	refreshedTargets, err := m.refreshAllTargets(ctx, tgb)
	if err != nil {
		return nil, err
	}
	targetsCacheItem := &targetsCacheItem{
		mutex:   sync.RWMutex{},
		targets: refreshedTargets,
	}
	m.targetsCache.Set(tgARN, targetsCacheItem, m.targetsCacheTTL)
	return cloneTargetInfoSlice(refreshedTargets), nil
}

// refreshAllTargets will refresh all targets for targetGroup.
func (m *cachedTargetsManager) refreshAllTargets(ctx context.Context, tgb *elbv2api.TargetGroupBinding) ([]TargetInfo, error) {
	targets, err := m.listTargetsFromAWS(ctx, tgb, nil)
	if err != nil {
		return nil, err
	}
	return targets, nil
}

// refreshUnhealthyTargets will refresh targets that are not in healthy status for targetGroup.
// To save API calls, we don't refresh targets that are already healthy since once a target turns healthy, we'll unblock it's readinessProbe.
// we can do nothing from controller perspective when a healthy target becomes unhealthy.
func (m *cachedTargetsManager) refreshUnhealthyTargets(ctx context.Context, tgb *elbv2api.TargetGroupBinding, cachedTargets []TargetInfo) ([]TargetInfo, error) {
	var refreshedTargets []TargetInfo
	var unhealthyTargets []elbv2sdk.TargetDescription
	for _, cachedTarget := range cachedTargets {
		if cachedTarget.IsHealthy() {
			refreshedTargets = append(refreshedTargets, cachedTarget)
		} else {
			unhealthyTargets = append(unhealthyTargets, cachedTarget.Target)
		}
	}
	if len(unhealthyTargets) == 0 {
		return refreshedTargets, nil
	}

	refreshedUnhealthyTargets, err := m.listTargetsFromAWS(ctx, tgb, unhealthyTargets)
	if err != nil {
		return nil, err
	}
	for _, target := range refreshedUnhealthyTargets {
		if target.IsNotRegistered() {
			continue
		}
		refreshedTargets = append(refreshedTargets, target)
	}
	return refreshedTargets, nil
}

// listTargetsFromAWS will list targets for TargetGroup using ELBV2API.
// if specified targets is non-empty, only these targets will be listed.
// otherwise, all targets for targetGroup will be listed.
func (m *cachedTargetsManager) listTargetsFromAWS(ctx context.Context, tgb *elbv2api.TargetGroupBinding, targets []elbv2sdk.TargetDescription) ([]TargetInfo, error) {
	req := &elbv2sdk.DescribeTargetHealthInput{
		TargetGroupArn: aws.String(tgb.Spec.TargetGroupARN),
		Targets:        pointerizeTargetDescriptions(targets),
	}
	var watchInstances sets.String = nil
	if *tgb.Spec.TargetType == elbv2api.TargetTypeInstance &&
		len(m.watchInstanceFilters) > 0 {
		watchInstances = sets.String{}
		err := m.getInstancesByTags(ctx, &watchInstances)
		if err != nil {
			return nil, err
		}
	}

	resp, err := m.elbv2Client.DescribeTargetHealthWithContext(ctx, req)
	if err != nil {
		return nil, err
	}

	listedTargets := make([]TargetInfo, 0, len(resp.TargetHealthDescriptions))
	for _, elem := range resp.TargetHealthDescriptions {
		if *tgb.Spec.TargetType == elbv2api.TargetTypeIP &&
			len(m.watchIPBlocks) > 0 &&
			!m.isInCIDRsRange(m.watchIPBlocks, *elem.Target.Id) {
			continue
		}
		if *tgb.Spec.TargetType == elbv2api.TargetTypeInstance &&
			len(watchInstances) > 0 &&
			!watchInstances.Has(*elem.Target.Id) {
			continue
		}
		listedTargets = append(listedTargets, TargetInfo{
			Target:       *elem.Target,
			TargetHealth: elem.TargetHealth,
		})
	}
	return listedTargets, nil
}

func (m *cachedTargetsManager) getInstancesByTags(ctx context.Context, watchedInstances *sets.String) error {
	ec2Filters := make([]*ec2.Filter, 0, len(m.watchInstanceFilters))
	for _, filter := range m.watchInstanceFilters {
		kv := strings.Split(filter, "=")
		name := &kv[0]
		values := make([]*string, 0)
		for _, v := range strings.Split(kv[1], ",") {
			values = append(values, &v)
		}
		ec2Filters = append(ec2Filters, &ec2.Filter{
			Name:   name,
			Values: values,
		})
	}
	instances, err := m.ec2Client.DescribeInstancesAsList(ctx, &ec2.DescribeInstancesInput{Filters: ec2Filters})
	if err != nil {
		return err
	}
	for _, instance := range instances {
		watchedInstances.Insert(*instance.InstanceId)
	}
	return nil
}

func (m *cachedTargetsManager) isInCIDRsRange(ipBlocks []string, ip string) bool {
	for _, cidr := range ipBlocks {
		_, subnet, _ := net.ParseCIDR(cidr)
		if subnet.Contains(net.ParseIP(ip)) {
			return true
		}
	}

	return false
}

// recordSuccessfulRegisterTargetsOperation will record a successful deregisterTarget operation
func (m *cachedTargetsManager) recordSuccessfulRegisterTargetsOperation(tgARN string, targets []elbv2sdk.TargetDescription) {
	m.targetsCacheMutex.RLock()
	rawTargetsCacheItem, exists := m.targetsCache.Get(tgARN)
	m.targetsCacheMutex.RUnlock()

	if !exists {
		return
	}
	targetsByUniqueID := make(map[string]elbv2sdk.TargetDescription, len(targets))
	for _, target := range targets {
		targetsByUniqueID[UniqueIDForTargetDescription(target)] = target
	}

	targetsCacheItem := rawTargetsCacheItem.(*targetsCacheItem)
	targetsCacheItem.mutex.Lock()
	defer targetsCacheItem.mutex.Unlock()
	for i := range targetsCacheItem.targets {
		cachedTargetUniqueID := UniqueIDForTargetDescription(targetsCacheItem.targets[i].Target)
		if _, ok := targetsByUniqueID[cachedTargetUniqueID]; ok {
			targetsCacheItem.targets[i].TargetHealth = nil
			delete(targetsByUniqueID, cachedTargetUniqueID)
		}
	}

	for _, target := range targetsByUniqueID {
		targetsCacheItem.targets = append(targetsCacheItem.targets, TargetInfo{
			Target:       target,
			TargetHealth: nil,
		})
	}
}

// recordSuccessfulDeregisterTargetsOperation will record a successful deregisterTarget operation
func (m *cachedTargetsManager) recordSuccessfulDeregisterTargetsOperation(tgARN string, targets []elbv2sdk.TargetDescription) {
	m.targetsCacheMutex.RLock()
	rawTargetsCacheItem, exists := m.targetsCache.Get(tgARN)
	m.targetsCacheMutex.RUnlock()

	if !exists {
		return
	}
	targetsByUniqueID := make(map[string]elbv2sdk.TargetDescription, len(targets))
	for _, target := range targets {
		targetsByUniqueID[UniqueIDForTargetDescription(target)] = target
	}

	targetsCacheItem := rawTargetsCacheItem.(*targetsCacheItem)
	targetsCacheItem.mutex.Lock()
	defer targetsCacheItem.mutex.Unlock()
	for i := range targetsCacheItem.targets {
		cachedTargetUniqueID := UniqueIDForTargetDescription(targetsCacheItem.targets[i].Target)
		if _, ok := targetsByUniqueID[cachedTargetUniqueID]; ok {
			targetsCacheItem.targets[i].TargetHealth = nil
			delete(targetsByUniqueID, cachedTargetUniqueID)
		}
	}
}

// chunkTargetDescriptions will split slice of TargetDescription into chunks
func chunkTargetDescriptions(targets []elbv2sdk.TargetDescription, chunkSize int) [][]elbv2sdk.TargetDescription {
	var chunks [][]elbv2sdk.TargetDescription
	for i := 0; i < len(targets); i += chunkSize {
		end := i + chunkSize
		if end > len(targets) {
			end = len(targets)
		}
		chunks = append(chunks, targets[i:end])
	}
	return chunks
}

// pointerizeTargetDescriptions converts slice of TargetDescription into slice of pointers to TargetDescription
// if targets is empty or nil, nil will be returned.
func pointerizeTargetDescriptions(targets []elbv2sdk.TargetDescription) []*elbv2sdk.TargetDescription {
	if len(targets) == 0 {
		return nil
	}
	result := make([]*elbv2sdk.TargetDescription, 0, len(targets))
	for i := range targets {
		result = append(result, &targets[i])
	}
	return result
}

// cloneTargetInfoSlice returns a clone of TargetInfoSlice.
func cloneTargetInfoSlice(targets []TargetInfo) []TargetInfo {
	return append(targets[:0:0], targets...)
}
