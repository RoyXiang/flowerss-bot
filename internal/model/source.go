package model

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"sort"
	"strings"
	"unicode"

	"github.com/indes/flowerss-bot/internal/config"
	"github.com/indes/flowerss-bot/internal/util"

	"github.com/SlyMarbo/rss"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type Source struct {
	ID         uint   `gorm:"primary_key;AUTO_INCREMENT"`
	Link       string `gorm:"uniqueIndex"`
	Title      string
	ErrorCount uint
	Content    []Content
	EditTime
}

func (s *Source) BeforeDelete(tx *gorm.DB) error {
	return tx.Where("source_id = ?", s.ID).Delete(Content{}).Error
}

func (s *Source) appendContents(items []*rss.Item) error {
	var contents []Content
	for _, item := range items {
		c, _ := getContentByFeedItem(s, item)
		if c.TorrentUrl != "" {
			return nil
		}
		contents = append(contents, c)
	}

	s.Content = contents
	// 开启task更新
	s.ErrorCount = 0
	if err := db.Save(&s).Error; err != nil {
		return err
	}
	return nil
}

func GetSourceByUrl(url string) (*Source, error) {
	var source Source
	if err := db.Where("link=?", url).First(&source).Error; err != nil {
		return nil, err
	}
	return &source, nil
}

func fetchFunc(url string) (resp *http.Response, err error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		zap.S().Fatal(err)
	}
	req.Header.Set("User-Agent", config.UserAgent)

	resp, err = util.HttpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	var data []byte
	if data, err = ioutil.ReadAll(resp.Body); err != nil {
		return nil, err
	}

	resp.Body = ioutil.NopCloser(strings.NewReader(strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, string(data))))
	return
}

func FindOrNewSourceByUrl(url string) (*Source, error) {
	var source Source

	err := db.Where("link = ?", url).First(&source).Error
	if err == nil {
		return &source, err
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// parsing task
	feed, err := rss.FetchByFunc(fetchFunc, url)
	if err != nil {
		return nil, fmt.Errorf("Feed 抓取错误 %v", err)
	}

	source.Title = feed.Title
	source.Link = url
	// 避免task更新
	source.ErrorCount = config.ErrorThreshold + 1

	// Get contents and insert
	items := feed.Items
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Date.Before(items[j].Date)
	})

	err = db.Create(&source).Error
	if err != nil {
		return nil, err
	}

	go func() {
		_ = source.appendContents(items)
	}()
	return &source, nil
}

func GetSources() (sources []*Source) {
	db.Find(&sources)
	return sources
}

func GetSubscribedNormalSources() []*Source {
	var subscribedSources []*Source
	sources := GetSources()
	for _, source := range sources {
		if source.IsSubscribed() && source.ErrorCount < config.ErrorThreshold {
			subscribedSources = append(subscribedSources, source)
		}
	}
	sort.SliceStable(subscribedSources, func(i, j int) bool {
		return subscribedSources[i].ID < subscribedSources[j].ID
	})
	return subscribedSources
}

func (s *Source) IsSubscribed() bool {
	var sub Subscribe
	db.Where("source_id=?", s.ID).FirstOrInit(&sub)
	return sub.SourceID == s.ID
}

func (s *Source) NeedUpdate() bool {
	var sub Subscribe
	db.Where("source_id=?", s.ID).First(&sub)
	sub.WaitTime += config.UpdateInterval
	if sub.Interval <= sub.WaitTime {
		sub.WaitTime = 0
		db.Save(&sub)
		return true
	} else {
		db.Save(&sub)
		return false
	}
}

// GetNewContents 获取rss新内容
func (s *Source) GetNewContents() ([]*Content, error) {
	zap.S().Debugw("fetch source updates",
		"source", s,
	)

	var newContents []*Content
	feed, err := rss.FetchByFunc(fetchFunc, s.Link)
	if err != nil {
		zap.S().Errorw("unable to fetch update", "error", err, "source", s)
		s.AddErrorCount()
		return nil, err
	}

	s.EraseErrorCount(feed)

	items := feed.Items
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Date.Before(items[j].Date)
	})
	for _, item := range items {
		c, isBroad, _ := GenContentAndCheckByFeedItem(s, item)
		if !isBroad {
			newContents = append(newContents, c)
		}
	}

	var firstContent Content
	shouldPublish := config.EnableTelegraph
	if err := db.Where("source_id=?", s.ID).First(&firstContent).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		if len(newContents) > 1 {
			shouldPublish = false
		}
	}
	for _, content := range newContents {
		if shouldPublish {
			content.Publish(s)
		}
		db.Create(content)
	}

	return newContents, nil
}

func GetSourcesByUserID(userID int64, page, limit int) (sources []Source, hasPrev, hasNext bool, err error) {
	var subs []Subscribe
	subs, hasPrev, hasNext, _ = GetSubsByUserIdByPage(userID, page, limit)

	for _, sub := range subs {
		var source Source
		db.Where("id=?", sub.SourceID).First(&source)
		if source.ID == sub.SourceID {
			sources = append(sources, source)
		}
	}
	err = nil
	return
}

func GetErrorSourcesByUserID(userID int64) ([]Source, error) {
	var sources []Source
	subs, err := GetSubsByUserID(userID)

	if err != nil {
		return nil, err
	}

	for _, sub := range subs {
		var source Source
		db.Where("id=?", sub.SourceID).First(&source)
		if source.ID == sub.SourceID && source.ErrorCount >= config.ErrorThreshold {
			sources = append(sources, source)
		}
	}

	sort.SliceStable(sources, func(i, j int) bool {
		return sources[i].ID < sources[j].ID
	})

	return sources, nil
}

func ActiveSourcesByUserID(userID int64) error {
	subs, err := GetSubsByUserID(userID)

	if err != nil {
		return err
	}

	for _, sub := range subs {
		var source Source
		db.Where("id=?", sub.SourceID).First(&source)
		if source.ID == sub.SourceID {
			source.ErrorCount = 0
			db.Save(&source)
		}
	}

	return nil
}

func PauseSourcesByUserID(userID int64) error {
	subs, err := GetSubsByUserID(userID)

	if err != nil {
		return err
	}

	for _, sub := range subs {
		var source Source
		db.Where("id=?", sub.SourceID).First(&source)
		if source.ID == sub.SourceID {
			source.ErrorCount = config.ErrorThreshold + 1
			db.Save(&source)
		}
	}

	return nil
}

func (s *Source) AddErrorCount() {
	s.ErrorCount++
	s.Save()
}

func (s *Source) EraseErrorCount(feed *rss.Feed) {
	s.Title = feed.Title
	s.ErrorCount = 0
	s.Save()
}

func (s *Source) Save() {
	db.Save(&s)
}

func GetSourceById(id uint) (*Source, error) {
	var source Source

	if err := db.Where("id=?", id).First(&source); err.Error != nil {
		return nil, errors.New("未找到 RSS 源")
	}

	return &source, nil
}
