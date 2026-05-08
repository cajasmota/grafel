package java

import "regexp"

// Field extraction from Java source using regex.
// Ported from: memx_indexer/languages/java/parser.py (field_declaration)

// FieldInfo holds extracted field metadata.
type FieldInfo struct {
	Name        string
	TypeName    string
	Annotations []string
	Line        int
}

// fieldDeclRE matches Java field declarations including annotations.
// Captures: optional annotations block, type, field name.
var fieldDeclRE = regexp.MustCompile(
	`(?m)(?:(@\w+(?:\([^)]*\))?)\s+)*` +
		`(?:(?:private|protected|public)\s+)?` +
		`(?:(?:static|final|transient|volatile)\s+)*` +
		`(\w+(?:\s*<[^>]*>)?)\s+` +
		`(\w+)\s*[;=]`,
)

// fieldAnnotationRE extracts individual annotations from a line.
var fieldAnnotationRE = regexp.MustCompile(`@(\w+)`)

// ExtractFields scans Java source for field declarations and returns
// the extracted field metadata. This is used by Lombok inference to
// know which fields exist on a class.
func ExtractFields(source, filePath string) []FieldInfo {
	var fields []FieldInfo

	for _, m := range fieldDeclRE.FindAllStringSubmatchIndex(source, -1) {
		// Group 2 = type, Group 3 = name
		if m[4] < 0 || m[6] < 0 {
			continue
		}
		typeName := source[m[4]:m[5]]
		fieldName := source[m[6]:m[7]]

		// Skip common false positives
		if typeName == "class" || typeName == "interface" || typeName == "enum" ||
			typeName == "return" || typeName == "new" || typeName == "import" ||
			typeName == "package" || typeName == "throws" || typeName == "extends" ||
			typeName == "implements" {
			continue
		}

		line := lineOf(source, m[0])

		// Collect annotations from the preceding context (up to 200 chars before)
		start := m[0]
		if start > 200 {
			start = m[0] - 200
		} else {
			start = 0
		}
		window := source[start:m[0]]
		var annotations []string
		for _, am := range fieldAnnotationRE.FindAllStringSubmatch(window, -1) {
			annotations = append(annotations, "@"+am[1])
		}

		fields = append(fields, FieldInfo{
			Name:        fieldName,
			TypeName:    typeName,
			Annotations: annotations,
			Line:        line,
		})
	}

	return fields
}
