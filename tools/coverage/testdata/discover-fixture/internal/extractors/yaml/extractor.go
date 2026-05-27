// yaml extractor fixture: mentions multiple CI / container / IaC paths.
package yaml

// Recognised paths: .github/workflows/*.yml, .gitlab-ci.yml, .circleci/config.yml,
// .travis.yml, azure-pipelines.yml, Jenkinsfile, bitbucket-pipelines.yml,
// docker-compose.yml, kubernetes manifest, helm chart, cloudformation template.
const Patterns = "yaml docker-compose kubernetes helm cloudformation"
