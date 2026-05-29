package store

import (
	"context"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func run(ctx context.Context, session neo4j.SessionWithContext) error {
	// Read with node label + relationship type.
	if _, err := session.Run(ctx,
		`MATCH (p:Person)-[:ACTED_IN]->(m:Movie) RETURN p, m`,
		nil); err != nil {
		return err
	}

	// Create a node.
	if _, err := session.Run(ctx,
		`CREATE (p:Person {name: $name}) RETURN p`,
		map[string]any{"name": "Ada"}); err != nil {
		return err
	}

	// Merge a relationship.
	if _, err := session.Run(ctx,
		`MATCH (a:Person), (b:Person) MERGE (a)-[r:KNOWS]->(b) RETURN r`,
		nil); err != nil {
		return err
	}

	// Delete.
	_, err := session.Run(ctx, `MATCH (p:Person {name: $name}) DETACH DELETE p`, nil)
	return err
}
