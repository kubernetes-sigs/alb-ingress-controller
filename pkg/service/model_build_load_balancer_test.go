package service

import (
	"context"
	"errors"
	elbv2sdk "github.com/aws/aws-sdk-go/service/elbv2"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/model/core"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/networking"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/annotations"
	elbv2deploy "sigs.k8s.io/aws-load-balancer-controller/pkg/deploy/elbv2"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/deploy/tracking"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/model/elbv2"
)

const lbAttrsDeletionProtectionEnabled = "deletion_protection.enabled"

func Test_defaultModelBuilderTask_buildLBAttributes(t *testing.T) {
	tests := []struct {
		testName  string
		svc       *corev1.Service
		wantError bool
		wantValue []elbv2.LoadBalancerAttribute
	}{
		{
			testName: "Default values",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"service.beta.kubernetes.io/aws-load-balancer-type": "nlb-ip",
					},
				},
			},
			wantError: false,
			wantValue: []elbv2.LoadBalancerAttribute{},
		},
		{
			testName: "Annotation specified",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"service.beta.kubernetes.io/aws-load-balancer-type":                              "nlb-ip",
						"service.beta.kubernetes.io/aws-load-balancer-access-log-enabled":                "true",
						"service.beta.kubernetes.io/aws-load-balancer-access-log-s3-bucket-name":         "nlb-bucket",
						"service.beta.kubernetes.io/aws-load-balancer-access-log-s3-bucket-prefix":       "bkt-pfx",
						"service.beta.kubernetes.io/aws-load-balancer-cross-zone-load-balancing-enabled": "true",
						"service.beta.kubernetes.io/aws-load-balancer-attributes":                        "deletion_protection.enabled=true",
					},
				},
			},
			wantError: false,
			wantValue: []elbv2.LoadBalancerAttribute{
				{
					Key:   lbAttrsAccessLogsS3Enabled,
					Value: "true",
				},
				{
					Key:   lbAttrsAccessLogsS3Bucket,
					Value: "nlb-bucket",
				},
				{
					Key:   lbAttrsAccessLogsS3Prefix,
					Value: "bkt-pfx",
				},
				{
					Key:   lbAttrsLoadBalancingCrossZoneEnabled,
					Value: "true",
				},
				{
					Key:   lbAttrsDeletionProtectionEnabled,
					Value: "true",
				},
			},
		},
		{
			testName: "Attributes from config map annotation",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"service.beta.kubernetes.io/aws-load-balancer-attributes": "access_logs.s3.enabled=true,access_logs.s3.bucket=nlb-bucket," +
							"access_logs.s3.prefix=bkt-pfx,load_balancing.cross_zone.enabled=true,deletion_protection.enabled=true",
					},
				},
			},
			wantError: false,
			wantValue: []elbv2.LoadBalancerAttribute{
				{
					Key:   lbAttrsAccessLogsS3Enabled,
					Value: "true",
				},
				{
					Key:   lbAttrsAccessLogsS3Bucket,
					Value: "nlb-bucket",
				},
				{
					Key:   lbAttrsAccessLogsS3Prefix,
					Value: "bkt-pfx",
				},
				{
					Key:   lbAttrsLoadBalancingCrossZoneEnabled,
					Value: "true",
				},
				{
					Key:   lbAttrsDeletionProtectionEnabled,
					Value: "true",
				},
			},
		},
		{
			testName: "Specific config overrides config map",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"service.beta.kubernetes.io/aws-load-balancer-attributes": "access_logs.s3.enabled=false,access_logs.s3.bucket=nlb-bucket," +
							"access_logs.s3.prefix=bkt-pfx,load_balancing.cross_zone.enabled=true",
						"service.beta.kubernetes.io/aws-load-balancer-access-log-enabled":                "true",
						"service.beta.kubernetes.io/aws-load-balancer-access-log-s3-bucket-name":         "overridden-nlb-bucket",
						"service.beta.kubernetes.io/aws-load-balancer-access-log-s3-bucket-prefix":       "overridden-bkt-pfx",
						"service.beta.kubernetes.io/aws-load-balancer-cross-zone-load-balancing-enabled": "false",
					},
				},
			},
			wantError: false,
			wantValue: []elbv2.LoadBalancerAttribute{
				{
					Key:   lbAttrsAccessLogsS3Enabled,
					Value: "true",
				},
				{
					Key:   lbAttrsAccessLogsS3Bucket,
					Value: "overridden-nlb-bucket",
				},
				{
					Key:   lbAttrsAccessLogsS3Prefix,
					Value: "overridden-bkt-pfx",
				},
				{
					Key:   lbAttrsLoadBalancingCrossZoneEnabled,
					Value: "false",
				},
			},
		},
		{
			testName: "Annotation invalid",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"service.beta.kubernetes.io/aws-load-balancer-type":                              "nlb-ip",
						"service.beta.kubernetes.io/aws-load-balancer-access-log-enabled":                "FalSe",
						"service.beta.kubernetes.io/aws-load-balancer-access-log-s3-bucket-name":         "nlb-bucket",
						"service.beta.kubernetes.io/aws-load-balancer-access-log-s3-bucket-prefix":       "bkt-pfx",
						"service.beta.kubernetes.io/aws-load-balancer-cross-zone-load-balancing-enabled": "true",
					},
				},
			},
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			parser := annotations.NewSuffixAnnotationParser("service.beta.kubernetes.io")
			builder := &defaultModelBuildTask{
				service:                              tt.svc,
				annotationParser:                     parser,
				defaultAccessLogsS3Bucket:            "",
				defaultAccessLogsS3Prefix:            "",
				defaultLoadBalancingCrossZoneEnabled: false,
				defaultProxyProtocolV2Enabled:        false,
				defaultHealthCheckProtocol:           elbv2.ProtocolTCP,
				defaultHealthCheckPort:               healthCheckPortTrafficPort,
				defaultHealthCheckPath:               "/",
				defaultHealthCheckInterval:           10,
				defaultHealthCheckTimeout:            10,
				defaultHealthCheckHealthyThreshold:   3,
				defaultHealthCheckUnhealthyThreshold: 3,
			}
			lbAttributes, err := builder.buildLoadBalancerAttributes(context.Background())
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.ElementsMatch(t, tt.wantValue, lbAttributes)
			}
		})
	}
}

