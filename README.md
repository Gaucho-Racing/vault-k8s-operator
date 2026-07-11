# Vault K8s Operator

[![Release](https://img.shields.io/github/v/release/Gaucho-Racing/vault-k8s-operator?style=flat-square)](https://github.com/Gaucho-Racing/vault-k8s-operator/releases)

Kubernetes operator that syncs Gaucho Racing [Vault](https://github.com/Gaucho-Racing/Vault) app-secrets into cluster `Secret` resources, and rolls consuming Deployments when values change.

The operator authenticates to Vault with the referenced `ServiceAccount`'s projected token (audience `gaucho-racing-vault`). Vault validates the token against the cluster's OIDC discovery endpoint and enforces its own selector rules to decide which app-secret fields the caller may read.

Links: [Vault](https://vault.gauchoracing.com) · [Vault repository](https://github.com/Gaucho-Racing/Vault)

## Installation

Install the CRD, RBAC, and manager Deployment with Kustomize:

```bash
kubectl apply -k github.com/Gaucho-Racing/vault-k8s-operator/config/default?ref=v1.1.1
```

Or wire it up via ArgoCD:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: vault-k8s-operator
  namespace: argocd
spec:
  source:
    repoURL: https://github.com/Gaucho-Racing/vault-k8s-operator.git
    targetRevision: v1.1.1
    path: config/default
  destination:
    server: https://kubernetes.default.svc
    namespace: vault-k8s-operator-system
  syncPolicy:
    automated: { prune: true, selfHeal: true }
    syncOptions: [CreateNamespace=true, ServerSideApply=true]
```

## Usage

Register your Kubernetes cluster in Vault first (Vault UI → Settings → Kubernetes Clusters). Then apply a `VaultSecretSync` referencing the Vault selectors you want materialized:

```yaml
apiVersion: vault.gauchoracing.com/v1alpha1
kind: VaultSecretSync
metadata:
  name: sentinel-secrets
  namespace: sentinel
spec:
  serviceAccountName: default
  target:
    name: sentinel-secrets
    type: Opaque
  refreshInterval: 5m
  rolloutTargets:
    - kind: Deployment
      name: core
    - kind: Deployment
      name: oauth
  secrets:
    DATABASE_HOST: gr-postgres.database_host
    DATABASE_PASSWORD: gr-postgres.database_password
    INTERNAL_BOOTSTRAP_SECRET: sentinel-prod.internal_bootstrap_secret
```

On every reconcile the operator:

1. Requests a projected token for `spec.serviceAccountName` with audience `gaucho-racing-vault`.
2. Sends the token + `spec.secrets` map to Vault's Kubernetes export endpoint.
3. Writes the returned values into `spec.target.name`, using the map's key as the k8s `Secret` key.
4. If the Secret content changed, restarts every Deployment/StatefulSet/DaemonSet listed in `spec.rolloutTargets`.

### Spec fields

| Field | Required | Default | Notes |
| --- | --- | --- | --- |
| `serviceAccountName` | No | `default` | SA in the same namespace as the CR. Its projected token is the caller identity Vault checks. |
| `target.name` | No | `metadata.name` | k8s `Secret` to write. |
| `target.type` | No | `Opaque` | Any valid Secret type. |
| `secrets` | Yes | | Map of `<k8s-secret-key>: <vault-app>.<vault-field>`. |
| `refreshInterval` | No | `5m` | How often to re-fetch from Vault. |
| `rolloutTargets` | No | `[]` | Workloads to restart when the Secret content changes. Supports `Deployment`, `StatefulSet`, `DaemonSet`. |
| `vaultURL` | No | `--vault-url` flag | Override the Vault URL per-CR. |

The operator writes a `Synced` condition on `.status.conditions` on every reconcile — successful syncs also emit an info log line with the target Secret and key count.

## Configuration

Manager flags (see `config/manager/manager.yaml`):

| Flag | Default | Purpose |
| --- | --- | --- |
| `--vault-url` | `https://vault.gauchoracing.com` | Base URL for the Vault API. |
| `--audience` | `gaucho-racing-vault` | OIDC audience used when minting the SA token. Must match Vault's expected audience. |
| `--leader-elect` | `true` | Standard controller-runtime leader election. |

Override per-Deployment via env vars `VAULT_URL` and `VAULT_AUDIENCE`.

## Vault Setup

The Vault side needs two things before the operator can sync:

1. **Register the cluster** in Vault (Vault UI → Settings → Kubernetes Clusters) with the cluster's OIDC issuer and the operator's audience.
2. **Grant read access** via a Kubernetes secret rule (Vault UI → Settings → Kubernetes Secret Rules) whose cluster/namespace/service-account patterns match the calling `VaultSecretSync` and whose selector list covers the Vault fields it references.

## Development

```bash
make manifests   # regenerate CRDs from api/*_types.go
make generate    # regenerate deepcopy
make test        # run unit tests
make docker-build IMG=ghcr.io/gaucho-racing/vault-k8s-operator:dev
```

## Related Projects

- [Vault](https://github.com/Gaucho-Racing/Vault): the secrets manager this operator reads from.
- [vault-pull-secrets](https://github.com/Gaucho-Racing/vault-pull-secrets): equivalent for GitHub Actions workflows.
