package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// runMongoosePopulate drives scanJSMongoosePopulateJoins over `src` and
// collects the emitted JOINS_COLLECTION edges.
func runMongoosePopulate(t *testing.T, src string) []types.RelationshipRecord {
	t.Helper()
	var rels []types.RelationshipRecord
	scanJSMongoosePopulateJoins(src, "svc/book.ts", "typescript",
		func(r types.RelationshipRecord) { rels = append(rels, r) },
	)
	return rels
}

// findPopulateJoin returns the JOINS_COLLECTION edge from→to emitted by the
// populate pass (pattern_type=mongoose_populate), or nil.
func findPopulateJoin(rels []types.RelationshipRecord, fromClass, toClass string) *types.RelationshipRecord {
	for i := range rels {
		r := &rels[i]
		if r.Kind == string(types.RelationshipKindJoinsCollection) &&
			r.Properties["pattern_type"] == "mongoose_populate" &&
			r.FromID == "Class:"+fromClass && r.ToID == "Class:"+toClass {
			return r
		}
	}
	return nil
}

// Classic Mongoose schema literal: a `ref: 'Author'` field that is actually
// populated → JOINS_COLLECTION(Class:Book → Class:Author).
func TestMongoosePopulate_SchemaRef_Join(t *testing.T) {
	src := `
const mongoose = require('mongoose');
const { Schema } = mongoose;

const BookSchema = new mongoose.Schema({
  title: { type: String },
  author: { type: Schema.Types.ObjectId, ref: 'Author' },
});

const Book = mongoose.model('Book', BookSchema);

async function withAuthor(id) {
  return Book.findById(id).populate('author');
}
`
	rels := runMongoosePopulate(t, src)

	j := findPopulateJoin(rels, "Book", "Author")
	if j == nil {
		t.Fatalf("expected JOINS_COLLECTION Class:Book -> Class:Author, got %+v", rels)
	}
	if j.Properties["via"] != "populate" {
		t.Errorf("via = %q, want populate", j.Properties["via"])
	}
	if j.Properties["ref"] != "Author" {
		t.Errorf("ref = %q, want Author", j.Properties["ref"])
	}
	if j.Properties["ref_field"] != "author" {
		t.Errorf("ref_field = %q, want author", j.Properties["ref_field"])
	}
}

// Model name resolution falls back to the schema variable when there is no
// explicit model() registration: `BookSchema` → owner `Book`.
func TestMongoosePopulate_SchemaVarOwnerFallback(t *testing.T) {
	src := `
import mongoose, { Schema } from 'mongoose';

const BookSchema = new Schema({
  author: { type: Schema.Types.ObjectId, ref: 'Author' },
});

function load() {
  return query.populate('author');
}
`
	rels := runMongoosePopulate(t, src)
	if findPopulateJoin(rels, "Book", "Author") == nil {
		t.Fatalf("expected Class:Book -> Class:Author from schema-var fallback, got %+v", rels)
	}
}

// @nestjs/mongoose: `@Prop({ ref: 'Author' }) author` in a `@Schema()` class +
// a `.populate('author')` → JOINS_COLLECTION(Class:Book → Class:Author).
func TestMongoosePopulate_NestProp_Join(t *testing.T) {
	src := `
import { Prop, Schema, SchemaFactory } from '@nestjs/mongoose';
import * as mongoose from 'mongoose';
import { Types } from 'mongoose';

@Schema()
export class Book {
  @Prop({ required: true })
  title: string;

  @Prop({ type: Types.ObjectId, ref: 'Author' })
  author: Author;
}

export const BookSchema = SchemaFactory.createForClass(Book);

async function detail(model) {
  return model.findOne().populate('author');
}
`
	rels := runMongoosePopulate(t, src)

	j := findPopulateJoin(rels, "Book", "Author")
	if j == nil {
		t.Fatalf("expected JOINS_COLLECTION Class:Book -> Class:Author from @Prop ref, got %+v", rels)
	}
	if j.Properties["ref_field"] != "author" {
		t.Errorf("ref_field = %q, want author", j.Properties["ref_field"])
	}
}