func Test_defaultModelBuilderTask_buildSubnetMappings(t *testing.T) {
	tests := []struct {
		name    string
		scheme  elbv2.LoadBalancerScheme
		subnets []*ec2.Subnet
		want    []elbv2.SubnetMapping
		svc     *corev1.Service
		wantErr error
	}{
		{
			name:   "Multiple subnets",
			scheme: elbv2.LoadBalancerSchemeInternetFacing,
			subnets: []*ec2.Subnet{
				{
					SubnetId:         aws.String("subnet-1"),
					AvailabilityZone: aws.String("us-west-2a"),
					VpcId:            aws.String("vpc-1"),
				},
				{
					SubnetId:         aws.String("subnet-2"),
					AvailabilityZone: aws.String("us-west-2b"),
					VpcId:            aws.String("vpc-1"),
				},
			},
			svc: &corev1.Service{},
			want: []elbv2.SubnetMapping{
				{
					SubnetID: "subnet-1",
				},
				{
					SubnetID: "subnet-2",
				},
			},
		},
		{
			name:   "When EIP allocation is configured",
			scheme: elbv2.LoadBalancerSchemeInternetFacing,
			subnets: []*ec2.Subnet{
				{
					SubnetId:         aws.String("subnet-1"),
					AvailabilityZone: aws.String("us-west-2a"),
					VpcId:            aws.String("vpc-1"),
				},
				{
					SubnetId:         aws.String("subnet-2"),
					AvailabilityZone: aws.String("us-west-2b"),
					VpcId:            aws.String("vpc-1"),
				},
			},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"service.beta.kubernetes.io/aws-load-balancer-eip-allocations": "eip1, eip2",
					},
				},
			},
			want: []elbv2.SubnetMapping{
				{
					SubnetID:     "subnet-1",
					AllocationID: aws.String("eip1"),
				},
				{
					SubnetID:     "subnet-2",
					AllocationID: aws.String("eip2"),
				},
			},
		},
		{
			name:   "When EIP allocation and subnet mismatch",
			scheme: elbv2.LoadBalancerSchemeInternetFacing,
			subnets: []*ec2.Subnet{
				{
					SubnetId:         aws.String("subnet-1"),
					AvailabilityZone: aws.String("us-west-2a"),
					VpcId:            aws.String("vpc-1"),
				},
				{
					SubnetId:         aws.String("subnet-2"),
					AvailabilityZone: aws.String("us-west-2b"),
					VpcId:            aws.String("vpc-1"),
				},
			},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"service.beta.kubernetes.io/aws-load-balancer-eip-allocations": "eip1",
					},
				},
			},
			wantErr: errors.New("number of EIP allocations (1) and subnets (2) must match"),
		},
		{
			name:   "When PrivateIpv4Addresses is configured",
			scheme: elbv2.LoadBalancerSchemeInternal,
			subnets: []*ec2.Subnet{
				{
					SubnetId:         aws.String("subnet-1"),
					AvailabilityZone: aws.String("us-west-2a"),
					VpcId:            aws.String("vpc-1"),
					CidrBlock:        aws.String("172.17.0.0/16"),
				},
				{
					SubnetId:         aws.String("subnet-2"),
					AvailabilityZone: aws.String("us-west-2b"),
					VpcId:            aws.String("vpc-1"),
					CidrBlock:        aws.String("172.16.0.0/16"), // not in the same order as annoation
				},
			},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"service.beta.kubernetes.io/aws-load-balancer-private-ipv4-addresses": "172.16.1.1, 172.17.1.1",
					},
				},
			},
			want: []elbv2.SubnetMapping{
				{
					SubnetID:           "subnet-1",
					PrivateIPv4Address: aws.String("172.17.1.1"),
				},
				{
					SubnetID:           "subnet-2",
					PrivateIPv4Address: aws.String("172.16.1.1"),
				},
			},
		},
		{
			name:   "When PrivateIPv4Address outside of CIDR",
			scheme: elbv2.LoadBalancerSchemeInternal,
			subnets: []*ec2.Subnet{
				{
					SubnetId:         aws.String("subnet-1"),
					AvailabilityZone: aws.String("us-west-2a"),
					VpcId:            aws.String("vpc-1"),
					CidrBlock:        aws.String("172.17.0.0/16"),
				},
				{
					SubnetId:         aws.String("subnet-2"),
					AvailabilityZone: aws.String("us-west-2b"),
					VpcId:            aws.String("vpc-1"),
					CidrBlock:        aws.String("172.16.0.0/16"), // not in the same order as annoation
				},
			},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"service.beta.kubernetes.io/aws-load-balancer-private-ipv4-addresses": "172.100.1.1, 172.200.1.1",
					},
				},
			},
			wantErr: errors.New("no matching ip for subnet subnet-1"),
		},
		{
			name:   "When PrivateIpv4Addresses and subnet mismatch",
			scheme: elbv2.LoadBalancerSchemeInternal,
			subnets: []*ec2.Subnet{
				{
					SubnetId:         aws.String("subnet-1"),
					AvailabilityZone: aws.String("us-west-2a"),
					VpcId:            aws.String("vpc-1"),
				},
				{
					SubnetId:         aws.String("subnet-2"),
					AvailabilityZone: aws.String("us-west-2b"),
					VpcId:            aws.String("vpc-1"),
				},
			},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"service.beta.kubernetes.io/aws-load-balancer-private-ipv4-addresses": "172.16.1.1",
					},
				},
			},
			wantErr: errors.New("number of PrivateIpv4Addresses (1) and subnets (2) must match"),
		},
		{
			name:   "When both EIP allocation and PrivateIpv4Addresses set",
			scheme: elbv2.LoadBalancerSchemeInternal,
			subnets: []*ec2.Subnet{
				{
					SubnetId:         aws.String("subnet-1"),
					AvailabilityZone: aws.String("us-west-2a"),
					VpcId:            aws.String("vpc-1"),
				},
				{
					SubnetId:         aws.String("subnet-2"),
					AvailabilityZone: aws.String("us-west-2b"),
					VpcId:            aws.String("vpc-1"),
				},
			},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"service.beta.kubernetes.io/aws-load-balancer-private-ipv4-addresses": "172.16.1.1, 172.17.1.1",
						"service.beta.kubernetes.io/aws-load-balancer-eip-allocations":        "eip1, eip2",
					},
				},
			},
			wantErr: errors.New("only one of EIP allocations or PrivateIpv4Addresses can be set"),
		},
		{
			name:   "When EIP allocation and LoadBalancerSchemeInternal set",
			scheme: elbv2.LoadBalancerSchemeInternal,
			subnets: []*ec2.Subnet{
				{
					SubnetId:         aws.String("subnet-1"),
					AvailabilityZone: aws.String("us-west-2a"),
					VpcId:            aws.String("vpc-1"),
				},
				{
					SubnetId:         aws.String("subnet-2"),
					AvailabilityZone: aws.String("us-west-2b"),
					VpcId:            aws.String("vpc-1"),
				},
			},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"service.beta.kubernetes.io/aws-load-balancer-eip-allocations": "eip1, eip2",
					},
				},
			},
			wantErr: errors.New("EIP allocations can only be set for internet facing load balancers"),
		},
		{
			name:   "When PrivateIpv4Addresses and LoadBalancerSchemeInternetFacing set",
			scheme: elbv2.LoadBalancerSchemeInternetFacing,
			subnets: []*ec2.Subnet{
				{
					SubnetId:         aws.String("subnet-1"),
					AvailabilityZone: aws.String("us-west-2a"),
					VpcId:            aws.String("vpc-1"),
				},
				{
					SubnetId:         aws.String("subnet-2"),
					AvailabilityZone: aws.String("us-west-2b"),
					VpcId:            aws.String("vpc-1"),
				},
			},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"service.beta.kubernetes.io/aws-load-balancer-private-ipv4-addresses": "172.16.1.1, 172.17.1.1",
					},
				},
			},
			wantErr: errors.New("PrivateIpv4Addresses can only be set for internal balancers"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			annotationParser := annotations.NewSuffixAnnotationParser("service.beta.kubernetes.io")
			builder := &defaultModelBuildTask{service: tt.svc, annotationParser: annotationParser}
			got, err := builder.buildLoadBalancerSubnetMappings(context.Background(), tt.scheme, tt.subnets)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func Test_defaultModelBuilderTask_getMatchingIPforSubnet(t *testing.T) {
	tests := []struct {
		name                 string
		subnet               *ec2.Subnet
		privateIpv4Addresses []string
		want                 string
		wantErr              error
	}{
		{
			name: "When ip is found for subnet",
			subnet: &ec2.Subnet{
				SubnetId:         aws.String("subnet-1"),
				AvailabilityZone: aws.String("us-west-2a"),
				VpcId:            aws.String("vpc-1"),
				CidrBlock:        aws.String("172.16.0.0/16"),
			},
			privateIpv4Addresses: []string{"172.17.1.1", "172.16.1.1"},
			want:                 "172.16.1.1",
		},
		{
			name: "When CIDR cannot be parsed",
			subnet: &ec2.Subnet{
				SubnetId:         aws.String("subnet-1"),
				AvailabilityZone: aws.String("us-west-2a"),
				VpcId:            aws.String("vpc-1"),
				CidrBlock:        aws.String("172.16.0.0.0/16"),
			},
			privateIpv4Addresses: []string{"172.17.1.1", "172.16.1.1"},
			wantErr:              errors.New("subnet CIDR block could not be parsed: invalid CIDR address: 172.16.0.0.0/16"),
		},
		{
			name: "When IP cannot be parsed",
			subnet: &ec2.Subnet{
				SubnetId:         aws.String("subnet-1"),
				AvailabilityZone: aws.String("us-west-2a"),
				VpcId:            aws.String("vpc-1"),
				CidrBlock:        aws.String("172.16.0.0/16"),
			},
			privateIpv4Addresses: []string{"172.17.1.1.1", "172.16.1.1"},
			wantErr:              errors.New("cannot parse ip 172.17.1.1.1"),
		},
		{
			name: "When no valid ip in cidr range",
			subnet: &ec2.Subnet{
				SubnetId:         aws.String("subnet-1"),
				AvailabilityZone: aws.String("us-west-2a"),
				VpcId:            aws.String("vpc-1"),
				CidrBlock:        aws.String("172.16.0.0/16"),
			},
			privateIpv4Addresses: []string{"172.100.1.1", "172.200.1.1"},
			wantErr:              errors.New("no matching ip for subnet subnet-1"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			annotationParser := annotations.NewSuffixAnnotationParser("service.beta.kubernetes.io")
			builder := &defaultModelBuildTask{service: nil, annotationParser: annotationParser}
			got, err := builder.getMatchingIPforSubnet(context.Background(), tt.subnet, tt.privateIpv4Addresses)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func Test_defaultModelBuilderTask_buildLoadBalancerSubnets(t *testing.T) {
	type resolveSubnetResults struct {
		subnets []*ec2.Subnet
		err     error
	}
	type args struct {
		stack core.Stack
	}
	type listLoadBalancerCall struct {
		sdkLBs  []elbv2deploy.LoadBalancerWithTags
		err			error
	}
	listLoadBalancerCallForEmptyLB := listLoadBalancerCall{
		sdkLBs: []elbv2deploy.LoadBalancerWithTags{},
	}
	tests := []struct {
		name                     string
		svc                      *corev1.Service
		scheme                   elbv2.LoadBalancerScheme
		provider 				 tracking.Provider
		args 					 args
		listLoadBalancersCalls	     	 []listLoadBalancerCall
		resolveViaDiscoveryCalls      []resolveSubnetResults
		resolveViaNameOrIDSlilceCalls []resolveSubnetResults
		want     				 map[string]string
	}{
		{
			name:   "subnet auto-discovery",
			svc:    &corev1.Service{},
			scheme: elbv2.LoadBalancerSchemeInternal,
			provider: tracking.NewDefaultProvider("service.k8s.aws", "cluster-name"),
			args:     args{stack: core.NewDefaultStack(core.StackID{Namespace: "namespace", Name: "serviceName"})},
			listLoadBalancersCalls: []listLoadBalancerCall{listLoadBalancerCallForEmptyLB},
			resolveViaDiscoveryCalls: []resolveSubnetResults{
					{
						subnets: []*ec2.Subnet{
							{
								SubnetId:  aws.String("subnet-a"),
								CidrBlock: aws.String("192.168.0.0/19"),
							},
							{
								SubnetId:  aws.String("subnet-b"),
								CidrBlock: aws.String("192.168.32.0/19"),
							},
						},
					},
				},
			want: map[string]string{
				"elbv2.k8s.aws/cluster": "cluster-name",
				"service.k8s.aws/stack": "namespace/serviceName",
			},
		},
		{
			name: "subnet annotation",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"service.beta.kubernetes.io/aws-load-balancer-subnets": "subnet-abc, Subnet Name XYZ",
					},
				},
			},
			scheme: elbv2.LoadBalancerSchemeInternal,
			provider: tracking.NewDefaultProvider("service.k8s.aws", "cluster-name"),
			args:     args{stack: core.NewDefaultStack(core.StackID{Namespace: "namespace", Name: "serviceName"})},
			resolveViaNameOrIDSlilceCalls: []resolveSubnetResults{
					{
						subnets: []*ec2.Subnet{
							{
								SubnetId:  aws.String("subnet-abc"),
								CidrBlock: aws.String("192.168.0.0/19"),
							},
							{
								SubnetId:  aws.String("subnet-xyz"),
								CidrBlock: aws.String("192.168.0.0/19"),
							},
						},
					},
				},
			want: 	map[string]string{
				"elbv2.k8s.aws/cluster": "cluster-name",
				"service.k8s.aws/stack": "namespace/serviceName",
			},
		},
		{
			name:   "subnet resolve via Name or ID",
			svc:    &corev1.Service{},
			scheme: elbv2.LoadBalancerSchemeInternal,
			provider: tracking.NewDefaultProvider("service.k8s.aws", "cluster-name"),
			args:     args{stack: core.NewDefaultStack(core.StackID{Namespace: "namespace", Name: "serviceName"})},
			listLoadBalancersCalls: []listLoadBalancerCall{
					{
						sdkLBs: []elbv2deploy.LoadBalancerWithTags{
							{
								LoadBalancer: &elbv2sdk.LoadBalancer{
									LoadBalancerArn: aws.String("lb-1"),
									AvailabilityZones: []*elbv2sdk.AvailabilityZone{
										{
											SubnetId: aws.String("subnet-c"),
										},
										{
											SubnetId: aws.String("subnet-d"),
										},
									},
								},
								Tags: map[string]string{
									"elbv2.k8s.aws/cluster": "cluster-name",
									"service.k8s.aws/stack": "namespace/serviceName",
								},
							},
						},
					},
				},
			resolveViaNameOrIDSlilceCalls: []resolveSubnetResults{
					{
						subnets: []*ec2.Subnet{
							{
								SubnetId:  aws.String("subnet-c"),
								CidrBlock: aws.String("192.168.0.0/19"),
							},
							{
								SubnetId:  aws.String("subnet-d"),
								CidrBlock: aws.String("192.168.0.0/19"),
							},
						},
					},
				},
			want: map[string]string{
				"elbv2.k8s.aws/cluster": "cluster-name",
				"service.k8s.aws/stack": "namespace/serviceName",
			},
		},

	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			elbv2TaggingManager := elbv2deploy.NewMockTaggingManager(ctrl)
			for _, call :=range tt.listLoadBalancersCalls {
				elbv2TaggingManager.EXPECT().ListLoadBalancers(gomock.Any(), gomock.Any()).Return(call.sdkLBs, call.err)
			}

			subnetsResolver := networking.NewMockSubnetsResolver(ctrl)
			for _, call := range tt.resolveViaDiscoveryCalls{
				subnetsResolver.EXPECT().ResolveViaDiscovery(gomock.Any(), gomock.Any()).Return(call.subnets, call.err)
			}
			for _, call := range tt.resolveViaNameOrIDSlilceCalls {
				subnetsResolver.EXPECT().ResolveViaNameOrIDSlice(gomock.Any(), gomock.Any(), gomock.Any()).Return(call.subnets, call.err)
			}
			annotationParser := annotations.NewSuffixAnnotationParser("service.beta.kubernetes.io")

			clusterName := "cluster-name"
			trackingProvider := tracking.NewDefaultProvider("ingress.k8s.aws", clusterName)

			builder := &defaultModelBuildTask{
					clusterName: 			clusterName,
					service: 				tt.svc,
					stack: 					tt.args.stack,
					annotationParser: 		annotationParser,
					subnetsResolver: 		subnetsResolver,
					trackingProvider: 		trackingProvider,
					elbv2TaggingManager:	elbv2TaggingManager,
				}
			var got = tt.provider.StackTags(tt.args.stack)
			assert.Equal(t, tt.want, got)

			builder.buildLoadBalancerSubnets(context.Background(), tt.scheme)
		})
	}
}

