package model

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/SlyMarbo/rss"
	"github.com/indes/flowerss-bot/config"
	"github.com/indes/flowerss-bot/tgraph"
	"github.com/indes/flowerss-bot/util"
	"gorm.io/gorm"

	parser "github.com/j-muller/go-torrent-parser"
)

const (
	httpPrefix         = "http"
	magnetPrefix       = "magnet:?xt=urn:btih:"
	magnetLength       = 60
	torrentContentType = "application/x-bittorrent"
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
	if c.TorrentUrl == "" {
		return c.HashID
	}
	if strings.HasPrefix(c.RawID, magnetPrefix) && len(c.RawID) == magnetLength {
		return c.RawID
	}
	return c.TorrentUrl
}

func (c *Content) Publish(source *Source) {
	if c.Description == "" || len([]rune(c.Description)) <= config.PreviewText {
		return
	}
	url, err := tgraph.PublishHtml(source.Title, c.Title, c.RawLink, c.Description)
	if err == nil {
		c.TelegraphURL = url
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

	var torrentUrl string
	for _, enclosure := range item.Enclosures {
		if enclosure.Type == torrentContentType {
			torrentUrl = enclosure.URL
			break
		}
	}

	if torrentUrl != "" {
		if strings.HasPrefix(c.RawID, magnetPrefix) && len(c.RawID) == magnetLength {
			c.TorrentUrl = torrentUrl
		} else if strings.HasPrefix(torrentUrl, httpPrefix) {
			infoHash := getTorrentInfoHash(torrentUrl)
			if infoHash != "" {
				c.RawID = fmt.Sprintf("%s%s", magnetPrefix, infoHash)
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

// DeleteContentsBySourceID delete contents in the db by sourceID
func DeleteContentsBySourceID(sid uint) {
	db.Delete(Content{}, "source_id = ?", sid)
}

func getTorrentInfoHash(torrentUrl string) (infoHash string) {
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
