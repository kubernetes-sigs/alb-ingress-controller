package networking

import (
	"context"
	"reflect"
	"testing"

	awssdk "github.com/aws/aws-sdk-go/aws"
	ec2sdk "github.com/aws/aws-sdk-go/service/ec2"
	gomock "github.com/golang/mock/gomock"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/aws/services"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func Test_defaultVPCInfoProvider_FetchVPCInfo(t *testing.T) {
	type describeVpcCall struct {
		input  *ec2sdk.DescribeVpcsInput
		output *ec2sdk.DescribeVpcsOutput
		err    error
	}

	type fields struct {
		describeVpcCalls []describeVpcCall
	}
	tests := []struct {
		name    string
		fields  fields
		want    *ec2sdk.Vpc
		wantErr bool
	}{
		{
			name: "from AWS",
			fields: fields{
				describeVpcCalls: []describeVpcCall{
					{
						input: &ec2sdk.DescribeVpcsInput{
							VpcIds: []*string{awssdk.String("vpc-2f09a348")},
						},
						output: &ec2sdk.DescribeVpcsOutput{
							Vpcs: []*ec2sdk.Vpc{{VpcId: awssdk.String("vpc-2f09a348")}},
						},
						err: nil,
					},
				},
			},
			want: &ec2sdk.Vpc{
				VpcId: awssdk.String("vpc-2f09a348"),
			},
			wantErr: false,
		},
		{
			name:   "from cache",
			fields: fields{},
			want: &ec2sdk.Vpc{
				VpcId: awssdk.String("vpc-2f09a348"),
			},
			wantErr: false,
		},
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ec2Client := services.NewMockEC2(ctrl)
	p := NewDefaultVPCInfoProvider(5, ec2Client, &log.NullLogger{})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, call := range tt.fields.describeVpcCalls {
				ec2Client.EXPECT().DescribeVpcsWithContext(gomock.Any(), call.input).Return(call.output, call.err)
			}

			got, err := p.FetchVPCInfo(context.Background(), "vpc-2f09a348")
			if (err != nil) != tt.wantErr {
				t.Errorf("defaultVPCInfoProvider.FetchVPCInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("defaultVPCInfoProvider.FetchVPCInfo() = %v, want %v", got, tt.want)
			}
		})
	}
}