func Test_defaultModelBuildTask_buildLoadBalancerIPAddressType(t *testing.T) {
	tests := []struct {
		name    string
		service *corev1.Service
		want    elbv2.IPAddressType
		wantErr bool
	}{
		{
			name: "ipv4_specified_expect_ipv4",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"service.beta.kubernetes.io/aws-load-balancer-ip-address-type": "ipv4"},
				},
			},
			want:    elbv2.IPAddressTypeIPV4,
			wantErr: false,
		},
		{
			name: "dualstack_specified_expect_dualstack",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"service.beta.kubernetes.io/aws-load-balancer-ip-address-type": "dualstack"},
				},
			},
			want:    elbv2.IPAddressTypeDualStack,
			wantErr: false,
		},
		{
			name: "default_value_no_ip_address_type_specified_expect_ipv4",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"service.beta.kubernetes.io/aws-load-balancer-other-annotation": "somevalue"},
				},
			},
			want:    elbv2.IPAddressTypeIPV4,
			wantErr: false,
		},
		{
			name: "invalid_value_expect_error",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"service.beta.kubernetes.io/aws-load-balancer-ip-address-type": "DualStack"},
				},
			},
			want:    "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := annotations.NewSuffixAnnotationParser("service.beta.kubernetes.io")
			builder := &defaultModelBuildTask{
				annotationParser:     parser,
				service:              tt.service,
				defaultIPAddressType: elbv2.IPAddressTypeIPV4,
			}

			got, err := builder.buildLoadBalancerIPAddressType(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("buildLoadBalancerIPAddressType() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("buildLoadBalancerIPAddressType() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_defaultModelBuildTask_buildAdditionalResourceTags(t *testing.T) {
	type fields struct {
		service             *corev1.Service
		defaultTags         map[string]string
		externalManagedTags sets.String
	}
	tests := []struct {
		name    string
		fields  fields
		want    map[string]string
		wantErr error
	}{
		{
			name: "empty default tags, no tags annotation",
			fields: fields{
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{},
					},
				},
				defaultTags: nil,
			},
			want: map[string]string{},
		},
		{
			name: "empty default tags, non-empty tags annotation",
			fields: fields{
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"service.beta.kubernetes.io/aws-load-balancer-additional-resource-tags": "k1=v1,k2=v2",
						},
					},
				},
				defaultTags: nil,
			},
			want: map[string]string{
				"k1": "v1",
				"k2": "v2",
			},
		},
		{
			name: "non-empty default tags, empty tags annotation",
			fields: fields{
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{},
					},
				},
				defaultTags: map[string]string{
					"k3": "v3",
					"k4": "v4",
				},
			},
			want: map[string]string{
				"k3": "v3",
				"k4": "v4",
			},
		},
		{
			name: "non-empty default tags, non-empty tags annotation",
			fields: fields{
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"service.beta.kubernetes.io/aws-load-balancer-additional-resource-tags": "k1=v1,k2=v2,k3=v3a",
						},
					},
				},
				defaultTags: map[string]string{
					"k3": "v3",
					"k4": "v4",
				},
			},
			want: map[string]string{
				"k1": "v1",
				"k2": "v2",
				"k3": "v3",
				"k4": "v4",
			},
		},
		{
			name: "non-empty external tags, non-empty tags annotation - no collision",
			fields: fields{
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"service.beta.kubernetes.io/aws-load-balancer-additional-resource-tags": "k1=v1,k2=v2,k3=v3a",
						},
					},
				},
				externalManagedTags: sets.NewString("k4"),
			},
			want: map[string]string{
				"k1": "v1",
				"k2": "v2",
				"k3": "v3a",
			},
		},
		{
			name: "non-empty external tags, non-empty tags annotation - has collision",
			fields: fields{
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"service.beta.kubernetes.io/aws-load-balancer-additional-resource-tags": "k1=v1,k2=v2,k3=v3a",
						},
					},
				},
				externalManagedTags: sets.NewString("k3", "k4"),
			},
			wantErr: errors.New("external managed tag key k3 cannot be specified on Service"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &defaultModelBuildTask{
				service:             tt.fields.service,
				defaultTags:         tt.fields.defaultTags,
				externalManagedTags: tt.fields.externalManagedTags,
				annotationParser:    annotations.NewSuffixAnnotationParser("service.beta.kubernetes.io"),
			}
			got, err := task.buildAdditionalResourceTags(context.Background())
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func Test_defaultModelBuildTask_buildLoadBalancerName(t *testing.T) {
	tests := []struct {
		name        string
		service     *corev1.Service
		clusterName string
		scheme      elbv2.LoadBalancerScheme
		want        string
	}{
		{
			name: "no name annotation",

			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "foo",
					Name:        "bar",
					Annotations: map[string]string{},
				},
			},
			scheme: elbv2.LoadBalancerSchemeInternetFacing,
			want:   "k8s-foo-bar-e053368fb2",
		},
		{
			name: "non-empty name annotation",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "bar",
					Annotations: map[string]string{
						"service.beta.kubernetes.io/aws-load-balancer-name": "baz",
					},
				},
			},
			want: "baz",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &defaultModelBuildTask{
				service:          tt.service,
				clusterName:      tt.clusterName,
				annotationParser: annotations.NewSuffixAnnotationParser("service.beta.kubernetes.io"),
			}
			got := task.buildLoadBalancerName(context.Background(), tt.scheme)
			assert.Equal(t, tt.want, got)
		})
	}
}
