# TargetGroupBinding
TargetGroupBinding is a [custom resource (CR)](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/) that can expose your pods using an existing [ALB TargetGroup](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-target-groups.html) or [NLB TargetGroup](https://docs.aws.amazon.com/elasticloadbalancing/latest/network/load-balancer-target-groups.html).

This will allow you to provision the load balancer infrastructure completely outside of Kubernetes but still manage the targets with Kubernetes Service.

!!!tip "usage to support Ingress and Service"
    The AWS LoadBalancer controller internally used TargetGroupBinding to support the functionality for Ingress and Service resource as well.
    It automatically creates TargetGroupBinding in the same namespace of the Service used. 
    
    You can view all TargetGroupBindings in a namespace by `kubectl get targetgroupbindings -n <your-namespace> -o wide`


## TargetType
TargetGroupBinding CR supports TargetGroups of either `instance` or `ip` TargetType.

!!!tip ""
    If TargetType is not explicitly specified, a mutating webhook will automatically call AWS API to find the TargetType for your TargetGroup and set it to correct value.


## Sample YAML
```yaml
apiVersion: elbv2.k8s.aws/v1beta1
kind: TargetGroupBinding
metadata:
  name: my-tgb
spec:
  serviceRef:
    name: awesome-service # route traffic to the awesome-service
    port: 80
  targetGroupARN: <arn-to-targetGroup>
```


## NodeSelector

### Default Node Selector

For `TargetType: instance`, all nodes of a cluster that match the following
selector are added to the target group by default:

```yaml
matchExpressions:
  - key: node-role.kubernetes.io/master
    operator: DoesNotExist
  - key: node.kubernetes.io/exclude-from-external-load-balancers
    operator: DoesNotExist
  - key: alpha.service-controller.kubernetes.io/exclude-balancer
    operator: DoesNotExist
  - key: eks.amazonaws.com/compute-type
    operator: NotIn
    values: ["fargate"]
```

### Custom Node Selector

TargetGroupBinding CR supports `NodeSelector` which is a
[LabelSelector][LabelSelector]. This will select nodes to attach to the
`instance` TargetType target group and **is merged with the default node
selector above**.

```yaml
apiVersion: elbv2.k8s.aws/v1beta1
kind: TargetGroupBinding
metadata:
  name: my-tgb
spec:
  nodeSelector:
    matchLabels:
      foo: bar
  ...
```

## Reference
See the [reference](./spec.md) for TargetGroupBinding CR

[LabelSelector]: https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#labelselector-v1-meta
