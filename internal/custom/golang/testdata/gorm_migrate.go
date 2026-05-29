package migrate

import "gorm.io/gorm"

func run(db *gorm.DB) {
	db.AutoMigrate(&User{}, &Company{}, &Order{})
	db.Migrator().CreateTable(&Role{})
	db.Migrator().AddColumn(&User{}, "Nickname")
	db.Migrator().CreateIndex(&User{}, "Email")
	db.Migrator().DropTable(&LegacyAudit{})
}

type (
	User        struct{}
	Company     struct{}
	Order       struct{}
	Role        struct{}
	LegacyAudit struct{}
)
