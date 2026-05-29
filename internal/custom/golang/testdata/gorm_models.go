package models

import (
	"time"

	"gorm.io/gorm"
)

// User embeds gorm.Model and carries explicit column/type field tags.
type User struct {
	gorm.Model
	Name      string `gorm:"column:name;type:varchar(255);not null"`
	Email     string `gorm:"column:email_address;type:varchar(320);uniqueIndex"`
	Age       int    `gorm:"column:age;type:int"`
	CompanyID uint   `gorm:"column:company_id"`

	// Relationships.
	Company Company  `gorm:"foreignKey:CompanyID"`
	Orders  []Order  `gorm:"foreignKey:UserID"`
	Profile *Profile `gorm:"foreignKey:UserID;references:ID"`
	Roles   []Role   `gorm:"many2many:user_roles"`
}

// Company is a plain GORM model with field tags but no gorm.Model embed.
type Company struct {
	ID   uint   `gorm:"primaryKey;column:id"`
	Name string `gorm:"column:name;type:text"`
}

type Order struct {
	gorm.Model
	UserID uint    `gorm:"column:user_id"`
	Total  float64 `gorm:"column:total;type:numeric(10,2)"`
}

type Profile struct {
	gorm.Model
	UserID uint   `gorm:"column:user_id"`
	Bio    string `gorm:"column:bio;type:text"`
}

type Role struct {
	gorm.Model
	Name      string    `gorm:"column:name"`
	CreatedAt time.Time `gorm:"column:created_at"`
}
