# Vendored Agent Sandbox core controller chart

This directory is derived from the official `helm/` chart in
`kubernetes-sigs/agent-sandbox` at commit
`3e77ccbac4db8a12b0157eafcad0d1ad5872f32a` (tag `v1.0.1`). It is vendored so
Terraform can install an immutable, reviewed artifact without fetching mutable
Git content while planning.

The Superverse MVP uses only the core `Sandbox` API. Relative to upstream this
copy deliberately:

- retains only `agents.x-k8s.io_sandboxes.yaml` under `crds/`;
- removes the extension ClusterRole and ClusterRoleBinding templates;
- removes the default `controller.extensions` value, so the controller is not
  started with the extension flag; and
- records the downstream packaging revision in `Chart.yaml`.

The controller image is supplied by the environment Terraform stack as the
upstream v1.0.1 tag plus immutable digest. The chart contains no image tag of
its own.

Upstream file checksums before the core-only reduction:

| file | SHA-256 |
| --- | --- |
| `helm/Chart.yaml` | `63715ddb9b453c419f3560b7ac60589e1fcd0c92ecb980df290ab17d3228a588` |
| `helm/values.yaml` | `6003e3b64117f12be5520e39da52f597b62e93661a7766c85a48d5be3f61682a` |
| `helm/crds/agents.x-k8s.io_sandboxes.yaml` | `bc6d66863decb675ce6da2c835ee3bb06aaaf8d73ad03aae9fe971fde5e8acce` |

The included `LICENSE` is the upstream Apache License 2.0.
