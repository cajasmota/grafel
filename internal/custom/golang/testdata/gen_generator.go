package main

import (
	"gorm.io/gen"

	"example.com/app/model"
)

// gen generator program: wires GORM models into the typed query API.
func main() {
	g := gen.NewGenerator(gen.Config{
		OutPath: "./query",
		Mode:    gen.WithoutContext | gen.WithDefaultQuery,
	})

	g.ApplyBasic(model.User{}, model.Post{})
	g.ApplyInterface(func(model.Querier) {}, model.User{})

	// Introspection path: generate models directly from DB tables.
	g.GenerateModel("comments")
	g.GenerateModelAs("user_roles", "UserRole")

	g.Execute()
}
