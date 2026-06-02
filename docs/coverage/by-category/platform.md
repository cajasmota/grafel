<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->
# platform

**Total**: 40 records · **java**: 2 · **javascript**: 1 · **JS/TS**: 1 · **multi**: 34 · **python**: 1 · **ruby**: 1

Back to [summary](../summary.md). Bucket: **Other**.



## IaC / Provisioning

| Language | Name | Dependency attribution | Resource extraction | Status | Notes |
|---|---|---|---|---|---|
| [multi](../by-language/multi.md) | [AWS CDK](../detail/infra.iac.cdk.md) | 🟢 | 🟢 | 🟢 | |
| [multi](../by-language/multi.md) | [AWS CloudFormation](../detail/infra.iac.cloudformation.md) | ✅ | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [Ansible (playbooks)](../detail/infra.iac.ansible.md) | 🟢 | 🟢 | 🟢 | |
| [multi](../by-language/multi.md) | [Azure Bicep](../detail/infra.iac.bicep.md) | ✅ | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [OpenTofu (HCL)](../detail/infra.iac.opentofu.md) | ✅ | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [Pulumi](../detail/infra.iac.pulumi.md) | 🟢 | 🟢 | 🟢 | |
| [multi](../by-language/multi.md) | [Serverless Framework](../detail/infra.iac.serverless-framework.md) | ✅ | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [Terraform (HCL)](../detail/infra.iac.terraform.md) | ✅ | ✅ | ✅ | |

## Containers & Orchestration

| Language | Name | Dependency attribution | Env resolution | Resource extraction | Status | Notes |
|---|---|---|---|---|---|---|
| [multi](../by-language/multi.md) | [Dockerfile](../detail/infra.container.dockerfile.md) | ✅ | — | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [Helm charts](../detail/infra.container.helm.md) | ✅ | ✅ | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [Kubernetes manifests](../detail/infra.container.kubernetes.md) | ✅ | — | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [Kustomize](../detail/infra.container.kustomize.md) | ✅ | — | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [docker-compose.yml](../detail/infra.container.docker-compose.md) | ✅ | — | ✅ | ✅ | |

## Config Files

| Language | Name | Env resolution | File parsing | Status | Notes |
|---|---|---|---|---|---|
| [java](../by-language/java.md) | [.properties (application.properties)](../detail/config.properties.md) | — | ✅ | ✅ | |
| [JS/TS](../by-language/jsts.md) | [tsconfig.json](../detail/config.tsconfig.md) | — | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [.env (names-only — values stripped at extraction boundary)](../detail/config.dotenv.md) | ✅ | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [.ini / setup.cfg / flake8 / mypy / pytest.ini](../detail/config.ini.md) | — | 🟢 | 🟢 | |
| [multi](../by-language/multi.md) | [.toml](../detail/config.toml.md) | — | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [.yaml / .yml](../detail/config.yaml.md) | — | ✅ | ✅ | |
| [multi](../by-language/multi.md) | [docker-compose.yml](../detail/config.docker-compose.md) | — | ✅ | ✅ | |

## Workflow / DAG & State Machines

| Language | Name | Dependency attribution | Resource extraction | Status | Notes |
|---|---|---|---|---|---|
| [java](../by-language/java.md) | [Spring StateMachine (FSM topology)](../detail/infra.state-machine.spring-statemachine.md) | 🟢 | 🟢 | 🟢 | |
| [javascript](../by-language/javascript.md) | [XState (FSM topology)](../detail/infra.state-machine.xstate.md) | 🟢 | 🟢 | 🟢 | |
| [multi](../by-language/multi.md) | [Apache Airflow (DAG topology)](../detail/infra.orchestration.airflow.md) | 🟢 | 🟢 | 🟢 | |
| [multi](../by-language/multi.md) | [Argo Workflows (DAG topology)](../detail/infra.orchestration.argo.md) | 🟢 | 🟢 | 🟢 | |
| [multi](../by-language/multi.md) | [Celery canvas (chain/group/chord topology)](../detail/infra.orchestration.celery-canvas.md) | 🟢 | 🟢 | 🟢 | |
| [python](../by-language/python.md) | [Python transitions (FSM topology)](../detail/infra.state-machine.python-transitions.md) | 🟢 | 🟢 | 🟢 | |
| [ruby](../by-language/ruby.md) | [Ruby AASM (FSM topology)](../detail/infra.state-machine.aasm.md) | 🟢 | 🟢 | 🟢 | |

## App Topology & Integration

| Language | Name | Dependency attribution | External service dependency | Resource extraction | Shared data coupling | Translation key usage | Status | Notes |
|---|---|---|---|---|---|---|---|---|
| [multi](../by-language/multi.md) | [API-gateway route topology (application frameworks)](../detail/infra.gateway.api-routing.md) | ✅ | — | 🟢 | — | — | 🟢 | |
| [multi](../by-language/multi.md) | [AWS CDK](../detail/infra.resource.aws-cdk.md) | — | — | 🟢 | — | — | 🟢 | |
| [multi](../by-language/multi.md) | [AWS CloudFormation](../detail/infra.resource.cloudformation.md) | ✅ | — | ✅ | — | — | ✅ | |
| [multi](../by-language/multi.md) | [Frontend route → component graph (SPA client routing)](../detail/frontend.routing.spa-route-component.md) | 🟢 | — | 🟢 | — | — | 🟢 | |
| [multi](../by-language/multi.md) | [Helm charts](../detail/infra.resource.helm.md) | — | — | ✅ | — | — | ✅ | |
| [multi](../by-language/multi.md) | [Kubernetes manifests](../detail/infra.resource.kubernetes.md) | — | — | ✅ | — | — | ✅ | |
| [multi](../by-language/multi.md) | [Pulumi](../detail/infra.resource.pulumi.md) | — | — | 🟢 | — | — | 🟢 | |
| [multi](../by-language/multi.md) | [Reverse-proxy / gateway request topology](../detail/infra.deployment.request-topology.md) | ✅ | — | 🟢 | — | — | 🟢 | |
| [multi](../by-language/multi.md) | [Shared-database cross-service coupling (SHARES_DATA)](../detail/analysis.architecture.shared-db-coupling.md) | — | — | — | ✅ | — | ✅ | |
| [multi](../by-language/multi.md) | [Structural coupling metrics (Ca/Ce/instability)](../detail/analysis.architecture.structural-coupling.md) | ✅ | — | — | — | — | ✅ | |
| [multi](../by-language/multi.md) | [Terraform / OpenTofu / Vault / Nomad / Packer / Waypoint](../detail/infra.resource.terraform.md) | ✅ | — | ✅ | — | — | ✅ | |
| [multi](../by-language/multi.md) | [Third-party SDK service dependencies (DEPENDS_ON_SERVICE)](../detail/analysis.integration.third-party-sdk.md) | — | 🟢 | — | — | — | 🟢 | |
| [multi](../by-language/multi.md) | [i18n translation-key usage (USES_TRANSLATION)](../detail/analysis.localization.i18n-keys.md) | — | — | — | — | 🟢 | 🟢 | |
