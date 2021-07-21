# AWS Load Balancer Controller

AWS Load Balancer controller Helm chart for Kubernetes

## TL;DR:
```sh
helm repo add eks https://aws.github.io/eks-charts
# If using IAM Roles for service account install as follows -  NOTE: you need to specify both of the chart values `serviceAccount.create=false` and `serviceAccount.name=aws-load-balancer-controller`
helm install aws-load-balancer-controller eks/aws-load-balancer-controller --set clusterName=my-cluster -n kube-system --set serviceAccount.create=false --set serviceAccount.name=aws-load-balancer-controller
# If not using IM Roles for service account
helm install aws-load-balancer-controller eks/aws-load-balancer-controller --set clusterName=my-cluster -n kube-system
```

## Introduction
AWS Load Balancer controller manages the following AWS resources
- Application Load Balancers to satisfy Kubernetes ingress objects
- Network Load Balancers to satisfy Kubernetes service objects of type LoadBalancer with appropriate annotations

## Security updates
**Note**: Deployed chart does not receive security updates automatically. You need to manually upgrade to a newer chart.

## Prerequisites
- Kubernetes >= 1.16 for ALB
- Kubernetes >= 1.16 for NLB IP/Instance using Service type NodePort
- Kubernetes >= v1.20 or EKS >= 1.16 or the following patch releases for Service type `LoadBalancer`
   - 1.18.18+ for 1.18
   - 1.19.10+ for 1.19
- IAM permissions

The controller runs on the worker nodes, so it needs access to the AWS ALB/NLB resources via IAM permissions. The
IAM permissions can either be setup via IAM roles for ServiceAccount or can be attached directly to the worker node IAM roles.

#### Setup IAM for ServiceAccount
1. Create IAM OIDC provider
    ```
    eksctl utils associate-iam-oidc-provider \
        --region <aws-region> \
        --cluster <your-cluster-name> \
        --approve
    ```
1. Download IAM policy for the AWS Load Balancer Controller
    ```
    curl -o iam-policy.json https://raw.githubusercontent.com/kubernetes-sigs/aws-load-balancer-controller/main/docs/install/iam_policy.json
    ```
1. Create an IAM policy called AWSLoadBalancerControllerIAMPolicy
    ```
    aws iam create-policy \
        --policy-name AWSLoadBalancerControllerIAMPolicy \
        --policy-document file://iam-policy.json
    ```
    Take note of the policy ARN that is returned

1. Create a IAM role and ServiceAccount for the Load Balancer controller, use the ARN from the step above
    ```
    eksctl create iamserviceaccount \
    --cluster=<cluster-name> \
    --namespace=kube-system \
    --name=aws-load-balancer-controller \
    --attach-policy-arn=arn:aws:iam::<AWS_ACCOUNT_ID>:policy/AWSLoadBalancerControllerIAMPolicy \
    --approve
    ```
#### Setup IAM manually
If not setting up IAM for ServiceAccount, apply the IAM policies from the following URL at minimum.
```
https://raw.githubusercontent.com/kubernetes-sigs/aws-alb-ingress-controller/main/docs/install/iam_policy.json
```

