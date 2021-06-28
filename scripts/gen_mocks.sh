## Note: mocks for interfaces from this project should be along with the original package.
##       mocks for interfaces from 3rd-party project should be put inside ./mocks folder.
## mockgen version v1.5.0
~/go/bin/mockgen -package=mock_client -destination=./mocks/controller-runtime/client/client_mocks.go sigs.k8s.io/controller-runtime/pkg/client Client
~/go/bin/mockgen -package=services -destination=./pkg/aws/services/elbv2_mocks.go sigs.k8s.io/aws-load-balancer-controller/pkg/aws/services ELBV2
~/go/bin/mockgen -package=services -destination=./pkg/aws/services/ec2_mocks.go sigs.k8s.io/aws-load-balancer-controller/pkg/aws/services EC2
~/go/bin/mockgen -package=webhook -destination=./pkg/webhook/mutator_mocks.go sigs.k8s.io/aws-load-balancer-controller/pkg/webhook Mutator
~/go/bin/mockgen -package=webhook -destination=./pkg/webhook/validator_mocks.go sigs.k8s.io/aws-load-balancer-controller/pkg/webhook Validator
~/go/bin/mockgen -package=k8s -destination=./pkg/k8s/finalizer_mocks.go sigs.k8s.io/aws-load-balancer-controller/pkg/k8s FinalizerManager
~/go/bin/mockgen -package=k8s -destination=./pkg/k8s/pod_info_repo_mocks.go sigs.k8s.io/aws-load-balancer-controller/pkg/k8s PodInfoRepo
~/go/bin/mockgen -package=networking -destination=./pkg/networking/security_group_manager_mocks.go sigs.k8s.io/aws-load-balancer-controller/pkg/networking SecurityGroupManager
~/go/bin/mockgen -package=networking -destination=./pkg/networking/subnet_resolver_mocks.go sigs.k8s.io/aws-load-balancer-controller/pkg/networking SubnetsResolver
~/go/bin/mockgen -package=networking -destination=./pkg/networking/az_info_provider_mocks.go sigs.k8s.io/aws-load-balancer-controller/pkg/networking AZInfoProvider
~/go/bin/mockgen -package=networking -destination=./pkg/networking/vpc_resolver_mocks.go sigs.k8s.io/aws-load-balancer-controller/pkg/networking VPCResolver
~/go/bin/mockgen -package=networking -destination=./pkg/networking/vpc_info_provider_mocks.go sigs.k8s.io/aws-load-balancer-controller/pkg/networking VPCInfoProvider
~/go/bin/mockgen -package=ingress -destination=./pkg/ingress/cert_discovery_mocks.go sigs.k8s.io/aws-load-balancer-controller/pkg/ingress CertDiscovery
~/go/bin/mockgen -package=elbv2 -destination=./pkg/deploy/elbv2/tagging_manager_mocks.go sigs.k8s.io/aws-load-balancer-controller/pkg/deploy/elbv2 TaggingManager
