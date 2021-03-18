package targetgroupbinding

import (
	"context"
	"github.com/aws/aws-sdk-go/aws"
	awssdk "github.com/aws/aws-sdk-go/aws"
	ec2sdk "github.com/aws/aws-sdk-go/service/ec2"
	elbv2sdk "github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/cache"
	elbv2api "sigs.k8s.io/aws-load-balancer-controller/apis/elbv2/v1beta1"
	mock_services "sigs.k8s.io/aws-load-balancer-controller/mocks/aws/services"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sync"
	"testing"
	"time"
)

func Test_cachedTargetsManager_RegisterTargets(t *testing.T) {
	type registerTargetsWithContextCall struct {
		req  *elbv2sdk.RegisterTargetsInput
		resp *elbv2sdk.RegisterTargetsOutput
		err  error
	}

	type fields struct {
		registerTargetsWithContextCalls []registerTargetsWithContextCall
		targetsCache                    map[string][]TargetInfo
	}
	type args struct {
		tgARN   string
		targets []elbv2sdk.TargetDescription
	}
	tests := []struct {
		name             string
		fields           fields
		args             args
		wantTargetsCache map[string][]TargetInfo
		wantErr          error
	}{
		{
			name: "register targets and targets for TargetGroup already exists in cache",
			fields: fields{
				registerTargetsWithContextCalls: []registerTargetsWithContextCall{
					{
						req: &elbv2sdk.RegisterTargetsInput{
							TargetGroupArn: awssdk.String("my-tg"),
							Targets: []*elbv2sdk.TargetDescription{
								{
									Id:   awssdk.String("192.168.1.2"),
									Port: awssdk.Int64(8080),
								},
								{
									Id:   awssdk.String("192.168.1.3"),
									Port: awssdk.Int64(8080),
								},
							},
						},
						resp: &elbv2sdk.RegisterTargetsOutput{},
					},
				},
				targetsCache: map[string][]TargetInfo{
					"my-tg": {
						{
							Target: elbv2sdk.TargetDescription{
								Id:   awssdk.String("192.168.1.1"),
								Port: awssdk.Int64(8080),
							},
							TargetHealth: &elbv2sdk.TargetHealth{
								State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
							},
						},
						{
							Target: elbv2sdk.TargetDescription{
								Id:   awssdk.String("192.168.1.2"),
								Port: awssdk.Int64(8080),
							},
							TargetHealth: &elbv2sdk.TargetHealth{
								Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumTargetTimeout),
								State:  awssdk.String(elbv2sdk.TargetHealthStateEnumUnhealthy),
							},
						},
					},
				},
			},
			args: args{
				tgARN: "my-tg",
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.3"),
						Port: awssdk.Int64(8080),
					},
				},
			},
			wantTargetsCache: map[string][]TargetInfo{
				"my-tg": {
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.1"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
						},
					},
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.2"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: nil,
					},
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.3"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: nil,
					},
				},
			},
		},
		{
			name: "register targets and targets for TargetGroup don't exists in cache",
			fields: fields{
				registerTargetsWithContextCalls: []registerTargetsWithContextCall{
					{
						req: &elbv2sdk.RegisterTargetsInput{
							TargetGroupArn: awssdk.String("my-tg"),
							Targets: []*elbv2sdk.TargetDescription{
								{
									Id:   awssdk.String("192.168.1.2"),
									Port: awssdk.Int64(8080),
								},
								{
									Id:   awssdk.String("192.168.1.3"),
									Port: awssdk.Int64(8080),
								},
							},
						},
						resp: &elbv2sdk.RegisterTargetsOutput{},
					},
				},
				targetsCache: nil,
			},
			args: args{
				tgARN: "my-tg",
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.3"),
						Port: awssdk.Int64(8080),
					},
				},
			},
			wantTargetsCache: nil,
		},
		{
			name: "register multiple targets in batches",
			fields: fields{
				registerTargetsWithContextCalls: []registerTargetsWithContextCall{
					{
						req: &elbv2sdk.RegisterTargetsInput{
							TargetGroupArn: awssdk.String("my-tg"),
							Targets: []*elbv2sdk.TargetDescription{
								{
									Id:   awssdk.String("192.168.1.1"),
									Port: awssdk.Int64(8080),
								},
								{
									Id:   awssdk.String("192.168.1.2"),
									Port: awssdk.Int64(8080),
								},
							},
						},
						resp: &elbv2sdk.RegisterTargetsOutput{},
					},
					{
						req: &elbv2sdk.RegisterTargetsInput{
							TargetGroupArn: awssdk.String("my-tg"),
							Targets: []*elbv2sdk.TargetDescription{
								{
									Id:   awssdk.String("192.168.1.3"),
									Port: awssdk.Int64(8080),
								},
								{
									Id:   awssdk.String("192.168.1.4"),
									Port: awssdk.Int64(8080),
								},
							},
						},
						resp: &elbv2sdk.RegisterTargetsOutput{},
					},
				},
				targetsCache: map[string][]TargetInfo{
					"my-tg": nil,
				},
			},
			args: args{
				tgARN: "my-tg",
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.3"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.4"),
						Port: awssdk.Int64(8080),
					},
				},
			},
			wantTargetsCache: map[string][]TargetInfo{
				"my-tg": {
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.1"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: nil,
					},
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.2"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: nil,
					},
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.3"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: nil,
					},
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.4"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: nil,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			elbv2Client := mock_services.NewMockELBV2(ctrl)
			for _, call := range tt.fields.registerTargetsWithContextCalls {
				elbv2Client.EXPECT().RegisterTargetsWithContext(gomock.Any(), call.req).Return(call.resp, call.err)
			}

			targetsCache := cache.NewExpiring()
			targetsCacheTTL := 1 * time.Minute
			for tgARN, targets := range tt.fields.targetsCache {
				targetsCache.Set(tgARN, &targetsCacheItem{
					mutex:   sync.RWMutex{},
					targets: targets,
				}, targetsCacheTTL)
			}
			m := cachedTargetsManager{
				elbv2Client:              elbv2Client,
				targetsCache:             targetsCache,
				targetsCacheTTL:          targetsCacheTTL,
				registerTargetsChunkSize: 2,
				logger:                   log.Log,
			}

			ctx := context.Background()
			err := m.RegisterTargets(ctx, tt.args.tgARN, tt.args.targets)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, len(tt.wantTargetsCache), targetsCache.Len())
				for tgARN, targets := range tt.wantTargetsCache {
					rawTargetsCacheItem, exists := targetsCache.Get(tgARN)
					assert.True(t, exists)
					targetsCacheItem := rawTargetsCacheItem.(*targetsCacheItem)
					assert.ElementsMatch(t, targets, targetsCacheItem.targets)
				}
			}
		})
	}
}

