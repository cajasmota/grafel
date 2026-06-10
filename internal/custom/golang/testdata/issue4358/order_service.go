package order

import "errors"

// OrderService is the production unit under test in order_service_test.go.
type OrderService struct {
	repo Repo
}

// NewOrderService constructs an OrderService.
func NewOrderService(repo Repo) *OrderService {
	return &OrderService{repo: repo}
}

// Place places an order and returns its id.
func (s *OrderService) Place(item string) (int, error) {
	if item == "" {
		return 0, errors.New("empty item")
	}
	return s.repo.Save(item)
}

// Repo is the persistence port.
type Repo interface {
	Save(item string) (int, error)
}
