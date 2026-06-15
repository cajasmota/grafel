# Terraform convention

Required reading: `_graph-searchability.md`.

Applies to Terraform / OpenTofu repos. For AWS-CDK, see `infra-cdk.md`. For Kubernetes manifests, see `infra-k8s.md`.

## Public surface

1. **Root modules** — directories containing a `terraform` block and `backend` configuration. Each is a deploy target.
2. **Reusable modules** — directories meant to be `module {}`-imported by other repos. Their `variables.tf` is the public API.
3. **Outputs** — `output {}` blocks. These are what other repos / other root modules consume.
4. **Provider configurations** — `required_providers` and provider aliases.

## Module shape

```
modules/
  <module>/
    main.tf
    variables.tf
    outputs.tf
    versions.tf
envs/
  prod/
    main.tf
    backend.tf
    terraform.tfvars
  staging/
```

A community typically maps to a single module or to one root env. Treat each `envs/<env>/` as its own module page.

## Entry points (Pass 3)

- Each `envs/<env>/` (root modules).
- Backend configuration — where state lives.
- `versions.tf` — required provider/Terraform versions.

## Dynamic edges (Pass 4)

- **Resource → resource** edges via interpolation (`aws_iam_role_policy_attachment.policy_arn = aws_iam_policy.foo.arn`). The graph captures these statically; document the non-obvious ones in `flows.md`.
- **Cross-state references** — `terraform_remote_state` reads another state file. The producing state belongs to another root module / another repo. Encode the bridge:
  ```markdown
  ## How `envs/prod` reads `network/prod` outputs
  ```
- **Data sources** — `data "aws_*"` lookups by tag or name couple this state to whatever creates the resource. If the producer is in another repo, that's a cross-repo edge.
- **Provisioners** (`local-exec`, `remote-exec`) — out-of-band side effects. Always document.

## Deployment signals (Pass 5)

- The `backend` block in every root module.
- CI files that run `terraform plan` / `apply` — branch protection, manual approval gates.
- `.tfvars` files — per-environment values. Note which are secret (and therefore committed via `*.auto.tfvars`-encrypted or via env vars).

## Manifest files

`.terraform.lock.hcl` (provider lockfile), `versions.tf` (constraints). `terraform init` populates the lock; document the policy on commits to it.

## Cross-cutting pitfalls

- **State locking** — backend must support locking (S3 with DynamoDB, Terraform Cloud). Note where it does not.
- **Workspaces vs directory layout** — pick one and document it. Mixing the two is a footgun.
- **`count` and `for_each`** changes that re-key resources cause destroy/create cycles. Document any resource that uses these as state-fragile.

## Cross-repo signals

The `terraform_remote_state` data source is the canonical cross-repo bridge. When another repo's `outputs.tf` declares a value this repo consumes, that's a high-confidence edge. `grafel_cross_links(action=list)` should usually accept these without manual intervention.