func Test_cachedTargetsManager_DeregisterTargets(t *testing.T) {
	type deregisterTargetsWithContextCall struct {
		req  *elbv2sdk.DeregisterTargetsInput
		resp *elbv2sdk.DeregisterTargetsOutput
		err  error
	}

	type fields struct {
		deregisterTargetsWithContextCalls []deregisterTargetsWithContextCall
		targetsCache                      map[string][]TargetInfo
	}
	type args struct {
		tgARN   string
		targets []elbv2sdk.TargetDescription
	}
	tests := []struct {
		name             string
		fields           fields
		args             args
		wantTargetsCache map[string][]TargetInfo
		wantErr          error
	}{
		{
			name: "deregister targets and targets for TargetGroup already exists in cache",
			fields: fields{
				deregisterTargetsWithContextCalls: []deregisterTargetsWithContextCall{
					{
						req: &elbv2sdk.DeregisterTargetsInput{
							TargetGroupArn: awssdk.String("my-tg"),
							Targets: []*elbv2sdk.TargetDescription{
								{
									Id:   awssdk.String("192.168.1.2"),
									Port: awssdk.Int64(8080),
								},
								{
									Id:   awssdk.String("192.168.1.3"),
									Port: awssdk.Int64(8080),
								},
							},
						},
						resp: &elbv2sdk.DeregisterTargetsOutput{},
					},
				},
				targetsCache: map[string][]TargetInfo{
					"my-tg": {
						{
							Target: elbv2sdk.TargetDescription{
								Id:   awssdk.String("192.168.1.1"),
								Port: awssdk.Int64(8080),
							},
							TargetHealth: &elbv2sdk.TargetHealth{
								State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
							},
						},
						{
							Target: elbv2sdk.TargetDescription{
								Id:   awssdk.String("192.168.1.2"),
								Port: awssdk.Int64(8080),
							},
							TargetHealth: &elbv2sdk.TargetHealth{
								Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumTargetTimeout),
								State:  awssdk.String(elbv2sdk.TargetHealthStateEnumUnhealthy),
							},
						},
					},
				},
			},
			args: args{
				tgARN: "my-tg",
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.3"),
						Port: awssdk.Int64(8080),
					},
				},
			},
			wantTargetsCache: map[string][]TargetInfo{
				"my-tg": {
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.1"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
						},
					},
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.2"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: nil,
					},
				},
			},
		},
		{
			name: "register targets and targets for TargetGroup don't exists in cache",
			fields: fields{
				deregisterTargetsWithContextCalls: []deregisterTargetsWithContextCall{
					{
						req: &elbv2sdk.DeregisterTargetsInput{
							TargetGroupArn: awssdk.String("my-tg"),
							Targets: []*elbv2sdk.TargetDescription{
								{
									Id:   awssdk.String("192.168.1.2"),
									Port: awssdk.Int64(8080),
								},
								{
									Id:   awssdk.String("192.168.1.3"),
									Port: awssdk.Int64(8080),
								},
							},
						},
						resp: &elbv2sdk.DeregisterTargetsOutput{},
					},
				},
				targetsCache: nil,
			},
			args: args{
				tgARN: "my-tg",
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.3"),
						Port: awssdk.Int64(8080),
					},
				},
			},
			wantTargetsCache: nil,
		},
		{
			name: "register multiple targets in batches",
			fields: fields{
				deregisterTargetsWithContextCalls: []deregisterTargetsWithContextCall{
					{
						req: &elbv2sdk.DeregisterTargetsInput{
							TargetGroupArn: awssdk.String("my-tg"),
							Targets: []*elbv2sdk.TargetDescription{
								{
									Id:   awssdk.String("192.168.1.1"),
									Port: awssdk.Int64(8080),
								},
								{
									Id:   awssdk.String("192.168.1.2"),
									Port: awssdk.Int64(8080),
								},
							},
						},
						resp: &elbv2sdk.DeregisterTargetsOutput{},
					},
					{
						req: &elbv2sdk.DeregisterTargetsInput{
							TargetGroupArn: awssdk.String("my-tg"),
							Targets: []*elbv2sdk.TargetDescription{
								{
									Id:   awssdk.String("192.168.1.3"),
									Port: awssdk.Int64(8080),
								},
								{
									Id:   awssdk.String("192.168.1.4"),
									Port: awssdk.Int64(8080),
								},
							},
						},
						resp: &elbv2sdk.DeregisterTargetsOutput{},
					},
				},
				targetsCache: nil,
			},
			args: args{
				tgARN: "my-tg",
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.3"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.4"),
						Port: awssdk.Int64(8080),
					},
				},
			},
			wantTargetsCache: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			elbv2Client := mock_services.NewMockELBV2(ctrl)
			for _, call := range tt.fields.deregisterTargetsWithContextCalls {
				elbv2Client.EXPECT().DeregisterTargetsWithContext(gomock.Any(), call.req).Return(call.resp, call.err)
			}

			targetsCache := cache.NewExpiring()
			targetsCacheTTL := 1 * time.Minute
			for tgARN, targets := range tt.fields.targetsCache {
				targetsCache.Set(tgARN, &targetsCacheItem{
					mutex:   sync.RWMutex{},
					targets: targets,
				}, targetsCacheTTL)
			}
			m := cachedTargetsManager{
				elbv2Client:                elbv2Client,
				targetsCache:               targetsCache,
				targetsCacheTTL:            targetsCacheTTL,
				deregisterTargetsChunkSize: 2,
				logger:                     log.Log,
			}

			ctx := context.Background()
			err := m.DeregisterTargets(ctx, tt.args.tgARN, tt.args.targets)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, len(tt.wantTargetsCache), targetsCache.Len())
				for tgARN, targets := range tt.wantTargetsCache {
					rawTargetsCacheItem, exists := targetsCache.Get(tgARN)
					assert.True(t, exists)
					targetsCacheItem := rawTargetsCacheItem.(*targetsCacheItem)
					assert.ElementsMatch(t, targets, targetsCacheItem.targets)
				}
			}
		})
	}
}

