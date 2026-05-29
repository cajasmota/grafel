package testdata

import "github.com/gin-gonic/gin"

// CreateUserReq is the request DTO bound from the JSON body.
type CreateUserReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
	Age      int    `json:"age"`
}

// UserResp is the response DTO serialised back to the client.
type UserResp struct {
	ID    int    `json:"id"`
	Email string `json:"email"`
}

func createUser(c *gin.Context) {
	var req CreateUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	resp := UserResp{ID: 1, Email: req.Email}
	c.JSON(200, resp)
}

func setup() {
	r := gin.Default()
	r.POST("/users", createUser)
	_ = r
}
