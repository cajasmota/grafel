package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// platformReg builds an in-memory registry covering the platform
// subcategory lanes (#3628 child). It includes the iac vs resource-graph
// duplicate-looking pairs (AWS CDK + Helm) so tests can assert they no
// longer render adjacently, plus the analysis.* integration/i18n records
// whose columns were structurally sparse in the old wide union.
func platformReg() *Registry {
	rec := func(id, sub string, caps map[string]string) Record {
		c := map[string]Capability{}
		for k, s := range caps {
			c[k] = Capability{Status: s}
		}
		return Record{
			ID: id, Category: "platform", Subcategory: sub,
			Language: "multi", Label: labelFor(id), Capabilities: c,
		}
	}
	return &Registry{
		SchemaVersion: SchemaVersion,
		Records: []Record{
			// IaC / Provisioning
			rec("infra.iac.terraform", "iac_provisioning", map[string]string{"resource_extraction": "full", "dependency_attribution": "full"}),
			rec("infra.iac.pulumi", "iac_provisioning", map[string]string{"resource_extraction": "partial", "dependency_attribution": "partial"}),
			rec("infra.iac.cdk", "iac_provisioning", map[string]string{"resource_extraction": "partial", "dependency_attribution": "partial"}),
			// Containers & Orchestration
			rec("infra.container.helm", "container_orchestration", map[string]string{"resource_extraction": "full", "dependency_attribution": "full", "env_resolution": "full"}),
			// Config Files
			rec("config.dotenv", "config_files", map[string]string{"file_parsing": "full", "env_resolution": "full"}),
			// Workflow / DAG
			rec("infra.orchestration.airflow", "workflow_dag", map[string]string{"resource_extraction": "partial", "dependency_attribution": "partial"}),
			// App Topology & Integration (resource-graph dup pairs + analysis records)
			rec("infra.resource.aws-cdk", "app_topology", map[string]string{"resource_extraction": "partial"}),
			rec("infra.resource.helm", "app_topology", map[string]string{"resource_extraction": "full"}),
			rec("infra.resource.cloudformation", "app_topology", map[string]string{"resource_extraction": "full", "dependency_attribution": "full"}),
			rec("analysis.architecture.shared-db-coupling", "app_topology", map[string]string{"shared_data_coupling": "full"}),
			rec("analysis.integration.third-party-sdk", "app_topology", map[string]string{"external_service_dependency": "partial"}),
			rec("analysis.localization.i18n-keys", "app_topology", map[string]string{"translation_key_usage": "partial"}),
		},
	}
}

func labelFor(id string) string {
	switch id {
	case "infra.iac.terraform":
		return "Terraform (HCL)"
	case "infra.iac.pulumi", "infra.resource.pulumi":
		return "Pulumi"
	case "infra.iac.cdk", "infra.resource.aws-cdk":
		return "AWS CDK"
	case "infra.resource.cloudformation":
		return "AWS CloudFormation"
	case "analysis.architecture.shared-db-coupling":
		return "Shared-database cross-service coupling"
	case "infra.container.helm", "infra.resource.helm":
		return "Helm charts"
	case "config.dotenv":
		return ".env"
	case "infra.orchestration.airflow":
		return "Apache Airflow (DAG topology)"
	case "analysis.integration.third-party-sdk":
		return "Third-party SDK service dependencies"
	case "analysis.localization.i18n-keys":
		return "i18n translation-key usage"
	}
	return id
}

// TestPlatformByCategoryRendersSubcategoryLanes asserts the platform
// by-category page splits into the named subcategory lanes instead of one
// wide sparse union, that each lane's pivot only declares the capability
// columns its records use, and that the iac vs resource-graph
// duplicate-name pairs no longer sit adjacent.
func TestPlatformByCategoryRendersSubcategoryLanes(t *testing.T) {
	root := t.TempDir()
	if err := generate(platformReg(), root); err != nil {
		t.Fatalf("generate: %v", err)
	}
	page := readFile(t, filepath.Join(root, "docs/coverage/by-category/platform.md"))

	// Each lane renders as its own section heading.
	for _, h := range []string{
		"## IaC / Provisioning",
		"## Containers & Orchestration",
		"## Config Files",
		"## Workflow / DAG & State Machines",
		"## App Topology & Integration",
	} {
		if !strings.Contains(page, h) {
			t.Errorf("platform.md missing subcategory section %q\n%s", h, page)
		}
	}

	// IaC lane is discoverable with its flagship tools.
	iac := sectionBody(page, "## IaC / Provisioning")
	for _, tool := range []string{"Terraform", "Pulumi", "AWS CDK"} {
		if !strings.Contains(iac, tool) {
			t.Errorf("IaC lane missing %q\n%s", tool, iac)
		}
	}

	// No 7-wide union: the legacy union header listed all of these columns
	// in a single table. The IaC lane must NOT carry the config/coupling/
	// i18n columns — it declares only resource/dependency columns.
	iacHeader := firstTableHeader(iac)
	for _, col := range []string{"File parsing", "Shared data coupling", "Translation key usage", "External service dependency"} {
		if strings.Contains(iacHeader, col) {
			t.Errorf("IaC lane header should not contain %q (sparse-union leak): %s", col, iacHeader)
		}
	}
	if !strings.Contains(iacHeader, "Resource extraction") || !strings.Contains(iacHeader, "Dependency attribution") {
		t.Errorf("IaC lane header missing its own columns: %s", iacHeader)
	}

	// The AWS CDK duplicate-name pair must land in different lanes
	// (iac_provisioning vs app_topology) so they are not adjacent rows.
	idxIac := strings.Index(page, "infra.iac.cdk")
	idxRes := strings.Index(page, "infra.resource.aws-cdk")
	if idxIac < 0 || idxRes < 0 {
		t.Fatalf("expected both CDK records present")
	}
	if !strings.Contains(iac, "infra.iac.cdk") {
		t.Errorf("infra.iac.cdk should be in the IaC lane")
	}
	appTopo := sectionBody(page, "## App Topology & Integration")
	if !strings.Contains(appTopo, "infra.resource.aws-cdk") {
		t.Errorf("infra.resource.aws-cdk should be in the App Topology lane")
	}
	if strings.Contains(iac, "infra.resource.aws-cdk") {
		t.Errorf("resource-graph CDK record leaked into the IaC lane (apparent dup)")
	}

	// No structurally-always-empty column: every column header in every
	// lane table must be backed by at least one non-"—" cell.
	assertNoAlwaysEmptyColumn(t, page)
}

