package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// realMePageRetrievePipeline is a BYTE-COPY of the live upvate-v3 consumer
// src/modules/me-page/pipelines/me-page-retrieve.pipeline.ts. It exercises the
// FUNCTIONAL aggregation-builder factory `mongo<T>()` followed by a fluent
// chain that includes `.lookupOne({ from: 'me_pages', ... })`. The pipeline is
// RETURNED (never passed to `.aggregate(...)`), which is precisely the live
// gap #4320: the existing scanner is gated on a `.aggregate(` call site.
const realMePageRetrievePipeline = `import { Types } from 'mongoose';
import { mongo } from '../../../common/query/mongo/aggregation.builder';
import type { AggregationBuilder } from '../../../common/query/mongo/aggregation.builder';
import type { MePageRetrieveRaw } from '../projections/me-page-retrieve.projection';

export interface MePageRetrievePipelineOptions {
  versionId: string;
}

interface MePageVersionSource {
  page_id: Types.ObjectId;
  name?: string;
  page_content?: Record<string, unknown>;
  created_at?: Date;
  updated_at?: Date;
  updated_by?: string;
  version_number?: number;
}

export function mePageRetrievePipeline(opts: MePageRetrievePipelineOptions): AggregationBuilder<MePageRetrieveRaw> {
  return mongo<MePageVersionSource>()
    .matchId(new Types.ObjectId(opts.versionId))
    .lookupOne({ from: 'me_pages', localField: 'page_id', foreignField: '_id', as: 'page' })
    .project({
      _id: 0,
      id: { $toString: '$page._id' },
      version_id: { $toString: '$_id' },
      name: 1,
      page_content: 1,
      is_current_version: { $eq: ['$_id', '$page.current_version'] },
      created_at: 1,
      updated_at: 1,
      updated_by: 1,
      version_number: 1,
    })
    .limit(1) as unknown as AggregationBuilder<MePageRetrieveRaw>;
}
`

// runORMOnSource drives the FULL applyORMQueries pass over a single TS source
// file (the live extraction entry point), returning the appended DataAccess
// stage entities and JOINS_COLLECTION edges.
func runORMOnSource(t *testing.T, path, src string) ([]types.EntityRecord, []types.RelationshipRecord) {
	t.Helper()
	res := applyORMQueries(DetectorPassArgs{
		Lang:    "typescript",
		Path:    path,
		Content: []byte(src),
	})
	var stages []types.EntityRecord
	for _, e := range res.Entities {
		if e.Kind == string(types.EntityKindDataAccess) {
			stages = append(stages, e)
		}
	}
	var joins []types.RelationshipRecord
	for _, r := range res.Relationships {
		if r.Kind == string(types.RelationshipKindJoinsCollection) {
			joins = append(joins, r)
		}
	}
	return stages, joins
}

// TestMongoFuncBuilder_4320_RealSource reproduces the live gap on a byte-copy of
// the real upvate-v3 consumer, then (after the fix) asserts the data-access
// semantics are emitted: a SCOPE.DataAccess node for the pipeline stages and a
// JOINS_COLLECTION edge to the looked-up `me_pages` collection.
func TestMongoFuncBuilder_4320_RealSource(t *testing.T) {
	stages, joins := runORMOnSource(t,
		"src/modules/me-page/pipelines/me-page-retrieve.pipeline.ts",
		realMePageRetrievePipeline)

	if len(stages) == 0 {
		t.Fatalf("GAP: no SCOPE.DataAccess stage entities emitted for functional mongo<>() builder pipeline")
	}

	// The lookupOne({ from: 'me_pages' }) must yield a $lookup data-access stage.
	var hasLookup bool
	for _, s := range stages {
		if s.Subtype == "$lookup" && s.Properties["from"] == "me_pages" {
			hasLookup = true
		}
	}
	if !hasLookup {
		t.Errorf("GAP: no $lookup DataAccess stage with from=me_pages; got stages=%v", stageSubtypesInOrder(stages))
	}

	// And a JOINS_COLLECTION edge to the me_pages collection Class.
	if findJoinTo(joins, CapitalisedSingular("me_pages")) == nil {
		t.Errorf("GAP: no JOINS_COLLECTION edge to Class:%s; joins=%v", CapitalisedSingular("me_pages"), joins)
	}

	// Stage ORDER is preserved: matchId → lookupOne($lookup) → project → limit.
	subs := stageSubtypesInOrder(stages)
	want := []string{"$match", "$lookup", "$project", "$limit"}
	if len(subs) != len(want) {
		t.Fatalf("stage subtypes = %v, want %v", subs, want)
	}
	for i := range want {
		if subs[i] != want[i] {
			t.Fatalf("stage subtypes = %v, want %v", subs, want)
		}
	}
}

// realInspectionDevicesPipeline is a TRIMMED byte-faithful copy of the live
// upvate-v3 consumer inspection-devices.pipeline.ts — it mixes an INLINE
// `.lookup({ from: 'inspection_groups', ... })` (statically resolvable join)
// with VARIABLE-passed `.lookupPipelineOne(deviceLookup)` (the `from` lives in a
// separate const → honestly unresolved). Asserts the inline join resolves and
// the variable-passed lookups do not crash or fabricate a join.
const realInspectionDevicesPipeline = `import { mongo, type AggregationBuilder, type PipelineLookup } from '../../../common/query/mongo/aggregation.builder';

const deviceLookup: PipelineLookup = { from: 'm_devices', as: 'device', pipeline: (sub) => sub.limit(1) };

export function inspectionDevicesPipeline(opts: { buildingId: number }): AggregationBuilder<unknown> {
  return mongo<Record<string, unknown>>()
    .lookup({ from: 'inspection_groups', localField: 'inspectionGroupId', foreignField: '_id', as: 'inspections_group' })
    .unwind('inspections_group', { preserveNullAndEmptyArrays: true })
    .match((w) => { w.eq('building_id', opts.buildingId); })
    .lookupPipelineOne(deviceLookup)
    .limit(50) as unknown as AggregationBuilder<unknown>;
}
`

func TestMongoFuncBuilder_4320_InlineLookupResolves(t *testing.T) {
	stages, joins := runORMOnSource(t,
		"src/modules/buildings/pipelines/inspection-devices.pipeline.ts",
		realInspectionDevicesPipeline)

	if len(stages) == 0 {
		t.Fatalf("no DataAccess stages emitted")
	}
	// Inline `.lookup({ from: 'inspection_groups' })` → JOINS_COLLECTION edge.
	if findJoinTo(joins, CapitalisedSingular("inspection_groups")) == nil {
		t.Errorf("no JOINS_COLLECTION edge to Class:%s; joins=%v",
			CapitalisedSingular("inspection_groups"), joins)
	}
	// Two $lookup stages: the inline one (resolved) AND the variable-passed
	// lookupPipelineOne (stage present, join honestly unresolved). The latter
	// must NOT fabricate a join to a phantom collection.
	var lookupStages int
	for _, s := range stages {
		if s.Subtype == "$lookup" {
			lookupStages++
		}
	}
	if lookupStages < 2 {
		t.Errorf("expected >=2 $lookup stages (inline + variable-passed), got %d", lookupStages)
	}
	// Honest limit: the only resolved join target is inspection_groups; the
	// variable-passed deviceLookup `from` (m_devices, in a separate const) is NOT
	// fabricated from the chain.
	if findJoinTo(joins, CapitalisedSingular("m_devices")) != nil {
		t.Errorf("fabricated a join to m_devices from a variable-passed lookup (should be unresolved)")
	}
}
