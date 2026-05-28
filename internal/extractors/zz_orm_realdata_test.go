package extractors

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/cajasmota/archigraph/internal/custom/javascript"
)

// TestORMRealDataCorpus walks an on-disk multi-file ORM corpus and runs the
// full custom-extractor dispatch pipeline (RunCustomExtractors + dispatch +
// language detection by extension) against it. This is the real-data check for
// issue #2861 — distinct from the in-package unit fixtures, it exercises the
// disk-backed dispatch path the daemon uses, including the prisma/sql →
// custom_js_ language routing. Defaults to the in-repo fixture corpus; set
// ORM_CORPUS to point at a different corpus dir.
func TestORMRealDataCorpus(t *testing.T) {
	root := os.Getenv("ORM_CORPUS")
	if root == "" {
		// Repo-root testdata/fixtures/orm_corpus, relative to this package dir
		// (internal/extractors/) which is the test's working directory.
		root = filepath.Join("..", "..", "testdata", "fixtures", "orm_corpus")
	}
	langByExt := map[string]string{
		".ts":     "typescript",
		".js":     "javascript",
		".sql":    "sql",
		".prisma": "prisma",
	}
	type want struct{ subtype, table string }
	got := map[string]int{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		lang := langByExt[strings.ToLower(filepath.Ext(path))]
		if lang == "" {
			return nil
		}
		content, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		ents, _ := RunCustomExtractors(context.Background(), FileInput{Path: path, Language: lang, Content: content})
		for _, e := range ents {
			if e.Kind == "SCOPE.Evolution" {
				got[e.Properties["framework"]+"/"+e.Subtype]++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Every ORM must yield at least its create/drop table schema-change ops.
	for _, w := range []want{
		{"prisma/create_table", ""},
		{"drizzle/create_table", ""},
		{"sequelize/create_table", ""},
		{"typeorm/create_table", ""},
		{"knex/create_table", ""},
		{"mikro-orm/create_table", ""},
		{"objection/create_table", ""},
	} {
		if got[w.subtype] == 0 {
			t.Errorf("real-data corpus: no migration op %q emitted", w.subtype)
		} else {
			t.Logf("real-data corpus: %s x%d", w.subtype, got[w.subtype])
		}
	}
}