func Test_cachedTargetsManager_ListTargets(t *testing.T) {
	ipTargetType := elbv2api.TargetTypeIP

	type describeTargetHealthWithContextCall struct {
		req  *elbv2sdk.DescribeTargetHealthInput
		resp *elbv2sdk.DescribeTargetHealthOutput
		err  error
	}
	type fields struct {
		describeTargetHealthWithContextCalls []describeTargetHealthWithContextCall
		targetsCache                         map[string][]TargetInfo
	}
	type args struct {
		tgb *elbv2api.TargetGroupBinding
	}

	tests := []struct {
		name             string
		fields           fields
		args             args
		want             []TargetInfo
		wantTargetsCache map[string][]TargetInfo
		wantErr          error
	}{
		{
			name: "when targets for targetGroup don't exists in cache",
			fields: fields{
				describeTargetHealthWithContextCalls: []describeTargetHealthWithContextCall{
					{
						req: &elbv2sdk.DescribeTargetHealthInput{
							TargetGroupArn: awssdk.String("my-tg"),
							Targets:        nil,
						},
						resp: &elbv2sdk.DescribeTargetHealthOutput{
							TargetHealthDescriptions: []*elbv2sdk.TargetHealthDescription{
								{
									Target: &elbv2sdk.TargetDescription{
										Id:   awssdk.String("192.168.1.1"),
										Port: awssdk.Int64(8080),
									},
									TargetHealth: &elbv2sdk.TargetHealth{
										State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
									},
								},
							},
						},
					},
				},
				targetsCache: nil,
			},
			args: args{
				tgb: &elbv2api.TargetGroupBinding{
					Spec: elbv2api.TargetGroupBindingSpec{
						TargetGroupARN: "my-tg",
						TargetType:     &ipTargetType,
					},
				},
			},
			want: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
			},
			wantTargetsCache: map[string][]TargetInfo{
				"my-tg": {
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.1"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
						},
					},
				},
			},
		},
		{
			name: "when targets for targetGroup exists in cache and don't need refresh",
			fields: fields{
				describeTargetHealthWithContextCalls: nil,
				targetsCache: map[string][]TargetInfo{
					"my-tg": {
						{
							Target: elbv2sdk.TargetDescription{
								Id:   awssdk.String("192.168.1.1"),
								Port: awssdk.Int64(8080),
							},
							TargetHealth: &elbv2sdk.TargetHealth{
								State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
							},
						},
					},
				},
			},
			args: args{
				tgb: &elbv2api.TargetGroupBinding{
					Spec: elbv2api.TargetGroupBindingSpec{
						TargetGroupARN: "my-tg",
						TargetType:     &ipTargetType,
					},
				},
			},
			want: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
			},
			wantTargetsCache: map[string][]TargetInfo{
				"my-tg": {
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.1"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
						},
					},
				},
			},
		},
		{
			name: "when targets for targetGroup exists in cache and needs refresh",
			fields: fields{
				describeTargetHealthWithContextCalls: []describeTargetHealthWithContextCall{
					{
						req: &elbv2sdk.DescribeTargetHealthInput{
							TargetGroupArn: awssdk.String("my-tg"),
							Targets: []*elbv2sdk.TargetDescription{
								{
									Id:   awssdk.String("192.168.1.2"),
									Port: awssdk.Int64(8080),
								},
							},
						},
						resp: &elbv2sdk.DescribeTargetHealthOutput{
							TargetHealthDescriptions: []*elbv2sdk.TargetHealthDescription{
								{
									Target: &elbv2sdk.TargetDescription{
										Id:   awssdk.String("192.168.1.2"),
										Port: awssdk.Int64(8080),
									},
									TargetHealth: &elbv2sdk.TargetHealth{
										State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
									},
								},
							},
						},
					},
				},
				targetsCache: map[string][]TargetInfo{
					"my-tg": {
						{
							Target: elbv2sdk.TargetDescription{
								Id:   awssdk.String("192.168.1.1"),
								Port: awssdk.Int64(8080),
							},
							TargetHealth: &elbv2sdk.TargetHealth{
								State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
							},
						},
						{
							Target: elbv2sdk.TargetDescription{
								Id:   awssdk.String("192.168.1.2"),
								Port: awssdk.Int64(8080),
							},
							TargetHealth: &elbv2sdk.TargetHealth{
								Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
								State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
							},
						},
					},
				},
			},
			args: args{
				tgb: &elbv2api.TargetGroupBinding{
					Spec: elbv2api.TargetGroupBindingSpec{
						TargetGroupARN: "my-tg",
						TargetType:     &ipTargetType,
					},
				},
			},
			want: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
			},
			wantTargetsCache: map[string][]TargetInfo{
				"my-tg": {
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.1"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
						},
					},
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.2"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			elbv2Client := mock_services.NewMockELBV2(ctrl)
			for _, call := range tt.fields.describeTargetHealthWithContextCalls {
				elbv2Client.EXPECT().DescribeTargetHealthWithContext(gomock.Any(), call.req).Return(call.resp, call.err)
			}
			targetsCache := cache.NewExpiring()
			targetsCacheTTL := 1 * time.Minute
			for tgARN, targets := range tt.fields.targetsCache {
				targetsCache.Set(tgARN, &targetsCacheItem{
					mutex:   sync.RWMutex{},
					targets: targets,
				}, targetsCacheTTL)
			}
			m := &cachedTargetsManager{
				elbv2Client:       elbv2Client,
				targetsCache:      targetsCache,
				targetsCacheMutex: sync.RWMutex{},
				targetsCacheTTL:   targetsCacheTTL,
			}

			ctx := context.Background()
			got, err := m.ListTargets(ctx, tt.args.tgb)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
				assert.Equal(t, len(tt.wantTargetsCache), targetsCache.Len())
				for tgARN, targets := range tt.wantTargetsCache {
					rawTargetsCacheItem, exists := targetsCache.Get(tgARN)
					assert.True(t, exists)
					targetsCacheItem := rawTargetsCacheItem.(*targetsCacheItem)
					assert.ElementsMatch(t, targets, targetsCacheItem.targets)
				}
			}
		})
	}
}

