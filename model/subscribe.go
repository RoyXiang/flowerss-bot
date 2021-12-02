package model

import (
	"errors"
	"strings"

	"github.com/indes/flowerss-bot/config"
	"gorm.io/gorm"
)

type Subscribe struct {
	ID                 uint  `gorm:"primary_key;AUTO_INCREMENT"`
	UserID             int64 `gorm:"index"`
	SourceID           uint  `gorm:"index"`
	EnableNotification int
	EnableTelegraph    int
	Tag                string
	Webhook            string
	Interval           int
	WaitTime           int
	EditTime
}

func (s *Subscribe) AfterDelete(tx *gorm.DB) (err error) {
	var count int64
	err = tx.Model(&Subscribe{}).Where("source_id = ?", s.SourceID).Count(&count).Error
	if err != nil {
		return
	}
	if count < 1 {
		err = tx.Delete(&Source{ID: s.SourceID}).Error
	}
	return
}

func RegistFeed(userID int64, feedUrl string) (source *Source, err error) {
	source, err = FindOrNewSourceByUrl(feedUrl)
	if err != nil {
		return
	}

	var subscribe Subscribe
	err = db.Where("user_id = ? and source_id = ?", userID, source.ID).First(&subscribe).Error
	if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
		return
	}

	subscribe.UserID = userID
	subscribe.SourceID = source.ID
	subscribe.EnableNotification = 1
	subscribe.EnableTelegraph = 1
	subscribe.Interval = config.UpdateInterval
	subscribe.WaitTime = config.UpdateInterval

	err = db.Create(&subscribe).Error
	return
}

func GetSubscribeByUserIDAndSourceID(userID int64, sourceID uint) (*Subscribe, error) {
	var sub Subscribe
	db.Where("user_id=? and source_id=?", userID, sourceID).First(&sub)
	if sub.UserID != int64(userID) {
		return nil, errors.New("未订阅该RSS源")
	}
	return &sub, nil
}

func GetSubscriberBySource(s *Source) []*Subscribe {
	if s == nil {
		return []*Subscribe{}
	}

	var subs []*Subscribe

	db.Where("source_id=?", s.ID).Find(&subs)
	return subs
}

func UnsubByUserIDAndSource(userID int64, source *Source) error {
	if source == nil {
		return errors.New("nil pointer")
	}

	var sub Subscribe
	db.Where("user_id=? and source_id=?", userID, source.ID).First(&sub)
	if sub.UserID != userID {
		return errors.New("未订阅该RSS源")
	}
	return db.Delete(&sub).Error
}

func UnsubByUserIDAndSubID(userID int64, subID uint) error {
	var sub Subscribe
	db.Where("id=?", subID).First(&sub)

	if sub.UserID != userID {
		return errors.New("未找到该条订阅")
	}
	return db.Delete(&sub).Error
}

func UnsubAllByUserID(userID int64) (success int, fail int, err error) {
	success = 0
	fail = 0
	var subs []Subscribe

	db.Where("user_id=?", userID).Find(&subs)

	for _, sub := range subs {
		err := sub.Unsub()
		if err != nil {
			fail += 1
		} else {
			success += 1
		}
	}
	err = nil

	return
}

func GetSubsByUserID(userID int64) ([]Subscribe, error) {
	var subs []Subscribe

	db.Where("user_id=?", userID).Find(&subs)

	return subs, nil
}

func GetSubscribeByID(id int) (*Subscribe, error) {
	var sub Subscribe
	err := db.Where("id=?  ", id).First(&sub).Error
	return &sub, err
}

func (s *Subscribe) ToggleNotification() error {
	if s.EnableNotification != 1 {
		s.EnableNotification = 1
	} else {
		s.EnableNotification = 0
	}
	return nil
}

func (s *Subscribe) ToggleTelegraph() error {
	if s.EnableTelegraph != 1 {
		s.EnableTelegraph = 1
	} else {
		s.EnableTelegraph = 0
	}
	return nil
}

func (s *Source) ToggleEnabled() error {
	if s.ErrorCount >= config.ErrorThreshold {
		s.ErrorCount = 0
	} else {
		s.ErrorCount = config.ErrorThreshold
	}

	///TODO a hack for save source changes
	s.Save()

	return nil
}

func (s *Subscribe) SetTag(tags []string) error {
	defer s.Save()

	tagStr := strings.Join(tags, " #")
	if tagStr != "" {
		s.Tag = "#" + tagStr
	} else {
		s.Tag = ""
	}

	return nil
}

func (s *Subscribe) SetWebhook(webhook string) error {
	defer s.Save()
	s.Webhook = webhook
	return nil
}

func (s *Subscribe) SetInterval(interval int) error {
	defer s.Save()
	s.Interval = interval
	return nil
}

func (s *Subscribe) Unsub() error {
	if s.ID == 0 {
		return errors.New("can't delete 0 subscribe")
	}

	return db.Delete(&s).Error
}

func (s *Subscribe) Save() {
	db.Save(&s)
}
