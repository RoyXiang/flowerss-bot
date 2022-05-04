package model

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/indes/flowerss-bot/internal/config"
	"github.com/indes/flowerss-bot/internal/tgraph"
	"github.com/indes/flowerss-bot/internal/util"

	"github.com/SlyMarbo/rss"
	parser "github.com/j-muller/go-torrent-parser"
	"gorm.io/gorm"
)

// Content fetcher content
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
	if c.TorrentUrl != "" {
		return c.RawID
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
		RawID:       item.ID,
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
	if torrentUrl == "" && strings.HasSuffix(c.RawLink, ".torrent") {
		torrentUrl = c.RawLink
	}
	for torrentUrl != "" {
		magnetLink := util.GetMagnetLink(c.RawID)
		if magnetLink != "" {
			c.RawID = magnetLink
			c.TorrentUrl = torrentUrl
			break
		}
		magnetLink = util.GetMagnetLink(torrentUrl)
		if magnetLink != "" {
			c.RawID = magnetLink
			c.TorrentUrl = torrentUrl
			break
		}
		infoHash := getTorrentInfoHash(torrentUrl)
		if infoHash != "" {
			c.RawID = fmt.Sprintf("%s%s", util.PrefixMagnet, infoHash)
			c.TorrentUrl = torrentUrl
		}
		break
	}

	return c, nil
}

// GenContentAndCheckByFeedItem generate content by fetcher item
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
	u, err := url.ParseRequestURI(torrentUrl)
	if err != nil {
		return
	}
	switch u.Hostname() {
	case "mikanani.me":
		base := filepath.Base(u.RawPath)
		if strings.HasSuffix(base, ".torrent") {
			infoHash = base[:len(base)-8]
			return
		}
	case "v2.uploadbt.com":
		infoHash = u.Query().Get("hash")
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
