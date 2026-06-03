package engine

import "testing"

// TestCFN_StampScalarProps_YAML asserts the EXACT curated scalar config
// properties are stamped on YAML resources, and that intrinsic-function /
// nested values are NOT stamped.
func TestCFN_StampScalarProps_YAML(t *testing.T) {
	src := `AWSTemplateFormatVersion: "2010-09-09"
Resources:
  ProcessorFn:
    Type: AWS::Lambda::Function
    Properties:
      Runtime: python3.12
      MemorySize: 512
      Timeout: 30
      Role: !GetAtt FnRole.Arn
      Environment:
        Variables:
          BUCKET: !Ref DataBucket
  Db:
    Type: AWS::RDS::DBInstance
    Properties:
      Engine: postgres
      DBInstanceClass: db.t3.micro
      AllocatedStorage: 20
      MasterUsername: !Ref DbUser
`
	ents, _ := cfnRun("yaml", "infra/template.yaml", src)

	fn := cfnFindEntityByLogical(ents, "ProcessorFn")
	if fn == nil {
		t.Fatalf("missing ProcessorFn entity")
	}
	for k, want := range map[string]string{
		"Runtime":    "python3.12",
		"MemorySize": "512",
		"Timeout":    "30",
	} {
		if got := fn.Properties[k]; got != want {
			t.Errorf("ProcessorFn.%s = %q, want %q", k, got, want)
		}
	}
	// Role is a !GetAtt intrinsic — must NOT be stamped (it stays an edge). Note
	// "Role" is not in the allow-list anyway, but assert no leakage of intrinsics.
	if v, ok := fn.Properties["Role"]; ok {
		t.Errorf("Role intrinsic must not be stamped; got %q", v)
	}

	db := cfnFindEntityByLogical(ents, "Db")
	if db == nil {
		t.Fatalf("missing Db entity")
	}
	for k, want := range map[string]string{
		"Engine":           "postgres",
		"DBInstanceClass":  "db.t3.micro",
		"AllocatedStorage": "20",
	} {
		if got := db.Properties[k]; got != want {
			t.Errorf("Db.%s = %q, want %q", k, got, want)
		}
	}
	// MasterUsername is !Ref → reference, and not in the allow-list either.
	if v, ok := db.Properties["MasterUsername"]; ok {
		t.Errorf("MasterUsername intrinsic must not be stamped; got %q", v)
	}
}

// TestCFN_StampScalarProps_JSON asserts curated scalars from a JSON template and
// that a long-form { "Fn::GetAtt": [...] } value is not stamped.
func TestCFN_StampScalarProps_JSON(t *testing.T) {
	src := `{
  "Resources": {
    "WebInstance": {
      "Type": "AWS::EC2::Instance",
      "Properties": {
        "InstanceType": "t3.micro",
        "SubnetId": { "Ref": "Subnet" }
      }
    }
  }
}`
	ents, _ := cfnRun("json", "infra/template.json", src)
	inst := cfnFindEntityByLogical(ents, "WebInstance")
	if inst == nil {
		t.Fatalf("missing WebInstance entity")
	}
	if got := inst.Properties["InstanceType"]; got != "t3.micro" {
		t.Errorf("WebInstance.InstanceType = %q, want %q", got, "t3.micro")
	}
	// SubnetId is a { "Ref": ... } object → not a scalar, not in allow-list.
	if v, ok := inst.Properties["SubnetId"]; ok {
		t.Errorf("SubnetId ref object must not be stamped; got %q", v)
	}
}

// TestCFN_StampScalarProps_SubTemplateNotStamped is the boundary case: a curated
// key whose value is a !Sub / ${...} template must NOT be stamped as a scalar.
func TestCFN_StampScalarProps_SubTemplateNotStamped(t *testing.T) {
	src := `Resources:
  Svc:
    Type: AWS::ECS::Service
    Properties:
      DesiredCount: 3
      Engine: !Sub "${EnginePrefix}-postgres"
`
	ents, _ := cfnRun("yaml", "infra/template.yaml", src)
	svc := cfnFindEntityByLogical(ents, "Svc")
	if svc == nil {
		t.Fatalf("missing Svc entity")
	}
	if got := svc.Properties["DesiredCount"]; got != "3" {
		t.Errorf("Svc.DesiredCount = %q, want %q", got, "3")
	}
	if v, ok := svc.Properties["Engine"]; ok {
		t.Errorf("!Sub-valued Engine must not be stamped; got %q", v)
	}
}

// Unit-level guard on the scalar value parser.
func TestCFN_LiteralScalarValue(t *testing.T) {
	cases := []struct {
		raw     string
		wantVal string
		wantOK  bool
	}{
		{`t3.micro`, "t3.micro", true},
		{`"t3.micro"`, "t3.micro", true},
		{`512`, "512", true},
		{`true`, "true", true},
		{`!Ref Foo`, "", false},
		{`!GetAtt Foo.Arn`, "", false},
		{`{ "Ref": "Foo" }`, "", false},
		{`!Sub "${X}-y"`, "", false},
		{`"${X}-y"`, "", false},
		{`[1, 2]`, "", false},
		{``, "", false},
	}
	for _, c := range cases {
		got, ok := cfnLiteralScalarValue(c.raw)
		if ok != c.wantOK || got != c.wantVal {
			t.Errorf("cfnLiteralScalarValue(%q) = (%q,%v), want (%q,%v)", c.raw, got, ok, c.wantVal, c.wantOK)
		}
	}
}
