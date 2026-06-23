package java_test

// config_consumer_test.go — value-asserting tests for the Java config-read pass
// (issue #3641, epic #3625). Asserts the SPECIFIC config key, not len>0.

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func extractJavaRaw(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "Cfg.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	return recs
}

func javaConfigKeysFrom(recs []types.EntityRecord, from string) map[string]bool {
	keys := map[string]bool{}
	for i := range recs {
		if recs[i].Name != from {
			continue
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == "DEPENDS_ON_CONFIG" {
				keys[r.Properties["config_key"]] = true
			}
		}
	}
	return keys
}

func javaHasConfigKeyEntity(recs []types.EntityRecord, key string) bool {
	for i := range recs {
		e := &recs[i]
		if e.Kind == "SCOPE.Config" && e.Subtype == "config_key" && e.Properties["config_key"] == key {
			return true
		}
	}
	return false
}

func TestJavaConfigConsumer_ValueAnnotation(t *testing.T) {
	src := `package com.example;

public class AppConfig {
    @Value("${app.timeout}")
    private int timeout;

    @Value("${db.url:jdbc:default}")
    private String dbUrl;
}
`
	recs := extractJavaRaw(t, src)
	keys := javaConfigKeysFrom(recs, "AppConfig")
	if !keys["app.timeout"] {
		t.Errorf("expected @Value read of app.timeout, got %v", keys)
	}
	// Default-value suffix must be stripped: db.url, not "db.url:jdbc:default".
	if !keys["db.url"] {
		t.Errorf("expected @Value read of db.url (default stripped), got %v", keys)
	}
	if keys["db.url:jdbc:default"] {
		t.Errorf("default suffix must be stripped from the config key")
	}
	if !javaHasConfigKeyEntity(recs, "app.timeout") {
		t.Errorf("expected config_key entity for app.timeout")
	}
}

func TestJavaConfigConsumer_ConfigurationProperties(t *testing.T) {
	src := `package com.example;

@ConfigurationProperties(prefix = "app")
public class AppProps {
    private int timeout;
}
`
	recs := extractJavaRaw(t, src)
	keys := javaConfigKeysFrom(recs, "AppProps")
	if !keys["app"] {
		t.Fatalf("expected @ConfigurationProperties prefix read of app, got %v", keys)
	}
}

func TestJavaConfigConsumer_EnvGetProperty(t *testing.T) {
	src := `package com.example;

public class Boot {
    public String port(Environment env) {
        return env.getProperty("server.port");
    }
}
`
	recs := extractJavaRaw(t, src)
	keys := javaConfigKeysFrom(recs, "Boot.port")
	if !keys["server.port"] {
		t.Fatalf("expected env.getProperty read of server.port, got %v", keys)
	}
}

func TestJavaConfigConsumer_ConfigProperty(t *testing.T) {
	src := `package com.example;

public class MpBean {
    @ConfigProperty(name = "db.url")
    String dbUrl;
}
`
	recs := extractJavaRaw(t, src)
	keys := javaConfigKeysFrom(recs, "MpBean")
	if !keys["db.url"] {
		t.Fatalf("expected @ConfigProperty read of db.url, got %v", keys)
	}
}

// Negative: env.getProperty(dynamicVar) must not fabricate a config key.
func TestJavaConfigConsumer_DynamicKeyIgnored(t *testing.T) {
	src := `package com.example;

public class Boot {
    public String read(Environment env, String name) {
        return env.getProperty(name);
    }
}
`
	recs := extractJavaRaw(t, src)
	keys := javaConfigKeysFrom(recs, "Boot.read")
	if len(keys) != 0 {
		t.Fatalf("dynamic getProperty(name) must emit no config key, got %v", keys)
	}
	for i := range recs {
		if recs[i].Kind == "SCOPE.Config" && recs[i].Subtype == "config_key" {
			t.Errorf("dynamic key must not create a config_key entity: %q", recs[i].Name)
		}
	}
}
