package model

import (
	"errors"
	"strings"

	"github.com/SlyMarbo/rss"
	"github.com/indes/flowerss-bot/config"
	"github.com/indes/flowerss-bot/tgraph"
	"gorm.io/gorm"
)

const (
	magnetPrefix = "magnet:?xt=urn:btih:"
)

// Content feed content
type Content struct {
	SourceID     uint
	HashID       string `gorm:"primary_key"`
	RawID        string `gorm:"index"`
	RawLink      string
	Title        string
	Description  string `gorm:"-"` //ignore to db
	TelegraphURL string
	EditTime
}

func getContentByFeedItem(source *Source, item *rss.Item, isFirstTime bool) (Content, error) {
	TelegraphURL := ""

	html := item.Content
	if html == "" {
		html = item.Summary
	}

	html = strings.Replace(html, "<![CDATA[", "", -1)
	html = strings.Replace(html, "]]>", "", -1)

	if !isFirstTime && config.EnableTelegraph && len([]rune(html)) > config.PreviewText {
		TelegraphURL = PublishItem(source, item, html)
	}

	var c = Content{
		Title:        strings.Trim(item.Title, " "),
		Description:  html, //replace all kinds of <br> tag
		SourceID:     source.ID,
		RawID:        item.ID,
		HashID:       genHashID(source.Link, item.ID),
		TelegraphURL: TelegraphURL,
		RawLink:      item.Link,
	}

	return c, nil
}

// GenContentAndCheckByFeedItem generate content by feed item
func GenContentAndCheckByFeedItem(s *Source, item *rss.Item) (*Content, bool, error) {
	var (
		content   Content
		isBroaded bool
		err       error
	)

	if strings.HasPrefix(item.ID, magnetPrefix) {
		err = db.Where("raw_id=?", item.ID).First(&content).Error
	} else {
		hashID := genHashID(s.Link, item.ID)
		err = db.Where("hash_id=?", hashID).First(&content).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		isBroaded = false
		content, _ = getContentByFeedItem(s, item, false)
		db.Create(&content)
	} else {
		isBroaded = true
	}

	return &content, isBroaded, nil
}

// DeleteContentsBySourceID delete contents in the db by sourceID
func DeleteContentsBySourceID(sid uint) {
	db.Delete(Content{}, "source_id = ?", sid)
}

// PublishItem publish item to telegraph
func PublishItem(source *Source, item *rss.Item, html string) string {
	url, _ := tgraph.PublishHtml(source.Title, item.Title, item.Link, html)
	return url
}
