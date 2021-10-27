package model

import (
	"fmt"
	"time"

	"github.com/cloudquery/sqlite"
	"github.com/indes/flowerss-bot/config"
	"github.com/indes/flowerss-bot/log"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var db *gorm.DB

// InitDB init db object
func InitDB() {
	connectDB()
	configDB()
	updateTable()
}

func configDB() {
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetMaxOpenConns(50)
	}
}

func updateTable() {
	createOrUpdateTable(&Subscribe{})
	createOrUpdateTable(&User{})
	createOrUpdateTable(&Source{})
	createOrUpdateTable(&Option{})
	createOrUpdateTable(&Content{})
}

// connectDB connect to db
func connectDB() {
	if config.RunMode == config.TestMode {
		return
	}

	dbLogger := &log.DbLogger{}
	if config.DBLogMode {
		dbLogger.LogMode(logger.Info)
	} else {
		dbLogger.LogMode(logger.Error)
	}
	dbConfig := &gorm.Config{
		Logger: dbLogger,
	}

	var err error
	if config.EnableMysql {
		db, err = gorm.Open(mysql.New(mysql.Config{
			DSN: fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
				config.Mysql.User, config.Mysql.Password, config.Mysql.Host, config.Mysql.Port, config.Mysql.DB),
		}), dbConfig)
	} else {
		dbConfig.SkipDefaultTransaction = true
		dbConfig.PrepareStmt = true
		db, err = gorm.Open(sqlite.Open(config.SQLitePath), dbConfig)
	}
	if err != nil {
		zap.S().Fatalf("connect db failed, err: %+v", err)
	}
}

// Disconnect disconnects from the database.
func Disconnect() {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

// createOrUpdateTable create table or Migrate table
func createOrUpdateTable(model interface{}) {
	if !db.Migrator().HasTable(model) {
		_ = db.Migrator().CreateTable(model)
	} else {
		_ = db.AutoMigrate(model)
	}
}

//EditTime timestamp
type EditTime struct {
	CreatedAt time.Time
	UpdatedAt time.Time
}
