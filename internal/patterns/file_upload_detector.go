package patterns

import (
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// fileUploadDetector detects file upload handling patterns.
// Matches Python file_upload_detector.py.
type fileUploadDetector struct{}

var (
	fuMulterInitRE        = regexp.MustCompile(`\bmulter\s*\(`)
	fuFormidableRE       = regexp.MustCompile(`(?:new\s+Formidable\s*\(|formidable\s*\()`)
	fuBusboyRE           = regexp.MustCompile(`\bbusboy\s*\(`)
	fuNestUploadedFileRE = regexp.MustCompile(`@UploadedFile\s*\(`)
	fuPyFlaskUploadRE     = regexp.MustCompile(`request\.files\[`)
	fuPyFastAPIUploadRE   = regexp.MustCompile(`UploadFile\b`)
	fuGoMultipartRE       = regexp.MustCompile(`(?:ParseMultipartForm|FormFile)\s*\(`)
	fuSpringMultipartRE   = regexp.MustCompile(`@RequestPart\b|MultipartFile\b`)
)

func (f *fileUploadDetector) Category() string { return "file_upload" }

func (f *fileUploadDetector) AppliesTo(src string) bool {
	return fuMulterInitRE.MatchString(src) ||
		fuFormidableRE.MatchString(src) ||
		fuBusboyRE.MatchString(src) ||
		fuNestUploadedFileRE.MatchString(src) ||
		fuPyFlaskUploadRE.MatchString(src) ||
		fuPyFastAPIUploadRE.MatchString(src) ||
		fuGoMultipartRE.MatchString(src) ||
		fuSpringMultipartRE.MatchString(src)
}

func (f *fileUploadDetector) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, name, library string, line int) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			name, "SCOPE.Pattern", "file_upload", language, line,
			map[string]string{"kind": "file_upload", "library": library}))
	}

	if m := fuMulterInitRE.FindStringIndex(src); m != nil {
		emit("multer", "file_upload_multer", "multer", lineOf(src, m[0]))
	}
	if m := fuFormidableRE.FindStringIndex(src); m != nil {
		emit("formidable", "file_upload_formidable", "formidable", lineOf(src, m[0]))
	}
	if m := fuBusboyRE.FindStringIndex(src); m != nil {
		emit("busboy", "file_upload_busboy", "busboy", lineOf(src, m[0]))
	}
	if m := fuNestUploadedFileRE.FindStringIndex(src); m != nil {
		emit("nestjs_upload", "file_upload_nestjs", "nestjs", lineOf(src, m[0]))
	}
	if m := fuPyFlaskUploadRE.FindStringIndex(src); m != nil {
		emit("flask_upload", "file_upload_flask", "flask", lineOf(src, m[0]))
	}
	if m := fuPyFastAPIUploadRE.FindStringIndex(src); m != nil {
		emit("fastapi_upload", "file_upload_fastapi", "fastapi", lineOf(src, m[0]))
	}
	if m := fuGoMultipartRE.FindStringIndex(src); m != nil {
		emit("go_multipart", "file_upload_go", "go_net_http", lineOf(src, m[0]))
	}
	if m := fuSpringMultipartRE.FindStringIndex(src); m != nil {
		emit("spring_multipart", "file_upload_spring", "spring", lineOf(src, m[0]))
	}

	return results
}

func init() {
	Register(&fileUploadDetector{})
}
