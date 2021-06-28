package framework

import (
	"flag"

	"github.com/pkg/errors"
)

var globalOptions Options

func init() {
	globalOptions.BindFlags()
}

// configuration options
type Options struct {
	ClusterName string
	AWSRegion   string
	AWSVPCID    string
	KubeConfig  string

	// AWS Load Balancer Controller image. leave empty to use default one from helm chart.
	ControllerImage string

	// Additional parameters for e2e tests
	S3BucketName    string
	CertificateARNs string
}

func (options *Options) BindFlags() {
	flag.StringVar(&options.ClusterName, "cluster-name", "", `Kubernetes cluster name (required)`)
	flag.StringVar(&options.AWSRegion, "aws-region", "", `AWS Region for the kubernetes cluster`)
	flag.StringVar(&options.AWSVPCID, "aws-vpc-id", "", `ID of VPC to create load balancers in`)

	flag.StringVar(&options.ControllerImage, "controller-image", "", `AWS Load Balancer Controller image`)

	flag.StringVar(&options.S3BucketName, "s3-bucket-name", "", `S3 bucket to use for testing load balancer access logging feature`)
	flag.StringVar(&options.CertificateARNs, "certificate-arns", "", `Certificate ARNs to use for TLS listeners`)
}

func (options *Options) Validate() error {
	if err := options.rebindFlags(); err != nil {
		return err
	}
	if len(options.ClusterName) == 0 {
		return errors.Errorf("%s must be set!", "cluster-name")
	}
	if len(options.AWSRegion) == 0 {
		return errors.Errorf("%s must be set!", "aws-region")
	}
	if len(options.AWSVPCID) == 0 {
		return errors.Errorf("%s must be set!", "aws-vpc-id")
	}
	return nil
}

func (options *Options) rebindFlags() error {
	// kubeconfig is already defined by controller-runtime. we rebind it to our KubeConfig variable.
	kubeConfigFlag := flag.Lookup("kubeconfig")
	if kubeConfigFlag == nil {
		return errors.Errorf("%s must be set!", "kubeconfig")
	}
	options.KubeConfig = kubeConfigFlag.Value.(flag.Getter).Get().(string)
	return nil
}
