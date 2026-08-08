// Package javascript — issue #6213, measured through the pipeline rather than
// at the predicate.
//
// This runs the two passes the daemon runs per file (base language extractor,
// then every registered custom extractor — internal/daemon/extract/subproc.go)
// over real NestJS source, and asserts on the merged record set the downstream
// id-keyed store dedups. That is the level the defect is observable at, and it
// is where the two halves meet:
//
//   - the base TS AST extractor emits SCOPE.Component/UsersService, and
//   - the NestJS custom extractor emits SCOPE.Component/UsersService,
//
// which is the SAME entity id (id = f(org, project, source_file, kind, name) —
// subtype is not an input). Before the fix the two carried contradictory
// subtypes, "angular_service" and "service", so which one reached the graph was
// decided by first-writer-wins at assembly rather than by what the class is.
//
// The observable consequence is engine.IsClassFoldSource: it is the gate on
// whether an entity is a class REPRESENTATION at all, consulted by the
// incremental path's FoldFrameworkClassKinds and (as isFoldSource /
// classLikeComponentSubtypes) by cmd/grafel's #1613 class-shadow fold and
// foldFileComponentDuplicates. It answered false for every NestJS provider,
// because ClassLikeComponentSubtypes carries the framework-neutral "service"
// and the record said "angular_service".
package javascript_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/types"
)

const nestServicePipelineSrc6213 = `import { Injectable } from '@nestjs/common';
import { InjectRepository } from '@nestjs/typeorm';
import { Repository } from 'typeorm';

import { User } from './user.entity';

@Injectable()
export class UsersService {
  constructor(
    @InjectRepository(User) private readonly users: Repository<User>,
  ) {}

  async findAll(): Promise<User[]> {
    return this.users.find();
  }
}
`

// TestIssue6213_NestServiceIsAClassFoldSource — the merged record set for a
// NestJS provider must be recognised as a class representation by the predicate
// every fold consults.
func TestIssue6213_NestServiceIsAClassFoldSource(t *testing.T) {
	ents := extractAngularPipeline(t, "src/users/users.service.ts", []byte(nestServicePipelineSrc6213))

	var got []types.EntityRecord
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Name == "UsersService" {
			got = append(got, e)
		}
	}
	if len(got) == 0 {
		t.Fatalf("no SCOPE.Component/UsersService in the merged set (%d entities)", len(ents))
	}

	for i := range got {
		e := got[i]
		if !engine.IsClassFoldSource(&e) {
			t.Errorf("engine.IsClassFoldSource == false for subtype=%q (provenance=%q): "+
				"a NestJS provider is invisible to the class folds",
				e.Subtype, e.Properties["provenance"])
		}
	}

	// Both emissions share one entity id, so a disagreement on subtype means the
	// graph's answer depends on assembly order. Pin that they agree.
	for i := 1; i < len(got); i++ {
		if got[i].ID != got[0].ID {
			continue
		}
		if got[i].Subtype != got[0].Subtype {
			t.Errorf("same entity id %s carries two subtypes: %q (framework=%q) and %q (provenance=%q)",
				got[0].ID, got[0].Subtype, got[0].Properties["framework"],
				got[i].Subtype, got[i].Properties["provenance"])
		}
	}
}

// TestIssue6213_AngularServiceStaysOutOfTheNestPath is the control at the same
// level: an Angular provider still comes out angular_service, and the NestJS
// custom extractor still declines the file (the #2933 bail on an @angular/*
// import), so there is exactly ONE record for it.
func TestIssue6213_AngularServiceStaysOutOfTheNestPath(t *testing.T) {
	const src = `import { Injectable } from '@angular/core';
import { HttpClient } from '@angular/common/http';

@Injectable({ providedIn: 'root' })
export class UserService {
  constructor(private http: HttpClient) {}
  load() { return this.http.get('/api/users'); }
}
`
	ents := extractAngularPipeline(t, "src/app/user.service.ts", []byte(src))

	var subtypes []string
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Name == "UserService" {
			subtypes = append(subtypes, e.Subtype)
		}
	}
	if len(subtypes) != 1 {
		t.Fatalf("want exactly one SCOPE.Component/UserService record, got %d: %v", len(subtypes), subtypes)
	}
	if subtypes[0] != "angular_service" {
		t.Errorf("subtype = %q, want %q", subtypes[0], "angular_service")
	}
}