func Test_cachedTargetsManager_refreshUnhealthyTargets(t *testing.T) {
	ipTargetType := elbv2api.TargetTypeIP

	type describeTargetHealthWithContextCall struct {
		req  *elbv2sdk.DescribeTargetHealthInput
		resp *elbv2sdk.DescribeTargetHealthOutput
		err  error
	}
	type fields struct {
		describeTargetHealthWithContextCalls []describeTargetHealthWithContextCall
	}
	type args struct {
		cachedTargets []TargetInfo
		tgb           *elbv2api.TargetGroupBinding
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    []TargetInfo
		wantErr error
	}{
		{
			name: "when all targets are already healthy",
			fields: fields{
				describeTargetHealthWithContextCalls: nil,
			},
			args: args{
				tgb: &elbv2api.TargetGroupBinding{
					Spec: elbv2api.TargetGroupBindingSpec{
						TargetGroupARN: "my-tg",
						TargetType:     &ipTargetType,
					},
				},
				cachedTargets: []TargetInfo{
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.1"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
						},
					},
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.2"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
						},
					},
				},
			},
			want: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
			},
		},
		{
			name: "when all targets are not healthy",
			fields: fields{
				describeTargetHealthWithContextCalls: []describeTargetHealthWithContextCall{
					{
						req: &elbv2sdk.DescribeTargetHealthInput{
							TargetGroupArn: awssdk.String("my-tg"),
							Targets: []*elbv2sdk.TargetDescription{
								{
									Id:   awssdk.String("192.168.1.1"),
									Port: awssdk.Int64(8080),
								},
								{
									Id:   awssdk.String("192.168.1.2"),
									Port: awssdk.Int64(8080),
								},
							},
						},
						resp: &elbv2sdk.DescribeTargetHealthOutput{
							TargetHealthDescriptions: []*elbv2sdk.TargetHealthDescription{
								{
									Target: &elbv2sdk.TargetDescription{
										Id:   awssdk.String("192.168.1.1"),
										Port: awssdk.Int64(8080),
									},
									TargetHealth: &elbv2sdk.TargetHealth{
										Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumTargetTimeout),
										State:  awssdk.String(elbv2sdk.TargetHealthStateEnumUnhealthy),
									},
								},
								{
									Target: &elbv2sdk.TargetDescription{
										Id:   awssdk.String("192.168.1.2"),
										Port: awssdk.Int64(8080),
									},
									TargetHealth: &elbv2sdk.TargetHealth{
										State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
									},
								},
							},
						},
					},
				},
			},
			args: args{
				tgb: &elbv2api.TargetGroupBinding{
					Spec: elbv2api.TargetGroupBindingSpec{
						TargetGroupARN: "my-tg",
						TargetType:     &ipTargetType,
					},
				},
				cachedTargets: []TargetInfo{
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.1"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumTargetTimeout),
							State:  awssdk.String(elbv2sdk.TargetHealthStateEnumUnhealthy),
						},
					},
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.2"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
							State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
						},
					},
				},
			},
			want: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumTargetTimeout),
						State:  awssdk.String(elbv2sdk.TargetHealthStateEnumUnhealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
			},
		},
		{
			name: "when some targets are healthy, some targets are not healthy",
			fields: fields{
				describeTargetHealthWithContextCalls: []describeTargetHealthWithContextCall{
					{
						req: &elbv2sdk.DescribeTargetHealthInput{
							TargetGroupArn: awssdk.String("my-tg"),
							Targets: []*elbv2sdk.TargetDescription{
								{
									Id:   awssdk.String("192.168.1.2"),
									Port: awssdk.Int64(8080),
								},
								{
									Id:   awssdk.String("192.168.1.3"),
									Port: awssdk.Int64(8080),
								},
							},
						},
						resp: &elbv2sdk.DescribeTargetHealthOutput{
							TargetHealthDescriptions: []*elbv2sdk.TargetHealthDescription{
								{
									Target: &elbv2sdk.TargetDescription{
										Id:   awssdk.String("192.168.1.2"),
										Port: awssdk.Int64(8080),
									},
									TargetHealth: &elbv2sdk.TargetHealth{
										Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumTargetTimeout),
										State:  awssdk.String(elbv2sdk.TargetHealthStateEnumUnhealthy),
									},
								},
								{
									Target: &elbv2sdk.TargetDescription{
										Id:   awssdk.String("192.168.1.3"),
										Port: awssdk.Int64(8080),
									},
									TargetHealth: &elbv2sdk.TargetHealth{
										State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
									},
								},
							},
						},
					},
				},
			},
			args: args{
				tgb: &elbv2api.TargetGroupBinding{
					Spec: elbv2api.TargetGroupBindingSpec{
						TargetGroupARN: "my-tg",
						TargetType:     &ipTargetType,
					},
				},
				cachedTargets: []TargetInfo{
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.1"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
						},
					},
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.2"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumTargetTimeout),
							State:  awssdk.String(elbv2sdk.TargetHealthStateEnumUnhealthy),
						},
					},
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.3"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
							State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
						},
					},
				},
			},
			want: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumTargetTimeout),
						State:  awssdk.String(elbv2sdk.TargetHealthStateEnumUnhealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.3"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
			},
		},
		{
			name: "when some targets have unknown targetHealth and removed after refresh",
			fields: fields{
				describeTargetHealthWithContextCalls: []describeTargetHealthWithContextCall{
					{
						req: &elbv2sdk.DescribeTargetHealthInput{
							TargetGroupArn: awssdk.String("my-tg"),
							Targets: []*elbv2sdk.TargetDescription{
								{
									Id:   awssdk.String("192.168.1.2"),
									Port: awssdk.Int64(8080),
								},
								{
									Id:   awssdk.String("192.168.1.3"),
									Port: awssdk.Int64(8080),
								},
							},
						},
						resp: &elbv2sdk.DescribeTargetHealthOutput{
							TargetHealthDescriptions: []*elbv2sdk.TargetHealthDescription{
								{
									Target: &elbv2sdk.TargetDescription{
										Id:   awssdk.String("192.168.1.2"),
										Port: awssdk.Int64(8080),
									},
									TargetHealth: &elbv2sdk.TargetHealth{
										Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumTargetTimeout),
										State:  awssdk.String(elbv2sdk.TargetHealthStateEnumUnhealthy),
									},
								},
								{
									Target: &elbv2sdk.TargetDescription{
										Id:   awssdk.String("192.168.1.3"),
										Port: awssdk.Int64(8080),
									},
									TargetHealth: &elbv2sdk.TargetHealth{
										Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumTargetNotRegistered),
										State:  awssdk.String(elbv2sdk.TargetHealthStateEnumUnused),
									},
								},
							},
						},
					},
				},
			},
			args: args{
				tgb: &elbv2api.TargetGroupBinding{
					Spec: elbv2api.TargetGroupBindingSpec{
						TargetGroupARN: "my-tg",
						TargetType:     &ipTargetType,
					},
				},
				cachedTargets: []TargetInfo{
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.1"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
						},
					},
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.2"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumTargetTimeout),
							State:  awssdk.String(elbv2sdk.TargetHealthStateEnumUnhealthy),
						},
					},
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.3"),
							Port: awssdk.Int64(8080),
						},
					},
				},
			},
			want: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumTargetTimeout),
						State:  awssdk.String(elbv2sdk.TargetHealthStateEnumUnhealthy),
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			elbv2Client := mock_services.NewMockELBV2(ctrl)
			for _, call := range tt.fields.describeTargetHealthWithContextCalls {
				elbv2Client.EXPECT().DescribeTargetHealthWithContext(gomock.Any(), call.req).Return(call.resp, call.err)
			}
			m := &cachedTargetsManager{
				elbv2Client: elbv2Client,
			}
			ctx := context.Background()
			got, err := m.refreshUnhealthyTargets(ctx, tt.args.tgb, tt.args.cachedTargets)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func Test_cachedTargetsManager_listTargetsFromAWS(t *testing.T) {
	ipTargetType := elbv2api.TargetTypeIP
	instanceTargetType := elbv2api.TargetTypeInstance

	type describeTargetHealthWithContextCall struct {
		req  *elbv2sdk.DescribeTargetHealthInput
		resp *elbv2sdk.DescribeTargetHealthOutput
		err  error
	}
	type describeInstancesAsListsCall struct {
		req  *ec2sdk.DescribeInstancesInput
		resp []*ec2sdk.Instance
		err  error
	}
	type fields struct {
		describeTargetHealthWithContextCalls []describeTargetHealthWithContextCall
		describeInstancesAsListsCalls        []describeInstancesAsListsCall
	}

	type args struct {
		targets              []elbv2sdk.TargetDescription
		tgb                  *elbv2api.TargetGroupBinding
		watchIPBlocks        []string
		watchInstanceFilters []string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    []TargetInfo
		wantErr error
	}{
		{
			name: "list with non-nil targets",
			fields: fields{
				describeTargetHealthWithContextCalls: []describeTargetHealthWithContextCall{
					{
						req: &elbv2sdk.DescribeTargetHealthInput{
							TargetGroupArn: awssdk.String("my-tg"),
							Targets: []*elbv2sdk.TargetDescription{
								{
									Id:   awssdk.String("192.168.1.1"),
									Port: awssdk.Int64(8080),
								},
							},
						},
						resp: &elbv2sdk.DescribeTargetHealthOutput{
							TargetHealthDescriptions: []*elbv2sdk.TargetHealthDescription{
								{
									Target: &elbv2sdk.TargetDescription{
										Id:   awssdk.String("192.168.1.1"),
										Port: awssdk.Int64(8080),
									},
									TargetHealth: &elbv2sdk.TargetHealth{
										Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
										State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
									},
								},
							},
						},
					},
				},
			},
			args: args{
				tgb: &elbv2api.TargetGroupBinding{
					Spec: elbv2api.TargetGroupBindingSpec{
						TargetGroupARN: "my-tg",
						TargetType:     &ipTargetType,
					},
				},
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
				},
			},
			want: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
						State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
					},
				},
			},
		},
		{
			name: "list with nil targets",
			fields: fields{
				describeTargetHealthWithContextCalls: []describeTargetHealthWithContextCall{
					{
						req: &elbv2sdk.DescribeTargetHealthInput{
							TargetGroupArn: awssdk.String("my-tg"),
							Targets:        nil,
						},
						resp: &elbv2sdk.DescribeTargetHealthOutput{
							TargetHealthDescriptions: []*elbv2sdk.TargetHealthDescription{
								{
									Target: &elbv2sdk.TargetDescription{
										Id:   awssdk.String("192.168.1.1"),
										Port: awssdk.Int64(8080),
									},
									TargetHealth: &elbv2sdk.TargetHealth{
										Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
										State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
									},
								},
								{
									Target: &elbv2sdk.TargetDescription{
										Id:   awssdk.String("192.168.1.2"),
										Port: awssdk.Int64(8080),
									},
									TargetHealth: &elbv2sdk.TargetHealth{
										Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
										State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
									},
								},
							},
						},
					},
				},
			},
			args: args{
				tgb: &elbv2api.TargetGroupBinding{
					Spec: elbv2api.TargetGroupBindingSpec{
						TargetGroupARN: "my-tg",
						TargetType:     &ipTargetType,
					},
				},
				targets: nil,
			},
			want: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
						State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
						State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
					},
				},
			},
		},
		{
			name: "list with nil targets filtered by CIDRs",
			fields: fields{
				describeTargetHealthWithContextCalls: []describeTargetHealthWithContextCall{
					{
						req: &elbv2sdk.DescribeTargetHealthInput{
							TargetGroupArn: awssdk.String("my-tg"),
							Targets:        nil,
						},
						resp: &elbv2sdk.DescribeTargetHealthOutput{
							TargetHealthDescriptions: []*elbv2sdk.TargetHealthDescription{
								{
									Target: &elbv2sdk.TargetDescription{
										Id:   awssdk.String("10.10.1.1"),
										Port: awssdk.Int64(8080),
									},
									TargetHealth: &elbv2sdk.TargetHealth{
										Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
										State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
									},
								},
								{
									Target: &elbv2sdk.TargetDescription{
										Id:   awssdk.String("192.168.1.1"),
										Port: awssdk.Int64(8080),
									},
									TargetHealth: &elbv2sdk.TargetHealth{
										Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
										State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
									},
								},
								{
									Target: &elbv2sdk.TargetDescription{
										Id:   awssdk.String("192.168.1.2"),
										Port: awssdk.Int64(8080),
									},
									TargetHealth: &elbv2sdk.TargetHealth{
										Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
										State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
									},
								},
							},
						},
					},
				},
			},
			args: args{
				targets:       nil,
				watchIPBlocks: []string{"192.168.1.0/24"},
				tgb: &elbv2api.TargetGroupBinding{
					Spec: elbv2api.TargetGroupBindingSpec{
						TargetGroupARN: "my-tg",
						TargetType:     &ipTargetType,
					},
				},
			},
			want: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
						State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
						State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
					},
				},
			},
		},
		{
			name: "list with nil targets filtered by tags",
			fields: fields{
				describeInstancesAsListsCalls: []describeInstancesAsListsCall{
					{
						req: &ec2sdk.DescribeInstancesInput{
							Filters: []*ec2sdk.Filter{
								{
									Name:   aws.String("tag:someTag"),
									Values: []*string{aws.String("someValue")},
								},
							},
						},
						resp: []*ec2sdk.Instance{
							{
								InstanceId: awssdk.String("i-abcdefg2"),
							},
							{
								InstanceId: awssdk.String("i-abcdefg3"),
							},
						},
					},
				},
				describeTargetHealthWithContextCalls: []describeTargetHealthWithContextCall{
					{
						req: &elbv2sdk.DescribeTargetHealthInput{
							TargetGroupArn: awssdk.String("my-tg"),
							Targets:        nil,
						},
						resp: &elbv2sdk.DescribeTargetHealthOutput{
							TargetHealthDescriptions: []*elbv2sdk.TargetHealthDescription{
								{
									Target: &elbv2sdk.TargetDescription{
										Id:   awssdk.String("i-abcdefg1"),
										Port: awssdk.Int64(8080),
									},
									TargetHealth: &elbv2sdk.TargetHealth{
										Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
										State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
									},
								},
								{
									Target: &elbv2sdk.TargetDescription{
										Id:   awssdk.String("i-abcdefg2"),
										Port: awssdk.Int64(8080),
									},
									TargetHealth: &elbv2sdk.TargetHealth{
										Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
										State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
									},
								},
								{
									Target: &elbv2sdk.TargetDescription{
										Id:   awssdk.String("i-abcdefg3"),
										Port: awssdk.Int64(8080),
									},
									TargetHealth: &elbv2sdk.TargetHealth{
										Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
										State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
									},
								},
							},
						},
					},
				},
			},
			args: args{
				targets:              nil,
				watchInstanceFilters: []string{"tag:someTag=someValue"},
				tgb: &elbv2api.TargetGroupBinding{
					Spec: elbv2api.TargetGroupBindingSpec{
						TargetGroupARN: "my-tg",
						TargetType:     &instanceTargetType,
					},
				},
			},
			want: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("i-abcdefg2"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
						State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
					},
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("i-abcdefg3"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
						State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			elbv2Client := mock_services.NewMockELBV2(ctrl)
			for _, call := range tt.fields.describeTargetHealthWithContextCalls {
				elbv2Client.EXPECT().DescribeTargetHealthWithContext(gomock.Any(), call.req).Return(call.resp, call.err)
			}

			ec2Client := mock_services.NewMockEC2(ctrl)
			for _, call := range tt.fields.describeInstancesAsListsCalls {
				ec2Client.EXPECT().DescribeInstancesAsList(gomock.Any(), call.req).Return(call.resp, call.err)
			}

			m := &cachedTargetsManager{
				elbv2Client:          elbv2Client,
				ec2Client:            ec2Client,
				watchIPBlocks:        tt.args.watchIPBlocks,
				watchInstanceFilters: tt.args.watchInstanceFilters,
			}
			ctx := context.Background()
			got, err := m.listTargetsFromAWS(ctx, tt.args.tgb, tt.args.targets)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func Test_cachedTargetsManager_recordSuccessfulRegisterTargetsOperation(t *testing.T) {
	type fields struct {
		targetsCache map[string][]TargetInfo
	}
	type args struct {
		tgARN   string
		targets []elbv2sdk.TargetDescription
	}
	tests := []struct {
		name             string
		fields           fields
		args             args
		wantTargetsCache map[string][]TargetInfo
	}{
		{
			name: "targets for tgARN don't exists in cache",
			fields: fields{
				targetsCache: nil,
			},
			args: args{
				tgARN: "my-tg",
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
				},
			},
			wantTargetsCache: nil,
		},
		{
			name: "targets for tgARN exists in cache, and contain the target",
			fields: fields{
				targetsCache: map[string][]TargetInfo{
					"my-tg": {
						{
							Target: elbv2sdk.TargetDescription{
								Id:   awssdk.String("192.168.1.1"),
								Port: awssdk.Int64(8080),
							},
							TargetHealth: &elbv2sdk.TargetHealth{
								Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
								State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
							},
						},
					},
				},
			},
			args: args{
				tgARN: "my-tg",
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
				},
			},
			wantTargetsCache: map[string][]TargetInfo{
				"my-tg": {
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.1"),
							Port: awssdk.Int64(8080),
						},
					},
				},
			},
		},
		{
			name: "targets for tgARN exists in cache, but don't contain the target",
			fields: fields{
				targetsCache: map[string][]TargetInfo{
					"my-tg": {
						{
							Target: elbv2sdk.TargetDescription{
								Id:   awssdk.String("192.168.1.1"),
								Port: awssdk.Int64(8080),
							},
							TargetHealth: &elbv2sdk.TargetHealth{
								Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
								State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
							},
						},
					},
				},
			},
			args: args{
				tgARN: "my-tg",
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
				},
			},
			wantTargetsCache: map[string][]TargetInfo{
				"my-tg": {
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.1"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
							State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
						},
					},
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.2"),
							Port: awssdk.Int64(8080),
						},
					},
				},
			},
		},
		{
			name: "targets for tgARN exists in cache, and contains multiple targets",
			fields: fields{
				targetsCache: map[string][]TargetInfo{
					"my-tg": {
						{
							Target: elbv2sdk.TargetDescription{
								Id:   awssdk.String("192.168.1.1"),
								Port: awssdk.Int64(8080),
							},
							TargetHealth: &elbv2sdk.TargetHealth{
								Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
								State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
							},
						},
						{
							Target: elbv2sdk.TargetDescription{
								Id:   awssdk.String("192.168.1.2"),
								Port: awssdk.Int64(8080),
							},
							TargetHealth: &elbv2sdk.TargetHealth{
								Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
								State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
							},
						},
					},
				},
			},
			args: args{
				tgARN: "my-tg",
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.3"),
						Port: awssdk.Int64(8080),
					},
				},
			},
			wantTargetsCache: map[string][]TargetInfo{
				"my-tg": {
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.1"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
							State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
						},
					},
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.2"),
							Port: awssdk.Int64(8080),
						},
					},
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.3"),
							Port: awssdk.Int64(8080),
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targetsCache := cache.NewExpiring()
			targetsCacheTTL := 1 * time.Minute
			for tgARN, targets := range tt.fields.targetsCache {
				targetsCache.Set(tgARN, &targetsCacheItem{
					mutex:   sync.RWMutex{},
					targets: targets,
				}, targetsCacheTTL)
			}

			m := &cachedTargetsManager{
				targetsCache:      targetsCache,
				targetsCacheMutex: sync.RWMutex{},
			}
			m.recordSuccessfulRegisterTargetsOperation(tt.args.tgARN, tt.args.targets)
			assert.Equal(t, len(tt.wantTargetsCache), targetsCache.Len())
			for tgARN, targets := range tt.wantTargetsCache {
				rawTargetsCacheItem, exists := targetsCache.Get(tgARN)
				assert.True(t, exists)
				targetsCacheItem := rawTargetsCacheItem.(*targetsCacheItem)
				assert.ElementsMatch(t, targets, targetsCacheItem.targets)
			}
		})
	}
}

