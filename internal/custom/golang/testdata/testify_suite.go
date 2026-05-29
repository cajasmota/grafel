package sample

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// UserServiceSuite exercises the user service via the testify suite pattern.
type UserServiceSuite struct {
	suite.Suite
	svc *UserService
}

func (s *UserServiceSuite) SetupTest() {
	s.svc = NewUserService()
}

func (s *UserServiceSuite) TestCreateUser() {
	u, err := s.svc.Create("alice")
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "alice", u.Name)
	assert.NotNil(s.T(), u.ID)
}

func (s *UserServiceSuite) TestDeleteUser() {
	err := s.svc.Delete(42)
	assert.NoError(s.T(), err)
}

func TestUserServiceSuite(t *testing.T) {
	suite.Run(t, new(UserServiceSuite))
}

// A plain (non-suite) test must not be mistaken for a suite case.
func TestStandalone(t *testing.T) {
	assert.True(t, true)
}
