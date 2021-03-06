# Controller configuration options
This document covers configuration of the AWS Load Balancer controller

## AWS API Access
To perform operations, the controller must have required IAM role capabilities for accessing and
provisioning ALB resources. There are many ways to achieve this, such as loading `AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY` as environment variables or using [kube2iam](https://github.com/jtblin/kube2iam).

Refer to the [installation guide](installation.md) for installing the controller in your kubernetes cluster and for the minimum required IAM permissions.

## Setting Ingress Resource Scope
You can limit the ingresses ALB ingress controller controls by combining following two approaches:

### Limiting ingress class
Setting the `--ingress-class` argument constrains the controller's scope to ingresses with matching `kubernetes.io/ingress.class` annotation.
This is especially helpful when running multiple ingress controllers in the same cluster. See [Using Multiple Ingress Controllers](https://github.com/nginxinc/kubernetes-ingress/tree/master/examples/multiple-ingress-controllers#using-multiple-ingress-controllers) for more details.

An example of the container spec portion of the controller, only listening for resources with the class "alb", would be as follows.

```yaml
spec:
  containers:
  - args:
    - --ingress-class=alb
```

Now, only ingress resources with the appropriate annotation are picked up, as seen below.

```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: echoserver
  namespace: echoserver
  annotations:
    kubernetes.io/ingress.class: "alb"
spec:
    ...
```

If the ingress class is not specified, the controller will reconcile Ingress objects without the ingress class specified or ingress class `alb`.

### Limiting Namespaces
Setting the `--watch-namespace` argument constrains the controller's scope to a single namespace. Ingress events outside of the namespace specified are not be seen by the controller.

An example of the container spec, for a controller watching only the `default` namespace, is as follows.

```yaml
spec:
  containers:
  - args:
    - --watch-namespace=default
```

!!!note ""
Currently, you can set only 1 namespace to watch in this flag. See [this Kubernetes issue](https://github.com/kubernetes/contrib/issues/847) for more details.

## Controller command line flags

!!!warning ""
    The --cluster-name flag is mandatory and the value must match the name of the kubernetes cluster. If you specify an incorrect name, the subnet auto-discovery will not work.

|Flag                                   | Type                            | Default         | Description |
|---------------------------------------|---------------------------------|-----------------|-------------|
|aws-api-throttle                       | AWS Throttle Config             | [default value](#default-throttle-config ) | throttle settings for AWS APIs, format: serviceID1:operationRegex1=rate:burst,serviceID2:operationRegex2=rate:burst |
|aws-max-retries                        | int                             | 10              | Maximum retries for AWS APIs |
|aws-region                             | string                          | [instance metadata](#instance-metadata)    | AWS Region for the kubernetes cluster |
|aws-vpc-cache-duration                 | string                          | 5               | Length of time in minutes to cache VPC info |
|aws-vpc-id                             | string                          | [instance metadata](#instance-metadata)    | ID of the AWS VPC where Load Balancer resources will be created |
|cluster-name                           | string                          |                 | Kubernetes cluster name|
|default-tags                           | stringMap                       |                 | Default AWS Tags that will be applied to all AWS resources managed by this controller |
|default-ssl-policy                     | string                          | ELBSecurityPolicy-2016-08 | Default SSL Policy that will be applied to all ingresses or services that do not have the SSL Policy annotation. |
|enable-leader-election                 | boolean                         | true            | Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager. |
|enable-pod-readiness-gate-inject       | boolean                         | true            | If enabled, targetHealth readiness gate will get injected to the pod spec for the matching endpoint pods. |
|enable-shield                          | boolean                         | true            | Enable Shield addon for ALB |
|enable-waf                             | boolean                         | true            | Enable WAF addon for ALB |
|enable-wafv2                           | boolean                         | true            | Enable WAF V2 addon for ALB |
|ingress-class                          | string                          | alb             | Name of the ingress class this controller satisfies |
|ingress-max-concurrent-reconciles      | int                             | 3               | Maximum number of concurrently running reconcile loops for ingress |
|kubeconfig                             | string                          | in-cluster config | Path to the kubeconfig file containing authorization and API server information |
|leader-election-id                     | string                          | aws-load-balancer-controller-leader | Name of the leader election ID to use for this controller |
|leader-election-namespace              | string                          |                 | Name of the leader election ID to use for this controller |
|log-level                              | string                          | info            | Set the controller log level - info, debug |
|metrics-bind-addr                      | string                          | :8080           | The address the metric endpoint binds to |
|service-max-concurrent-reconciles      | int                             | 3               | Maximum number of concurrently running reconcile loops for service |
|sync-period                            | duration                        | 1h0m0s          | Period at which the controller forces the repopulation of its local object stores|
|targetgroupbinding-max-concurrent-reconciles | int                       | 3               | Maximum number of concurrently running reconcile loops for targetGroupBinding |
|watch-namespace                        | string                          |                 | Namespace the controller watches for updates to Kubernetes objects, If empty, all namespaces are watched. |
|webhook-bind-port                      | int                             | 9443            | The TCP port the Webhook server binds to |


### Default throttle config
```
WAF Regional:^AssociateWebACL|DisassociateWebACL=0.5:1,WAF Regional:^GetWebACLForResource|ListResourcesForWebACL=1:1,WAFV2:^AssociateWebACL|DisassociateWebACL=0.5:1,WAFV2:^GetWebACLForResource|ListResourcesForWebACL=1:1
```

AWS Web Application Firewall (WAF) 

### Instance metadata
If running on EC2, the default values are obtained from the instance metadata service.
