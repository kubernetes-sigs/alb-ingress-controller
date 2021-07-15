# Redirect Traffic from HTTP to HTTPS

We'll use the [`alb.ingress.kubernetes.io/actions.${action-name}`](../ingress/annotations.md#actions) annotation to setup an ingress to redirect http traffic into https


## Example Ingress Manifest
```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  namespace: default
  name: ingress
  annotations:
    kubernetes.io/ingress.class: alb
    alb.ingress.kubernetes.io/certificate-arn: arn:aws:acm:us-west-2:xxxx:certificate/xxxxxx
    alb.ingress.kubernetes.io/listen-ports: '[{"HTTP": 80}, {"HTTPS":443}]'
    alb.ingress.kubernetes.io/actions.ssl-redirect: '{"Type": "redirect", "RedirectConfig": { "Protocol": "HTTPS", "Port": "443", "StatusCode": "HTTP_301"}}'
    alb.ingress.kubernetes.io/ssl-redirect: '443'
spec:
  rules:
    - http:
        paths:
         - path: /*
           backend:
             serviceName: ssl-redirect
             servicePort: use-annotation
         - path: /users/*
           backend:
             serviceName: user-service
             servicePort: 80
         - path: /*
           backend:
             serviceName: default-service
             servicePort: 80
```

!!!note
    - `alb.ingress.kubernetes.io/listen-ports` annotation must at least include [{"HTTP": 80}, {"HTTPS":443}] to listen on 80 and 443.
    - `alb.ingress.kubernetes.io/certificate-arn` annotation must be set to allow listen for HTTPS traffic
    - the `ssl-redirect` action must be be first rule(which will be evaluated first by ALB)

## How it works
By default, all rules specified in ingress spec will be applied to all listeners(one listener per port) on ALB.

If there is an redirection rule, the AWS Load Balancer controller will check it against every listener(port) to see whether it will introduce infinite redirection loop, and **will ignore that rule for specific listener.**

So for our above example, the rule by `ssl-redirect` will only been applied to http(80) listener.
