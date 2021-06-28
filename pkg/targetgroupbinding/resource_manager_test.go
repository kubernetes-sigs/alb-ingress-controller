package targetgroupbinding

import (
	"context"
	"testing"

	awssdk "github.com/aws/aws-sdk-go/aws"
	ec2sdk "github.com/aws/aws-sdk-go/service/ec2"
	elbv2sdk "github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/equality"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/k8s"
	testclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func Test_defaultResourceManager_updateTargetHealthPodConditionForPod(t *testing.T) {
	type env struct {
		pods []*corev1.Pod
	}

	type args struct {
		pod                  k8s.PodInfo
		targetHealth         *elbv2sdk.TargetHealth
		targetHealthCondType corev1.PodConditionType
	}

	tests := []struct {
		name    string
		env     env
		args    args
		want    bool
		wantPod *corev1.Pod
		wantErr error
	}{
		{
			name: "pod contains readinessGate and targetHealth is healthy - add pod condition",
			env: env{
				pods: []*corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "default",
							Name:      "my-pod",
							UID:       "my-pod-uuid",
						},
						Spec: corev1.PodSpec{
							ReadinessGates: []corev1.PodReadinessGate{
								{
									ConditionType: "target-health.elbv2.k8s.aws/my-tgb",
								},
							},
						},
						Status: corev1.PodStatus{
							Conditions: []corev1.PodCondition{
								{
									Type:   corev1.ContainersReady,
									Status: corev1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			args: args{
				pod: k8s.PodInfo{
					Key: types.NamespacedName{Namespace: "default", Name: "my-pod"},
					UID: "my-pod-uuid",
					ReadinessGates: []corev1.PodReadinessGate{
						{
							ConditionType: "target-health.elbv2.k8s.aws/my-tgb",
						},
					},
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.ContainersReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
				targetHealth: &elbv2sdk.TargetHealth{
					State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
				},
				targetHealthCondType: "target-health.elbv2.k8s.aws/my-tgb",
			},
			want: false,
			wantPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "my-pod",
					UID:       "my-pod-uuid",
				},
				Spec: corev1.PodSpec{
					ReadinessGates: []corev1.PodReadinessGate{
						{
							ConditionType: "target-health.elbv2.k8s.aws/my-tgb",
						},
					},
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   "target-health.elbv2.k8s.aws/my-tgb",
							Status: corev1.ConditionTrue,
						},
						{
							Type:   corev1.ContainersReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
		},
		{
			name: "pod contains readinessGate and targetHealth is healthy - update pod condition",
			env: env{
				pods: []*corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "default",
							Name:      "my-pod",
							UID:       "my-pod-uuid",
						},
						Spec: corev1.PodSpec{
							ReadinessGates: []corev1.PodReadinessGate{
								{
									ConditionType: "target-health.elbv2.k8s.aws/my-tgb",
								},
							},
						},
						Status: corev1.PodStatus{
							Conditions: []corev1.PodCondition{
								{
									Type:   "target-health.elbv2.k8s.aws/my-tgb",
									Status: corev1.ConditionFalse,
								},
								{
									Type:   corev1.ContainersReady,
									Status: corev1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			args: args{
				pod: k8s.PodInfo{
					Key: types.NamespacedName{Namespace: "default", Name: "my-pod"},
					UID: "my-pod-uuid",
					ReadinessGates: []corev1.PodReadinessGate{
						{
							ConditionType: "target-health.elbv2.k8s.aws/my-tgb",
						},
					},
					Conditions: []corev1.PodCondition{
						{
							Type:   "target-health.elbv2.k8s.aws/my-tgb",
							Status: corev1.ConditionFalse,
						},
						{
							Type:   corev1.ContainersReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
				targetHealth: &elbv2sdk.TargetHealth{
					State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
				},
				targetHealthCondType: "target-health.elbv2.k8s.aws/my-tgb",
			},
			want: false,
			wantPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "my-pod",
					UID:       "my-pod-uuid",
				},
				Spec: corev1.PodSpec{
					ReadinessGates: []corev1.PodReadinessGate{
						{
							ConditionType: "target-health.elbv2.k8s.aws/my-tgb",
						},
					},
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   "target-health.elbv2.k8s.aws/my-tgb",
							Status: corev1.ConditionTrue,
						},
						{
							Type:   corev1.ContainersReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
		},
		{
			name: "pod contains readinessGate and targetHealth is unhealthy - update pod condition",
			env: env{
				pods: []*corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "default",
							Name:      "my-pod",
							UID:       "my-pod-uuid",
						},
						Spec: corev1.PodSpec{
							ReadinessGates: []corev1.PodReadinessGate{
								{
									ConditionType: "target-health.elbv2.k8s.aws/my-tgb",
								},
							},
						},
						Status: corev1.PodStatus{
							Conditions: []corev1.PodCondition{
								{
									Type:    "target-health.elbv2.k8s.aws/my-tgb",
									Status:  corev1.ConditionFalse,
									Reason:  elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress,
									Message: "Target registration is in progress",
								},
								{
									Type:   corev1.ContainersReady,
									Status: corev1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			args: args{
				pod: k8s.PodInfo{
					Key: types.NamespacedName{Namespace: "default", Name: "my-pod"},
					UID: "my-pod-uuid",
					ReadinessGates: []corev1.PodReadinessGate{
						{
							ConditionType: "target-health.elbv2.k8s.aws/my-tgb",
						},
					},
					Conditions: []corev1.PodCondition{
						{
							Type:    "target-health.elbv2.k8s.aws/my-tgb",
							Status:  corev1.ConditionFalse,
							Reason:  elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress,
							Message: "Target registration is in progress",
						},
						{
							Type:   corev1.ContainersReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
				targetHealth: &elbv2sdk.TargetHealth{
					State:       awssdk.String(elbv2sdk.TargetHealthStateEnumUnhealthy),
					Reason:      awssdk.String(elbv2sdk.TargetHealthReasonEnumTargetFailedHealthChecks),
					Description: awssdk.String("Health checks failed"),
				},
				targetHealthCondType: "target-health.elbv2.k8s.aws/my-tgb",
			},
			want: true,
			wantPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "my-pod",
					UID:       "my-pod-uuid",
				},
				Spec: corev1.PodSpec{
					ReadinessGates: []corev1.PodReadinessGate{
						{
							ConditionType: "target-health.elbv2.k8s.aws/my-tgb",
						},
					},
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:    "target-health.elbv2.k8s.aws/my-tgb",
							Status:  corev1.ConditionFalse,
							Reason:  elbv2sdk.TargetHealthReasonEnumTargetFailedHealthChecks,
							Message: "Health checks failed",
						},
						{
							Type:   corev1.ContainersReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
		},
		{
			name: "pod contains readinessGate and targetHealth is nil",
			env: env{
				pods: []*corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "default",
							Name:      "my-pod",
							UID:       "my-pod-uuid",
						},
						Spec: corev1.PodSpec{
							ReadinessGates: []corev1.PodReadinessGate{
								{
									ConditionType: "target-health.elbv2.k8s.aws/my-tgb",
								},
							},
						},
						Status: corev1.PodStatus{
							Conditions: []corev1.PodCondition{
								{
									Type:   corev1.ContainersReady,
									Status: corev1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			args: args{
				pod: k8s.PodInfo{
					Key: types.NamespacedName{Namespace: "default", Name: "my-pod"},
					UID: "my-pod-uuid",
					ReadinessGates: []corev1.PodReadinessGate{
						{
							ConditionType: "target-health.elbv2.k8s.aws/my-tgb",
						},
					},
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.ContainersReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
				targetHealth:         nil,
				targetHealthCondType: "target-health.elbv2.k8s.aws/my-tgb",
			},
			want: true,
			wantPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "my-pod",
					UID:       "my-pod-uuid",
				},
				Spec: corev1.PodSpec{
					ReadinessGates: []corev1.PodReadinessGate{
						{
							ConditionType: "target-health.elbv2.k8s.aws/my-tgb",
						},
					},
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   "target-health.elbv2.k8s.aws/my-tgb",
							Status: corev1.ConditionUnknown,
						},
						{
							Type:   corev1.ContainersReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8sSchema := runtime.NewScheme()
			clientgoscheme.AddToScheme(k8sSchema)
			k8sClient := testclient.NewFakeClientWithScheme(k8sSchema)

			m := &defaultResourceManager{
				k8sClient: k8sClient,
				logger:    &log.NullLogger{},
			}

			ctx := context.Background()
			for _, pod := range tt.env.pods {
				err := k8sClient.Create(ctx, pod.DeepCopy())
				assert.NoError(t, err)
			}

			got, err := m.updateTargetHealthPodConditionForPod(context.Background(),
				tt.args.pod, tt.args.targetHealth, tt.args.targetHealthCondType)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)

				updatedPod := &corev1.Pod{}
				err := m.k8sClient.Get(ctx, tt.args.pod.Key, updatedPod)
				assert.NoError(t, err)

				opts := cmp.Options{
					equality.IgnoreFakeClientPopulatedFields(),
					cmpopts.IgnoreTypes(metav1.Time{}),
				}
				assert.True(t, cmp.Equal(tt.wantPod, updatedPod, opts), "diff", cmp.Diff(tt.wantPod, updatedPod, opts))
			}
		})
	}
}

func Test_containsTargetsInInitialState(t *testing.T) {
	type args struct {
		matchedEndpointAndTargets []podEndpointAndTargetPair
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "contains initial targets",
			args: args{
				matchedEndpointAndTargets: []podEndpointAndTargetPair{
					{
						target: TargetInfo{
							TargetHealth: &elbv2sdk.TargetHealth{
								State:       awssdk.String(elbv2sdk.TargetHealthStateEnumInitial),
								Reason:      awssdk.String(elbv2sdk.TargetHealthReasonEnumElbRegistrationInProgress),
								Description: awssdk.String("Target registration is in progress"),
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "contains no initial targets",
			args: args{
				matchedEndpointAndTargets: []podEndpointAndTargetPair{
					{
						target: TargetInfo{
							TargetHealth: &elbv2sdk.TargetHealth{
								State: awssdk.String(elbv2sdk.TargetHealthStateEnumHealthy),
							},
						},
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsTargetsInInitialState(tt.args.matchedEndpointAndTargets)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_buildPodConditionPatch(t *testing.T) {
	type args struct {
		pod       k8s.PodInfo
		condition corev1.PodCondition
	}
	tests := []struct {
		name      string
		args      args
		wantPatch []byte
		wantErr   error
	}{
		{
			name: "standard case",
			args: args{
				pod: k8s.PodInfo{
					Key: types.NamespacedName{Namespace: "ns-1", Name: "pod-1"},
					UID: "pod-uuid",
				},
				condition: corev1.PodCondition{
					Type:    "custom-condition",
					Status:  corev1.ConditionTrue,
					Reason:  "some-reason",
					Message: "some-msg",
				},
			},
			wantPatch: []byte(`{"metadata":{"uid":"pod-uuid"},"status":{"conditions":[{"lastProbeTime":null,"lastTransitionTime":null,"message":"some-msg","reason":"some-reason","status":"True","type":"custom-condition"}]}}`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildPodConditionPatch(tt.args.pod, tt.args.condition)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
				gotPatch, _ := got.Data(nil)
				assert.Equal(t, tt.wantPatch, gotPatch)
				assert.Equal(t, types.StrategicMergePatchType, got.Type())
			}
		})
	}
}

func Test_isELBV2TargetInELBVPC(t *testing.T) {
	v := &ec2sdk.Vpc{
		CidrBlock: awssdk.String("192.168.1.0/24"),
		CidrBlockAssociationSet: []*ec2sdk.VpcCidrBlockAssociation{
			{
				CidrBlock: awssdk.String("10.10.10.0/24"),
			},
			{
				CidrBlock: awssdk.String("10.10.20.0/24"),
			},
		},
	}

	type args struct {
		podIP string
		vpc   *ec2sdk.Vpc
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "pod IP is in VPC CIDR",
			args: args{
				podIP: "192.168.1.1",
				vpc:   v,
			},
			want: true,
		},
		{
			name: "pod IP is in first secondary VPC CIDR",
			args: args{
				podIP: "10.10.10.1",
				vpc:   v,
			},
			want: true,
		},
		{
			name: "pod IP is in second secondary first VPC CIDR",
			args: args{
				podIP: "10.10.20.1",
				vpc:   v,
			},
			want: true,
		},
		{
			name: "pod IP is not in VPC",
			args: args{
				podIP: "10.0.0.1",
				vpc:   v,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isELBV2TargetInELBVPC(tt.args.podIP, tt.args.vpc); got != tt.want {
				t.Errorf("isELBV2TargetInELBVPC() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_isIPinCIDR(t *testing.T) {
	type args struct {
		ipAddr    string
		cidrBlock string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "yes",
			args: args{
				ipAddr:    "10.10.10.10",
				cidrBlock: "10.0.0.0/8",
			},
			want: true,
		},
		{
			name: "no",
			args: args{
				ipAddr:    "10.10.10.10",
				cidrBlock: "10.0.0.0/24",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isIPinCIDR(tt.args.ipAddr, tt.args.cidrBlock); got != tt.want {
				t.Errorf("isIPinCIDR() = %v, want %v", got, tt.want)
			}
		})
	}
}
