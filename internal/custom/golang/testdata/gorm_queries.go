package repo

import "gorm.io/gorm"

func queries(db *gorm.DB) {
	var users []User
	var user User

	db.Where("age > ?", 18).Find(&users)
	db.Model(&User{}).Where("name = ?", "alice").First(&user)
	db.Joins("Company").Preload("Orders").Find(&users)
	db.Select("id", "name").Order("created_at desc").Limit(10).Find(&users)
	db.Create(&user)
	db.Model(&user).Updates(map[string]interface{}{"age": 30})
	db.Delete(&user)
	db.Table("legacy_users").Count(&[]int64{0}[0])
}

type User struct{}
