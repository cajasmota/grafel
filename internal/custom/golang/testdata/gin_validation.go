package fixtures

import (
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

// CreateUserReq is a gin-bound DTO using binding: struct tags.
type CreateUserReq struct {
	Name  string `json:"name" binding:"required,min=2,max=64"`
	Email string `json:"email" binding:"required,email"`
	Age   int    `json:"age" binding:"gte=0,lte=130"`
	Skip  string `json:"-" binding:"-"`
}

func setup() {
	r := gin.Default()

	validate := validator.New()
	validate.RegisterValidation("is_even", func(fl validator.FieldLevel) bool {
		return fl.Field().Int()%2 == 0
	})

	r.POST("/users", func(c *gin.Context) {
		var req CreateUserReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(201, req)
	})
}
