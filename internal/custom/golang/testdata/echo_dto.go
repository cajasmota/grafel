package testdata

import "github.com/labstack/echo/v4"

// LoginReq is the request DTO bound by echo's c.Bind.
type LoginReq struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required,min=6"`
}

// TokenResp is the response DTO serialised by c.JSON.
type TokenResp struct {
	Token   string `json:"token"`
	Expires int64  `json:"expires"`
}

func login(c echo.Context) error {
	var req LoginReq
	if err := c.Bind(&req); err != nil {
		return err
	}
	resp := TokenResp{Token: "abc", Expires: 3600}
	return c.JSON(200, resp)
}

func setupEcho() {
	e := echo.New()
	e.POST("/login", login)
	_ = e
}
