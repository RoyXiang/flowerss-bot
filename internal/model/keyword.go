package model

import (
	"errors"
)

type Keyword struct {
	ID      uint  `gorm:"primary_key;AUTO_INCREMENT"`
	UserID  int64 `gorm:"index"`
	Keyword string
	EditTime
}

func GetUserKeywordsByPage(userId int64, page, limit int) (keywords []Keyword, hasPrev, hasNext bool, err error) {
	offset := getPageOffset(page, limit)
	if offset < 0 {
		err = errors.New("无效的页数")
		return
	}

	var count int64
	condition := &Keyword{UserID: userId}
	if err = db.Model(condition).Where(condition).Count(&count).Error; err != nil {
		return
	}
	if err = db.Offset(offset).Limit(limit).Order("keyword").Where(condition).Find(&keywords).Error; err != nil {
		return
	}

	if page > 1 {
		hasPrev = true
	}
	if int64(offset)+int64(limit) < count {
		hasNext = true
	}
	return
}

func SaveKeyword(userId int64, keyword string) error {
	return db.Create(&Keyword{
		UserID:  userId,
		Keyword: keyword,
	}).Error
}

func RemoveKeyword(id uint, userId int64) error {
	return db.Delete(&Keyword{
		ID:     id,
		UserID: userId,
	}).Error
}
