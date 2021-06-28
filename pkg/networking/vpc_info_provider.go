package networking

import (
	"context"
	"sync"
	"time"

	awssdk "github.com/aws/aws-sdk-go/aws"
	ec2sdk "github.com/aws/aws-sdk-go/service/ec2"
	"github.com/go-logr/logr"
	"github.com/patrickmn/go-cache"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/aws/services"
)

// VPCInfoProvider is responsible for providing VPC info.
type VPCInfoProvider interface {
	FetchVPCInfo(ctx context.Context, vpcID string) (*ec2sdk.Vpc, error)
}

// NewDefaultVPCInfoProvider constructs new defaultVPCInfoProvider.
func NewDefaultVPCInfoProvider(cacheDuration int, ec2Client services.EC2, logger logr.Logger) *defaultVPCInfoProvider {
	return &defaultVPCInfoProvider{
		ec2Client:         ec2Client,
		vpcInfoCache:      cache.New(time.Duration(cacheDuration)*time.Minute, 10*time.Minute),
		vpcInfoCacheMutex: sync.RWMutex{},
		logger:            logger,
	}
}

var _ VPCInfoProvider = &defaultVPCInfoProvider{}

// default implementation for VPCInfoProvider.
type defaultVPCInfoProvider struct {
	ec2Client         services.EC2
	vpcInfoCache      *cache.Cache
	vpcInfoCacheMutex sync.RWMutex

	logger logr.Logger
}

func (p *defaultVPCInfoProvider) FetchVPCInfo(ctx context.Context, vpcID string) (*ec2sdk.Vpc, error) {
	if vpcInfo := p.fetchVPCInfoFromCache(); vpcInfo != nil {
		return vpcInfo, nil
	}

	// Fetch VPC info from the AWS API and cache response before returning.
	vpcInfo, err := p.fetchVPCInfoFromAWS(ctx, vpcID)
	if err != nil {
		return nil, nil
	}
	p.saveVPCInfoToCache(vpcInfo)

	return vpcInfo, nil
}

func (p *defaultVPCInfoProvider) fetchVPCInfoFromCache() *ec2sdk.Vpc {
	p.vpcInfoCacheMutex.RLock()
	defer p.vpcInfoCacheMutex.RUnlock()

	vpcInfo, found := p.vpcInfoCache.Get("vpcInfo")
	if !found {
		return nil
	}

	return vpcInfo.(*ec2sdk.Vpc)
}

func (p *defaultVPCInfoProvider) saveVPCInfoToCache(vpcInfo *ec2sdk.Vpc) {
	p.vpcInfoCacheMutex.Lock()
	defer p.vpcInfoCacheMutex.Unlock()

	p.vpcInfoCache.SetDefault("vpcInfo", vpcInfo)
}

// fetchVPCInfoFromAWS will fetch VPC info from the AWS API.
func (p *defaultVPCInfoProvider) fetchVPCInfoFromAWS(ctx context.Context, vpcID string) (*ec2sdk.Vpc, error) {
	req := &ec2sdk.DescribeVpcsInput{
		VpcIds: []*string{awssdk.String(vpcID)},
	}
	resp, err := p.ec2Client.DescribeVpcsWithContext(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp.Vpcs[0], nil
}