#### Upgrading from ALB ingress controller
If migrating from ALB ingress controller, grant [additional IAM permissions](https://raw.githubusercontent.com/kubernetes-sigs/aws-load-balancer-controller/main/docs/install/iam_policy_v1_to_v2_additional.json).

#### Upgrading from  AWS Load Balancer controller v2.1.3 and earlier
- Additional IAM permissions required, ensure you have granted the [required IAM permissions](https://raw.githubusercontent.com/kubernetes-sigs/aws-load-balancer-controller/main/docs/install/iam_policy.json).
- CRDs need to be updated as follows
```shell script
kubectl apply -k "github.com/aws/eks-charts/stable/aws-load-balancer-controller//crds?ref=master"
```
- you can run helm upgrade without uninstalling the old chart completely

## Installing the Chart
**Note**: You need to uninstall aws-alb-ingress-controller. Please refer to the [upgrade](#Upgrade) section below before you proceed.

Add the EKS repository to Helm:
```shell script
helm repo add eks https://aws.github.io/eks-charts
```

Install the TargetGroupBinding CRDs:

```shell script
kubectl apply -k "github.com/aws/eks-charts/stable/aws-load-balancer-controller//crds?ref=master"
```

Install the AWS Load Balancer controller, if using iamserviceaccount
```shell script
# NOTE: The clusterName value must be set either via the values.yaml or the Helm command line. The <k8s-cluster-name> in the command
# below should be replaced with name of your k8s cluster before running it.
helm upgrade -i aws-load-balancer-controller eks/aws-load-balancer-controller -n kube-system --set clusterName=<k8s-cluster-name> --set serviceAccount.create=false --set serviceAccount.name=aws-load-balancer-controller
```

Install the AWS Load Balancer controller, if not using iamserviceaccount
```shell script
helm upgrade -i aws-load-balancer-controller eks/aws-load-balancer-controller -n kube-system --set clusterName=<k8s-cluster-name>
```

## Upgrade
The new controller is backwards compatible with the existing ingress objects. However, it will not coexist with the older aws-alb-ingress-controller.
The old controller must be uninstalled completely before installing the new version.
### Kubectl installation
If you had installed the previous version via kubectl, uninstall as follows
```shell script
$ kubectl delete deployment -n kube-system alb-ingress-controller
# Find the version of the current controller
$ kubectl describe deployment  -n kube-system  alb-ingress-controller |grep Image
      Image:      docker.io/amazon/aws-alb-ingress-controller:v1.1.8
# In this case, the version is v1.1.8, the rbac roles can be removed as follows
$ kubectl delete -f https://raw.githubusercontent.com/kubernetes-sigs/aws-alb-ingress-controller/v1.1.8/docs/examples/rbac-role.yaml
```
### Helm installation
If you had installed the incubator/aws-alb-ingress-controller Helm chart, uninstall as follows
```shell script
# NOTE: If installed under a different chart name and namespace, please specify as appropriate
$ helm delete aws-alb-ingress-controller -n kube-system
```

If you had installed the 0.1.x version of eks-charts/aws-load-balancer-controller chart earlier, the upgrade to chart version 1.0.0 will
not work due to incompatibility of the webhook api version, uninstall as follows
```shell script
$ helm delete aws-load-balancer-controller -n kube-system
```

## Uninstalling the Chart
```sh
helm delete aws-load-balancer-controller -n kube-system
```

## HA configuration
Chart release v1.2.0 and later enables high availability configuration by default.
- The default number of replicas is 2. You can pass`--set replicaCount=1` flag during chart installation to disable this. Due to leader election, only one controller will actively reconcile resources.
- The default priority class for the controller pods is `system-cluster-critical`
- Soft pod anti-affinity is enabled for controller pods with `topologyKey: kubernetes.io/hostname` if custom affinity is not configured
- Pod disruption budget (PDB) has not been set by default. If you plan on running at least 2 controller pods, you can pass `--set podDisruptionBudget.maxUnavailable=1` flag during chart installation

## Configuration

The following tables lists the configurable parameters of the chart and their default values.
The default values set by the application itself can be confirmed [here](https://kubernetes-sigs.github.io/aws-load-balancer-controller/guide/controller/configurations/).

| Parameter                                   | Description                                                                                              | Default                                                                            |
| ------------------------------------------- | -------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `image.repository`                          | image repository                                                                                         | `602401143452.dkr.ecr.us-west-2.amazonaws.com/amazon/aws-load-balancer-controller` |
| `image.tag`                                 | image tag                                                                                                | `<VERSION>`                                                                        |
| `image.pullPolicy`                          | image pull policy                                                                                        | `IfNotPresent`                                                                     |
| `clusterName`                               | Kubernetes cluster name                                                                                  | None                                                                               |
| `securityContext`                           | Set to security context for pod                                                                          | `{}`                                                                               |
| `resources`                                 | Controller pod resource requests & limits                                                                | `{}`                                                                               |
| `priorityClassName`                         | Controller pod priority class                                                                            | system-cluster-critical                                                            |
| `nodeSelector`                              | Node labels for controller pod assignment                                                                | `{}`                                                                               |
| `tolerations`                               | Controller pod toleration for taints                                                                     | `{}`                                                                               |
| `affinity`                                  | Affinity for pod assignment                                                                              | `{}`                                                                               |
| `podAnnotations`                            | Annotations to add to each pod                                                                           | `{}`                                                                               |
| `podLabels`                                 | Labels to add to each pod                                                                                | `{}`                                                                               |
| `rbac.create`                               | if `true`, create and use RBAC resources                                                                 | `true`                                                                             |
| `serviceAccount.annotations`                | optional annotations to add to service account                                                           | None                                                                               |
| `serviceAccount.automountServiceAccountToken`                | Automount API credentials for a Service Account                                         | `true`                                                                             |
| `serviceAccount.create`                     | If `true`, create a new service account                                                                  | `true`                                                                             |
| `serviceAccount.name`                       | Service account to be used                                                                               | None                                                                               |
| `terminationGracePeriodSeconds`             | Time period for controller pod to do a graceful shutdown                                                 | 10                                                                                 |
| `ingressClass`                              | The ingress class to satisfy                                                                             | alb                                                                                |
| `region`                                    | The AWS region for the kubernetes cluster                                                                | None                                                                               |
| `vpcId`                                     | The VPC ID for the Kubernetes cluster                                                                    | None                                                                               |
| `awsMaxRetries`                             | Maximum retries for AWS APIs                                                                             | None                                                                               |
| `enablePodReadinessGateInject`              | If enabled, targetHealth readiness gate will get injected to the pod spec for the matching endpoint pods | None                                                                               |
| `enableShield`                              | Enable Shield addon for ALB                                                                              | None                                                                               |
| `enableWaf`                                 | Enable WAF addon for ALB                                                                                 | None                                                                               |
| `enableWafv2`                               | Enable WAF V2 addon for ALB                                                                              | None                                                                               |
| `ingressMaxConcurrentReconciles`            | Maximum number of concurrently running reconcile loops for ingress                                       | None                                                                               |
| `logLevel`                                  | Set the controller log level - info, debug                                                               | None                                                                               |
| `metricsBindAddr`                           | The address the metric endpoint binds to                                                                 | ""                                                                                 |
| `webhookBindPort`                           | The TCP port the Webhook server binds to                                                                 | None                                                                               |
| `serviceMaxConcurrentReconciles`            | Maximum number of concurrently running reconcile loops for service                                       | None                                                                               |
| `targetgroupbindingMaxConcurrentReconciles` | Maximum number of concurrently running reconcile loops for targetGroupBinding                            | None                                                                               |
| `targetgroupbindingMaxExponentialBackoffDelay` | Maximum duration of exponential backoff for targetGroupBinding reconcile failures                     | None                                                                               |
| `syncPeriod`                                | Period at which the controller forces the repopulation of its local object stores                        | None                                                                               |
| `watchNamespace`                            | Namespace the controller watches for updates to Kubernetes objects, If empty, all namespaces are watched | None                                                                               |
| `disableIngressClassAnnotation`             | Disables the usage of kubernetes.io/ingress.class annotation                                             | None                                                                               |
| `disableIngressGroupNameAnnotation`         | Disables the usage of alb.ingress.kubernetes.io/group.name annotation                                    | None                                                                               |
| `defaultSSLPolicy`                          | Specifies the default SSL policy to use for HTTPS or TLS listeners                                       | None                                                                               |
| `externalManagedTags`                       | Specifies the list of tag keys on AWS resources that are managed externally                              | `[]`                                                                               |
| `livenessProbe`                             | Liveness probe settings for the controller                                                               | (see `values.yaml`)                                                                |
| `env`                                       | Environment variables to set for aws-load-balancer-controller pod                                        | None                                                                               |
| `hostNetwork`                               | If `true`, use hostNetwork                                                                               | `false`                                                                            |
| `extraVolumeMounts`                         | Extra volume mounts for the pod                                                                          | `[]`                                                                               |
| `extraVolumes`                              | Extra volumes for the pod                                                                                | `[]`                                                                               |
| `defaultTags`                               | Default tags to apply to all AWS resources managed by this controller                                    | `{}`                                                                               |
| `replicaCount`                              | Number of controller pods to run, only one will be active due to leader election                         | `2`                                                                                |
| `podDisruptionBudget`                       | Limit the disruption for controller pods. Require at least 2 controller replicas and 3 worker nodes      | `{}`                                                                               |
| `udpateStrategy`                            | Defines the update strategy for the deployment                                                           | `{}`                                                                               |
