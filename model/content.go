package model

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/SlyMarbo/rss"
	"github.com/indes/flowerss-bot/config"
	"github.com/indes/flowerss-bot/tgraph"
	"github.com/indes/flowerss-bot/util"
	"gorm.io/gorm"

	parser "github.com/j-muller/go-torrent-parser"
)

// Content feed content
type Content struct {
	SourceID     uint
	HashID       string `gorm:"primary_key"`
	RawID        string `gorm:"index"`
	RawLink      string
	TorrentUrl   string
	Title        string
	Description  string `gorm:"-"` //ignore to db
	TelegraphURL string
	EditTime
}

func (c *Content) GetTriggerId() string {
	if strings.HasPrefix(c.RawID, util.PrefixMagnet) && len(c.RawID) == util.LengthMagnet {
		return c.RawID
	} else if c.TorrentUrl != "" {
		return c.TorrentUrl
	} else if c.RawID == c.RawLink {
		return c.RawID
	}
	return c.HashID
}

func (c *Content) Publish(source *Source) {
	if c.TelegraphURL != "" {
		return
	}
	if c.Description == "" || len([]rune(c.Description)) <= config.PreviewText {
		return
	}
	urlStr, err := tgraph.PublishHtml(source.Title, c.Title, c.RawLink, c.Description)
	if err == nil {
		c.TelegraphURL = urlStr
	}
}

func getContentByFeedItem(source *Source, item *rss.Item) (Content, error) {
	html := item.Content
	if html == "" {
		html = item.Summary
	}

	html = strings.Replace(html, "<![CDATA[", "", -1)
	html = strings.Replace(html, "]]>", "", -1)

	var c = Content{
		Title:       strings.Trim(item.Title, " "),
		Description: html, //replace all kinds of <br> tag
		SourceID:    source.ID,
		RawID:       strings.ToLower(item.ID),
		HashID:      genHashID(source.Link, item.ID),
		RawLink:     item.Link,
	}

	if strings.HasPrefix(c.RawLink, util.PrefixInstantView) {
		u, err := url.Parse(c.RawLink)
		if err == nil {
			query := u.Query()
			originalUrl := query.Get("url")
			if originalUrl != "" && query.Get("rhash") != "" {
				c.TelegraphURL = c.RawLink
				c.RawLink = originalUrl
				if c.RawID == c.TelegraphURL {
					c.RawID = originalUrl
				}
			}
		}
	}

	var torrentUrl string
	for _, enclosure := range item.Enclosures {
		if enclosure.Type == util.ContentTypeTorrent {
			torrentUrl = enclosure.URL
			break
		}
	}

	if torrentUrl != "" {
		if strings.HasPrefix(c.RawID, util.PrefixMagnet) && len(c.RawID) == util.LengthMagnet {
			c.TorrentUrl = torrentUrl
		} else {
			infoHash := getTorrentInfoHash(torrentUrl)
			if infoHash != "" {
				c.RawID = fmt.Sprintf("%s%s", util.PrefixMagnet, infoHash)
				c.TorrentUrl = torrentUrl
			}
		}
	}

	return c, nil
}

// GenContentAndCheckByFeedItem generate content by feed item
func GenContentAndCheckByFeedItem(s *Source, item *rss.Item) (*Content, bool, error) {
	var (
		content   Content
		isBroaded bool
	)

	hashID := genHashID(s.Link, item.ID)
	err := db.Where("hash_id=?", hashID).First(&content).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		isBroaded = false
		content, _ = getContentByFeedItem(s, item)
	} else {
		isBroaded = true
	}

	return &content, isBroaded, nil
}

func getTorrentInfoHash(torrentUrl string) (infoHash string) {
	_, err := url.ParseRequestURI(torrentUrl)
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodGet, torrentUrl, nil)
	if err != nil {
		return
	}
	resp, err := util.HttpClient.Do(req)
	if err != nil {
		return
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return
	}
	torrent, err := parser.Parse(resp.Body)
	if err == nil {
		infoHash = torrent.InfoHash
	}
	return
}
