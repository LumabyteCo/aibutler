# Kubernetes Deployment

## Prerequisites

- Kubernetes 1.24+
- Helm 3.x

## Install with Helm

```bash
helm install aibutler ./deploy/helm/aibutler
```

## Custom Values

```bash
helm install aibutler ./deploy/helm/aibutler \
  --set image.tag=0.1.0 \
  --set persistence.size=5Gi \
  --set ingress.enabled=true \
  --set ingress.hosts[0].host=aibutler.example.com
```

## Ingress

Enable ingress in `values.yaml`:

```yaml
ingress:
  enabled: true
  className: nginx
  hosts:
    - host: aibutler.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: aibutler-tls
      hosts:
        - aibutler.example.com
```

## Autoscaling

```yaml
autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70
```

## Health Probes

The Helm chart configures liveness and readiness probes against `/healthz` automatically.

## Secrets

Store sensitive configuration in the Helm secret:

```yaml
secrets:
  OPENAI_API_KEY: sk-...
  TELEGRAM_TOKEN: ...
```
