package order

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// OrderServiceSuite exercises OrderService via the testify suite pattern.
type OrderServiceSuite struct {
	suite.Suite
	svc *OrderService
}

func (s *OrderServiceSuite) SetupTest() {
	s.svc = NewOrderService(&fakeRepo{})
}

func (s *OrderServiceSuite) TestPlace() {
	id, err := s.svc.Place("widget")
	require.NoError(s.T(), err)
	assert.Equal(s.T(), 1, id)
}

func (s *OrderServiceSuite) TestPlaceEmpty() {
	_, err := s.svc.Place("")
	assert.Error(s.T(), err)
}

func TestOrderServiceSuite(t *testing.T) {
	suite.Run(t, new(OrderServiceSuite))
}

// Top-level stdlib test exercising OrderService.Place via name affinity
// (TestOrderService_Place -> OrderService.Place).
func TestOrderService_Place(t *testing.T) {
	svc := NewOrderService(&fakeRepo{})
	id, err := svc.Place("gadget")
	require.NoError(t, err)
	assert.Equal(t, 1, id)
}

type fakeRepo struct{}

func (f *fakeRepo) Save(item string) (int, error) { return 1, nil }