// Multiple ref fields, multiple populates → multiple distinct join edges.
func TestMongoosePopulate_MultipleRefs(t *testing.T) {
	src := `
const mongoose = require('mongoose');
const { Schema } = mongoose;

const BookSchema = new mongoose.Schema({
  author: { type: Schema.Types.ObjectId, ref: 'Author' },
  publisher: { type: Schema.Types.ObjectId, ref: 'Publishers' },
  ignored: { type: Schema.Types.ObjectId, ref: 'Reviewer' },
});

const Book = mongoose.model('Book', BookSchema);

function full() {
  return Book.find().populate('author').populate('publisher');
}
`
	rels := runMongoosePopulate(t, src)

	if findPopulateJoin(rels, "Book", "Author") == nil {
		t.Errorf("missing Class:Book -> Class:Author; rels=%+v", rels)
	}
	// 'Publishers' singularises to 'Publisher'.
	if findPopulateJoin(rels, "Book", "Publisher") == nil {
		t.Errorf("missing Class:Book -> Class:Publisher; rels=%+v", rels)
	}
	// 'Reviewer' has a ref but is never populated → no edge.
	if findPopulateJoin(rels, "Book", "Reviewer") != nil {
		t.Errorf("did not expect Class:Book -> Class:Reviewer (ref never populated)")
	}
	// Exactly two populate joins.
	count := 0
	for _, r := range rels {
		if r.Properties["pattern_type"] == "mongoose_populate" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("populate join count = %d, want 2; rels=%+v", count, rels)
	}
}

// Negative: a ref declared but never populated yields NO edge.
func TestMongoosePopulate_RefNotPopulated_NoEdge(t *testing.T) {
	src := `
const mongoose = require('mongoose');
const { Schema } = mongoose;
const BookSchema = new mongoose.Schema({
  author: { type: Schema.Types.ObjectId, ref: 'Author' },
});
const Book = mongoose.model('Book', BookSchema);
function bare() { return Book.find(); }
`
	rels := runMongoosePopulate(t, src)
	if len(rels) != 0 {
		t.Fatalf("expected no edges when ref is never populated, got %+v", rels)
	}
}

// Negative: a dynamic `.populate(fieldVar)` (no string literal) yields NO edge,
// even though the ref is declared.
func TestMongoosePopulate_DynamicPopulate_NoEdge(t *testing.T) {
	src := `
const mongoose = require('mongoose');
const { Schema } = mongoose;
const BookSchema = new mongoose.Schema({
  author: { type: Schema.Types.ObjectId, ref: 'Author' },
});
const Book = mongoose.model('Book', BookSchema);
function dyn(field) { return Book.find().populate(field); }
`
	rels := runMongoosePopulate(t, src)
	if len(rels) != 0 {
		t.Fatalf("expected no edges for dynamic populate(field), got %+v", rels)
	}
}

// Negative: a dynamic `ref:` (variable, not a string literal) yields NO edge
// even though the field is populated.
func TestMongoosePopulate_DynamicRef_NoEdge(t *testing.T) {
	src := `
const mongoose = require('mongoose');
const { Schema } = mongoose;
const refTarget = 'Author';
const BookSchema = new mongoose.Schema({
  author: { type: Schema.Types.ObjectId, ref: refTarget },
});
const Book = mongoose.model('Book', BookSchema);
function load() { return Book.find().populate('author'); }
`
	rels := runMongoosePopulate(t, src)
	if len(rels) != 0 {
		t.Fatalf("expected no edges for dynamic ref, got %+v", rels)
	}
}

// Gate: a non-mongoose file is never scanned (no false joins on arbitrary
// `.populate(...)` from other libraries).
func TestMongoosePopulate_NonMongooseGate(t *testing.T) {
	src := `
function render() {
  grid.populate('rows');
}
const config = { ref: 'Author' };
`
	rels := runMongoosePopulate(t, src)
	if len(rels) != 0 {
		t.Fatalf("expected no edges in non-mongoose file, got %+v", rels)
	}
}
