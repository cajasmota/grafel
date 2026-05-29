package store

import (
	"github.com/elastic/go-elasticsearch/v8"
)

// Product is a document shape marshalled to JSON for indexing.
type Product struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Price float64 `json:"price,omitempty"`
	Skip  string  `json:"-"`
}

func run(es *elasticsearch.Client) error {
	// Index-creation: the migration analogue.
	if _, err := es.Indices.Create("products"); err != nil {
		return err
	}

	// Search the products index.
	if _, err := es.Search(
		es.Search.WithIndex("products"),
	); err != nil {
		return err
	}

	// Index a document.
	if _, err := es.Index("products", nil); err != nil {
		return err
	}

	if _, err := es.Get("products", "1"); err != nil {
		return err
	}

	if _, err := es.Delete("products", "1"); err != nil {
		return err
	}

	_, err := es.Count(es.Count.WithIndex("products"))
	return err
}
