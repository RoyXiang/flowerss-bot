package model

import "errors"

// User subscriber
//
// TelegramID 用作外键
type User struct {
	ID         int64    `gorm:"primary_key"`
	TelegramID int64    `gorm:"uniqueIndex"`
	Source     []Source `gorm:"many2many:subscribes;"`
	State      int      `gorm:"DEFAULT:0;"`
	Token      string
	EditTime
}

// FindOrCreateUserByTelegramID find subscriber or init a subscriber by telegram ID
func FindOrCreateUserByTelegramID(telegramID int64) (*User, error) {
	var user User
	db.Where(User{TelegramID: telegramID}).FirstOrCreate(&user)

	return &user, nil
}

func SaveTokenByUserId(userId int64, token string) error {
	user, _ := FindOrCreateUserByTelegramID(userId)
	user.Token = token
	return db.Save(user).Error
}

// GetSubSourceMap get user subscribe and fetcher source
func (user *User) GetSubSourceMap() (map[Subscribe]Source, error) {
	m := make(map[Subscribe]Source)

	subs, err := GetSubsByUserID(user.TelegramID)
	if err != nil {
		return nil, errors.New("get subs error")
	}

	for _, sub := range subs {
		source, err := GetSourceById(sub.SourceID)
		if err != nil {
			continue
		}
		m[sub] = *source
	}

	return m, nil
}
