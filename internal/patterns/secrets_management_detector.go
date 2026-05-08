package patterns

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// secretsManagementDetector detects secrets management integrations.
// Matches Python secrets_management_detector.py.
type secretsManagementDetector struct{}

var secretsImportTokens = []string{
	"hvac", "vault", "aws_secretsmanager", "secretsmanager",
	"azure.keyvault", "SecretClient",
	"google.cloud.secretmanager", "SecretManagerServiceClient",
}

var secretsSourceTokens = []string{
	"vault.NewClient(", "vault.read(", "client.secrets(",
	"secretsmanager", "get_secret_value",
	"SecretClient(", "getSecretValue(",
}

var (
	secretsPyHvacRE     = regexp.MustCompile(`\bhvac\s*\.\s*(?:Client|AsyncClient)\s*\(`)
	secretsPyBoto3SMRE  = regexp.MustCompile(`boto3\s*\.\s*(?:client|resource)\s*\(\s*["']secretsmanager["']`)
	secretsPyAzureKVRE  = regexp.MustCompile(`(?:SecretClient|azure\.keyvault\.secrets\.SecretClient)\s*\(`)
	secretsGoVaultRE    = regexp.MustCompile(`\bvault\s*\.\s*NewClient\s*\(`)
	secretsGoAWSRE      = regexp.MustCompile(`secretsmanager\.New(?:FromConfig)?\s*\(`)
	secretsNodeVaultRE  = regexp.MustCompile(`(?:require|import).*['"]node-vault['"]`)
	secretsNodeAWSSDKRE = regexp.MustCompile(`SecretsManager\s*\(`)
	secretsJavaVaultRE  = regexp.MustCompile(`(?:VaultTemplate|SpringVaultConfiguration|VaultEndpoint)`)
)

func (s *secretsManagementDetector) Category() string { return "secrets_management" }

func (s *secretsManagementDetector) AppliesTo(src string) bool {
	srcLower := strings.ToLower(src)
	for _, tok := range secretsImportTokens {
		if strings.Contains(srcLower, strings.ToLower(tok)) {
			return true
		}
	}
	for _, tok := range secretsSourceTokens {
		if strings.Contains(src, tok) {
			return true
		}
	}
	return false
}

func (s *secretsManagementDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, name, provider string, line int) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			name, "SCOPE.Pattern", "secrets_management", language, line,
			map[string]string{"kind": "secrets_management", "provider": provider}))
	}

	if m := secretsPyHvacRE.FindStringIndex(src); m != nil {
		emit("py:vault", "secrets_vault_hvac", "vault", lineOf(src, m[0]))
	}
	if m := secretsPyBoto3SMRE.FindStringIndex(src); m != nil {
		emit("py:aws_sm", "secrets_aws_secretsmanager", "aws_secrets_manager", lineOf(src, m[0]))
	}
	if m := secretsPyAzureKVRE.FindStringIndex(src); m != nil {
		emit("py:azure_kv", "secrets_azure_keyvault", "azure_key_vault", lineOf(src, m[0]))
	}
	if m := secretsGoVaultRE.FindStringIndex(src); m != nil {
		emit("go:vault", "secrets_vault_go", "vault", lineOf(src, m[0]))
	}
	if m := secretsGoAWSRE.FindStringIndex(src); m != nil {
		emit("go:aws_sm", "secrets_aws_sm_go", "aws_secrets_manager", lineOf(src, m[0]))
	}
	if m := secretsNodeVaultRE.FindStringIndex(src); m != nil {
		emit("node:vault", "secrets_vault_node", "vault", lineOf(src, m[0]))
	}
	if m := secretsNodeAWSSDKRE.FindStringIndex(src); m != nil {
		emit("node:aws_sm", "secrets_aws_sm_node", "aws_secrets_manager", lineOf(src, m[0]))
	}
	if m := secretsJavaVaultRE.FindStringIndex(src); m != nil {
		emit("java:vault", "secrets_vault_java", "vault", lineOf(src, m[0]))
	}

	return results
}

func init() {
	Register(&secretsManagementDetector{})
}