func Test_cachedTargetsManager_recordSuccessfulDeregisterTargetsOperation(t *testing.T) {
	type fields struct {
		targetsCache map[string][]TargetInfo
	}
	type args struct {
		tgARN   string
		targets []elbv2sdk.TargetDescription
	}
	tests := []struct {
		name             string
		fields           fields
		args             args
		wantTargetsCache map[string][]TargetInfo
	}{
		{
			name: "targets for tgARN don't exists in cache",
			fields: fields{
				targetsCache: nil,
			},
			args: args{
				tgARN: "my-tg",
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
				},
			},
			wantTargetsCache: nil,
		},
		{
			name: "targets for tgARN exists in cache, and contain the target",
			fields: fields{
				targetsCache: map[string][]TargetInfo{
					"my-tg": {
						{
							Target: elbv2sdk.TargetDescription{
								Id:   awssdk.String("192.168.1.1"),
								Port: awssdk.Int64(8080),
							},
							TargetHealth: &elbv2sdk.TargetHealth{
								Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
								State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
							},
						},
					},
				},
			},
			args: args{
				tgARN: "my-tg",
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
				},
			},
			wantTargetsCache: map[string][]TargetInfo{
				"my-tg": {
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.1"),
							Port: awssdk.Int64(8080),
						},
					},
				},
			},
		},
		{
			name: "targets for tgARN exists in cache, but don't contain the target",
			fields: fields{
				targetsCache: map[string][]TargetInfo{
					"my-tg": {
						{
							Target: elbv2sdk.TargetDescription{
								Id:   awssdk.String("192.168.1.1"),
								Port: awssdk.Int64(8080),
							},
							TargetHealth: &elbv2sdk.TargetHealth{
								Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
								State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
							},
						},
					},
				},
			},
			args: args{
				tgARN: "my-tg",
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
				},
			},
			wantTargetsCache: map[string][]TargetInfo{
				"my-tg": {
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.1"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
							State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
						},
					},
				},
			},
		},
		{
			name: "targets for tgARN exists in cache, and contains multiple targets",
			fields: fields{
				targetsCache: map[string][]TargetInfo{
					"my-tg": {
						{
							Target: elbv2sdk.TargetDescription{
								Id:   awssdk.String("192.168.1.1"),
								Port: awssdk.Int64(8080),
							},
							TargetHealth: &elbv2sdk.TargetHealth{
								Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
								State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
							},
						},
						{
							Target: elbv2sdk.TargetDescription{
								Id:   awssdk.String("192.168.1.2"),
								Port: awssdk.Int64(8080),
							},
							TargetHealth: &elbv2sdk.TargetHealth{
								Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
								State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
							},
						},
					},
				},
			},
			args: args{
				tgARN: "my-tg",
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.3"),
						Port: awssdk.Int64(8080),
					},
				},
			},
			wantTargetsCache: map[string][]TargetInfo{
				"my-tg": {
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.1"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
							State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
						},
					},
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.2"),
							Port: awssdk.Int64(8080),
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targetsCache := cache.NewExpiring()
			targetsCacheTTL := 1 * time.Minute
			for tgARN, targets := range tt.fields.targetsCache {
				targetsCache.Set(tgARN, &targetsCacheItem{
					mutex:   sync.RWMutex{},
					targets: targets,
				}, targetsCacheTTL)
			}

			m := &cachedTargetsManager{
				targetsCache:      targetsCache,
				targetsCacheMutex: sync.RWMutex{},
			}
			m.recordSuccessfulDeregisterTargetsOperation(tt.args.tgARN, tt.args.targets)
			assert.Equal(t, len(tt.wantTargetsCache), targetsCache.Len())
			for tgARN, targets := range tt.wantTargetsCache {
				rawTargetsCacheItem, exists := targetsCache.Get(tgARN)
				assert.True(t, exists)
				targetsCacheItem := rawTargetsCacheItem.(*targetsCacheItem)
				assert.ElementsMatch(t, targets, targetsCacheItem.targets)
			}
		})
	}
}