// TestSummarySurfacesIaC asserts the summary's Platform subcategories
// discoverability section names IaC with a link into the platform page
// and lists Terraform-class tools.
func TestSummarySurfacesIaC(t *testing.T) {
	root := t.TempDir()
	if err := generate(platformReg(), root); err != nil {
		t.Fatalf("generate: %v", err)
	}
	summary := readFile(t, filepath.Join(root, "docs/coverage/summary.md"))

	if !strings.Contains(summary, "Platform subcategories") {
		t.Fatalf("summary.md missing Platform subcategories section:\n%s", summary)
	}
	if !strings.Contains(summary, "[IaC / Provisioning](by-category/platform.md#iac--provisioning)") {
		t.Errorf("summary.md missing linked IaC / Provisioning entry:\n%s", summary)
	}
	// Tool names should be discoverable in the IaC row.
	iacRow := lineContaining(summary, "IaC / Provisioning")
	for _, tool := range []string{"Terraform", "AWS CDK"} {
		if !strings.Contains(iacRow, tool) {
			t.Errorf("summary IaC row missing tool %q: %q", tool, iacRow)
		}
	}
}

func TestHeadingAnchor(t *testing.T) {
	cases := []struct{ in, want string }{
		{"IaC / Provisioning", "iac--provisioning"},
		{"Containers & Orchestration", "containers--orchestration"},
		{"Config Files", "config-files"},
		{"Workflow / DAG & State Machines", "workflow--dag--state-machines"},
		{"App Topology & Integration", "app-topology--integration"},
	}
	for _, c := range cases {
		if got := headingAnchor(c.in); got != c.want {
			t.Errorf("headingAnchor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPlatformToolSamples(t *testing.T) {
	in := []string{"Terraform (HCL)", "Pulumi", "AWS CDK", "Azure Bicep", "Serverless Framework"}
	got := platformToolSamples(in, 4)
	want := "Terraform, Pulumi, AWS CDK, Azure Bicep, …"
	if got != want {
		t.Errorf("platformToolSamples = %q, want %q", got, want)
	}
	// De-duplication on the shortened leading token.
	dup := platformToolSamples([]string{"Helm charts", "Helm charts"}, 4)
	if dup != "Helm charts" {
		t.Errorf("platformToolSamples dedup = %q, want %q", dup, "Helm charts")
	}
}

// --- helpers ---

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// sectionBody returns the text from heading up to (not including) the next
// "## " heading at the same level.
func sectionBody(page, heading string) string {
	i := strings.Index(page, heading)
	if i < 0 {
		return ""
	}
	rest := page[i+len(heading):]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		return rest[:j]
	}
	return rest
}

func firstTableHeader(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "| Language | Name |") {
			return line
		}
	}
	return ""
}

func lineContaining(s, sub string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, sub) {
			return line
		}
	}
	return ""
}

// assertNoAlwaysEmptyColumn checks every markdown table on the page: each
// capability column (those between "Name" and the trailing Status/Notes
// columns) must have at least one non-"—" data cell across the table's
// rows.
func assertNoAlwaysEmptyColumn(t *testing.T, page string) {
	t.Helper()
	lines := strings.Split(page, "\n")
	var headers []string
	var dataRows [][]string
	flush := func() {
		if len(headers) == 0 {
			return
		}
		// Columns: skip 0 (empty), 1 (Language), 2 (Name); drop the last
		// two (Status, Notes) — capability columns are the middle ones.
		for col := 3; col < len(headers)-2; col++ {
			name := strings.TrimSpace(headers[col])
			if name == "" {
				continue
			}
			seen := false
			for _, r := range dataRows {
				if col < len(r) && strings.TrimSpace(r[col]) != "—" && strings.TrimSpace(r[col]) != "" {
					seen = true
					break
				}
			}
			if len(dataRows) > 0 && !seen {
				t.Errorf("column %q is structurally always-empty (all —) in a platform table", name)
			}
		}
		headers = nil
		dataRows = nil
	}
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(trimmed, "| Language | Name |"):
			flush()
			headers = strings.Split(ln, "|")
		case strings.HasPrefix(trimmed, "|---"):
			// separator row — ignore
		case strings.HasPrefix(trimmed, "|") && headers != nil:
			dataRows = append(dataRows, strings.Split(ln, "|"))
		default:
			flush()
		}
	}
	flush()
}
