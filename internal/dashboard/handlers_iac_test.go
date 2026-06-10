package dashboard

import "testing"

func TestIaCToolForEntity(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		subtype  string
		language string
		props    map[string]string
		wantTool string
		wantOK   bool
	}{
		{"cdk infraresource", "SCOPE.InfraResource", "", "typescript",
			map[string]string{"iac_tool": "aws-cdk"}, "aws-cdk", true},
		{"pulumi", "SCOPE.InfraResource", "", "python",
			map[string]string{"iac_tool": "pulumi"}, "pulumi", true},
		{"cfn datastore via semantic kind", "SCOPE.Datastore", "", "yaml",
			map[string]string{"iac_tool": "cloudformation"}, "cloudformation", true},
		{"terraform resource (no iac_tool prop)", "SCOPE.Component", "resource", "terraform",
			nil, "terraform", true},
		{"hcl resource alias", "SCOPE.Component", "resource", "hcl",
			nil, "terraform", true},
		{"terraform module block renders (#4625)", "SCOPE.Component", "module", "terraform",
			nil, "terraform", true},
		{"hcl module block renders (#4625)", "SCOPE.Component", "module", "hcl",
			nil, "terraform", true},
		{"terraform provider block is not a resource", "SCOPE.Component", "provider", "terraform",
			nil, "", false},
		{"plain component (non-iac)", "SCOPE.Component", "resource", "go",
			nil, "", false},
		{"non-iac function", "SCOPE.Function", "", "go", nil, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tool, ok := iacToolForEntity(c.kind, c.subtype, c.language, c.props)
			if tool != c.wantTool || ok != c.wantOK {
				t.Fatalf("iacToolForEntity = (%q,%v), want (%q,%v)", tool, ok, c.wantTool, c.wantOK)
			}
		})
	}
}

func TestIaCResourceTypeOf(t *testing.T) {
	cases := []struct {
		name  string
		ename string
		props map[string]string
		want  string
	}{
		{"cdk construct_type", "DataBucket", map[string]string{"construct_type": "s3.Bucket"}, "s3.Bucket"},
		{"cfn resource_type", "MyTable", map[string]string{"resource_type": "AWS::DynamoDB::Table"}, "AWS::DynamoDB::Table"},
		{"terraform name-encoded", "aws_db_instance.main", nil, "aws_db_instance"},
		{"terraform module (#4625)", "module.dispatch_queue", nil, "module"},
		{"no type", "thing", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := iacResourceTypeOf(c.ename, c.props); got != c.want {
				t.Fatalf("iacResourceTypeOf = %q, want %q", got, c.want)
			}
		})
	}
}

func TestIaCCategoryOf(t *testing.T) {
	// Explicit property wins.
	if got := iacCategoryOf("s3.Bucket", map[string]string{"resource_category": "storage"}); got != "storage" {
		t.Fatalf("explicit category = %q, want storage", got)
	}
	// resource_scope back-compat fallback.
	if got := iacCategoryOf("", map[string]string{"resource_scope": "queue"}); got != "queue" {
		t.Fatalf("resource_scope fallback = %q, want queue", got)
	}
	// Recomputed from type (Terraform path — category not in Properties).
	if got := iacCategoryOf("aws_db_instance", nil); got != "datastore" {
		t.Fatalf("recomputed category = %q, want datastore", got)
	}
	if got := iacCategoryOf("", nil); got != "" {
		t.Fatalf("empty category = %q, want empty", got)
	}
	// #4625 — a Terraform module instance is its own diagram category.
	if got := iacCategoryOf("module", nil); got != "module" {
		t.Fatalf("module category = %q, want module", got)
	}
}

func TestIaCRelationFacet(t *testing.T) {
	cases := []struct {
		name       string
		kind       string
		props      map[string]string
		wantFacet  string
		wantDetail string
	}{
		{"grant", "DEPENDS_ON", map[string]string{"reason": "grant", "grant": "grantReadWrite"}, "grant", "grantReadWrite"},
		{"event_source", "DEPENDS_ON", map[string]string{"reason": "event_source"}, "event_source", ""},
		{"props_ref dependency", "DEPENDS_ON", map[string]string{"reason": "props_ref", "props_ref": "dataBucket"}, "dependency", "dataBucket"},
		{"plain depends_on", "DEPENDS_ON", nil, "dependency", ""},
		{"contains topology", "CONTAINS", nil, "topology", ""},
		{"sam trigger", "TRIGGERS", map[string]string{"trigger": "Api"}, "trigger", "Api"},
		{"serves route", "SERVES", map[string]string{"http_method": "GET", "route_path": "/x"}, "trigger", "GET"},
		// #4625 — cross-module output ref carries a derived semantic verb as the facet.
		{"cross-module consumes", "USES",
			map[string]string{"dataflow": "cross_module", "semantic": "consumes", "module_output": "queue_url"},
			"consumes", "queue_url"},
		{"cross-module redrive", "USES",
			map[string]string{"dataflow": "cross_module", "semantic": "redrive", "module_output": "queue_arn"},
			"redrive", "queue_arn"},
		{"cross-module generic falls back to dependency", "USES",
			map[string]string{"dataflow": "cross_module", "semantic": "dependency", "module_output": "id"},
			"dependency", "id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			facet, detail := iacRelationFacet(c.kind, c.props)
			if facet != c.wantFacet || detail != c.wantDetail {
				t.Fatalf("iacRelationFacet = (%q,%q), want (%q,%q)", facet, detail, c.wantFacet, c.wantDetail)
			}
		})
	}
}

func TestIaCIsOutputEntity(t *testing.T) {
	if !iacIsOutputEntity("SCOPE.Config", "", map[string]string{"export_name": "BucketArn"}) {
		t.Fatal("cfn export should be an output entity")
	}
	if !iacIsOutputEntity("SCOPE.Schema", "output", nil) {
		t.Fatal("hcl output should be an output entity")
	}
	if iacIsOutputEntity("SCOPE.InfraResource", "", map[string]string{"iac_tool": "aws-cdk"}) {
		t.Fatal("a resource is not an output entity")
	}
}

func TestIDTail(t *testing.T) {
	if got := idTail("SCOPE.InfraResource:DataBucket"); got != "DataBucket" {
		t.Fatalf("idTail = %q, want DataBucket", got)
	}
	if got := idTail("noColon"); got != "noColon" {
		t.Fatalf("idTail = %q, want noColon", got)
	}
}