func Test_chunkTargetDescriptions(t *testing.T) {
	type args struct {
		targets   []elbv2sdk.TargetDescription
		chunkSize int
	}
	tests := []struct {
		name string
		args args
		want [][]elbv2sdk.TargetDescription
	}{
		{
			name: "can be evenly chunked",
			args: args{
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.3"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.4"),
						Port: awssdk.Int64(8080),
					},
				},
				chunkSize: 2,
			},
			want: [][]elbv2sdk.TargetDescription{
				{
					{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
				},
				{
					{
						Id:   awssdk.String("192.168.1.3"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.4"),
						Port: awssdk.Int64(8080),
					},
				},
			},
		},
		{
			name: "cannot be evenly chunked",
			args: args{
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.3"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.4"),
						Port: awssdk.Int64(8080),
					},
				},
				chunkSize: 3,
			},
			want: [][]elbv2sdk.TargetDescription{
				{
					{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.3"),
						Port: awssdk.Int64(8080),
					},
				},
				{

					{
						Id:   awssdk.String("192.168.1.4"),
						Port: awssdk.Int64(8080),
					},
				},
			},
		},
		{
			name: "chunkSize equal to total count",
			args: args{
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.3"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.4"),
						Port: awssdk.Int64(8080),
					},
				},
				chunkSize: 4,
			},
			want: [][]elbv2sdk.TargetDescription{
				{
					{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.3"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.4"),
						Port: awssdk.Int64(8080),
					},
				},
			},
		},
		{
			name: "chunkSize greater than total count",
			args: args{
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.3"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.4"),
						Port: awssdk.Int64(8080),
					},
				},
				chunkSize: 10,
			},
			want: [][]elbv2sdk.TargetDescription{
				{
					{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.3"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.4"),
						Port: awssdk.Int64(8080),
					},
				},
			},
		},
		{
			name: "chunk nil slice",
			args: args{
				targets:   nil,
				chunkSize: 2,
			},
			want: nil,
		},
		{
			name: "chunk empty slice",
			args: args{
				targets:   []elbv2sdk.TargetDescription{},
				chunkSize: 2,
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chunkTargetDescriptions(tt.args.targets, tt.args.chunkSize)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_pointerizeTargetDescriptions(t *testing.T) {
	type args struct {
		targets []elbv2sdk.TargetDescription
	}
	tests := []struct {
		name string
		args args
		want []*elbv2sdk.TargetDescription
	}{
		{
			name: "nil targets",
			args: args{
				targets: nil,
			},
			want: nil,
		},
		{
			name: "empty targets",
			args: args{
				targets: []elbv2sdk.TargetDescription{},
			},
			want: nil,
		},
		{
			name: "non-empty targets",
			args: args{
				targets: []elbv2sdk.TargetDescription{
					{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
				},
			},
			want: []*elbv2sdk.TargetDescription{
				{
					Id:   awssdk.String("192.168.1.1"),
					Port: awssdk.Int64(8080),
				},
				{
					Id:   awssdk.String("192.168.1.2"),
					Port: awssdk.Int64(8080),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pointerizeTargetDescriptions(tt.args.targets)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_cloneTargetInfoSlice(t *testing.T) {
	type args struct {
		targets []TargetInfo
	}
	tests := []struct {
		name string
		args args
		want []TargetInfo
	}{
		{
			name: "nil targets",
			args: args{
				targets: nil,
			},
			want: nil,
		},
		{
			name: "empty targets",
			args: args{
				targets: []TargetInfo{},
			},
			want: []TargetInfo{},
		},
		{
			name: "non-empty targets",
			args: args{
				targets: []TargetInfo{
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.1"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: nil,
					},
					{
						Target: elbv2sdk.TargetDescription{
							Id:   awssdk.String("192.168.1.2"),
							Port: awssdk.Int64(8080),
						},
						TargetHealth: &elbv2sdk.TargetHealth{
							Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
							State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
						},
					},
				},
			},
			want: []TargetInfo{
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.1"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: nil,
				},
				{
					Target: elbv2sdk.TargetDescription{
						Id:   awssdk.String("192.168.1.2"),
						Port: awssdk.Int64(8080),
					},
					TargetHealth: &elbv2sdk.TargetHealth{
						Reason: awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
						State:  awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cloneTargetInfoSlice(tt.args.targets)
			assert.Equal(t, tt.want, got)
		})
	}
}
