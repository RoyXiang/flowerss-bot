package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/indes/flowerss-bot/config"
	"github.com/indes/flowerss-bot/model"
	"github.com/indes/flowerss-bot/util"
	"github.com/putdotio/go-putio/putio"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	tb "gopkg.in/tucnak/telebot.v2"
)

func getUserHtml(user, chat *tb.Chat, defaultText string) (text string) {
	if user.ID != chat.ID {
		if user.ID > 0 {
			text = fmt.Sprintf("<a href=\"tg://user?id=%d\">%s</a> ", user.ID, html.EscapeString(user.Title))
		} else {
			text = fmt.Sprintf("<a href=\"https://t.me/%s\">%s</a> ", user.Username, html.EscapeString(user.Title))
		}
	}
	if text != "" {
		if user.Type == tb.ChatChannel || user.Type == tb.ChatChannelPrivate {
			text = "频道 " + text
		} else if user.Type == tb.ChatGroup || user.Type == tb.ChatSuperGroup {
			text = "群组 " + text
		}
	}
	if text == "" {
		text = defaultText
	}
	return
}

func registerFeed(chat, user *tb.Chat, url string) {
	msg, err := B.Send(chat, "处理中...")

	source, err := model.FindOrNewSourceByUrl(url)
	if err != nil {
		_, _ = B.Edit(msg, fmt.Sprintf("%s，订阅失败", err))
		return
	}

	err = model.RegistFeed(user.ID, source.ID)
	zap.S().Infof("%d for %d subscribe [%d]%s %s", chat.ID, user.ID, source.ID, source.Title, source.Link)
	if err != nil {
		_, _ = B.Edit(msg, "订阅失败")
		return
	}

	newText := getUserHtml(user, chat, "")
	newText += fmt.Sprintf("订阅 <a href=\"%s\">%s</a> 成功", source.Link, source.Title)
	_, _ = B.Edit(msg, newText, &tb.SendOptions{
		DisableWebPagePreview: true,
		ParseMode:             tb.ModeHTML,
	})
}

//SendError send error user
func SendError(c *tb.Chat) {
	_, _ = B.Send(c, "请输入正确的指令！")
}

//BroadcastNews send new contents message to subscriber
func BroadcastNews(source *model.Source, subs []*model.Subscribe, contents []*model.Content) {
	zap.S().Infow("broadcast news",
		"feed id", source.ID,
		"feed title", source.Title,
		"subscriber count", len(subs),
		"new contents", len(contents),
	)

	for _, content := range contents {
		previewText := trimDescription(content.Description, config.PreviewText)

		for _, sub := range subs {
			tpldata := &config.TplData{
				SourceTitle:     source.Title,
				ContentTitle:    content.Title,
				RawLink:         content.RawLink,
				PreviewText:     previewText,
				TelegraphURL:    content.TelegraphURL,
				Tags:            sub.Tag,
				EnableTelegraph: sub.EnableTelegraph == 1 && content.TelegraphURL != "",
			}

			u := &tb.User{
				ID: int(sub.UserID),
			}

			history := &model.History{
				Type:      model.TelegramMessage,
				TriggerId: content.GetTriggerId(),
				TargetId:  strconv.FormatInt(sub.UserID, 10),
			}
			if history.IsSaved() {
				continue
			}

			o := &tb.SendOptions{
				DisableWebPagePreview: config.DisableWebPagePreview,
				ParseMode:             config.MessageMode,
				DisableNotification:   sub.EnableNotification != 1,
			}
			msg, err := tpldata.Render(config.MessageMode)
			if err != nil {
				zap.S().Errorw("broadcast news error, tpldata.Render err",
					"error", err.Error(),
				)
				return
			}
			if _, err := B.Send(u, msg, o); err != nil {

				if strings.Contains(err.Error(), "Forbidden") {
					zap.S().Errorw("broadcast news error, bot stopped by user",
						"error", err.Error(),
						"user id", sub.UserID,
						"source id", sub.SourceID,
						"title", source.Title,
						"link", source.Link,
					)
					sub.Unsub()
				}

				/*
					Telegram return error if markdown message has incomplete format.
					Print the msg to warn the user
					api error: Bad Request: can't parse entities: Can't find end of the entity starting at byte offset 894
				*/
				if strings.Contains(err.Error(), "parse entities") {
					zap.S().Errorw("broadcast news error, markdown error",
						"markdown msg", msg,
						"error", err.Error(),
					)
				}
			} else {
				history.Save()
			}
		}
	}
}

// BroadcastSourceError send feed updata error message to subscribers
func BroadcastSourceError(source *model.Source) {
	subs := model.GetSubscriberBySource(source)
	var u tb.User
	for _, sub := range subs {
		message := fmt.Sprintf("<a href=\"%s\">%s</a> 已经累计连续%d次更新失败，暂时停止更新", source.Link, html.EscapeString(source.Title), config.ErrorThreshold)
		u.ID = int(sub.UserID)
		_, _ = B.Send(&u, message, &tb.SendOptions{
			ParseMode: tb.ModeHTML,
		})
	}
}

type webhookBody struct {
	Title string   `json:"title"`
	Guid  string   `json:"guid"`
	Link  string   `json:"link"`
	Tags  []string `json:"tags"`
}

// SendWebhook send new contents to webhook
func SendWebhook(subs []*model.Subscribe, contents []*model.Content) {
	webhooks := make(map[string]struct{})
	exists := struct{}{}
	for _, sub := range subs {
		if sub.Webhook != "" {
			webhooks[sub.Webhook] = exists
		}
	}
	if len(webhooks) == 0 {
		return
	}

	for _, content := range contents {
		if content.TorrentUrl == "" {
			continue
		}

		body := webhookBody{
			Title: content.Title,
			Guid:  content.RawID,
			Link:  content.TorrentUrl,
		}

		for _, sub := range subs {
			if sub.Webhook == "" {
				continue
			}

			history := &model.History{
				Type:      model.Webhook,
				TriggerId: content.GetTriggerId(),
				TargetId:  sub.Webhook,
			}
			if history.IsSaved() {
				continue
			}

			if sub.Tag != "" {
				tags := strings.Split(sub.Tag, " ")
				body.Tags = make([]string, len(tags))
				for i, tag := range tags {
					body.Tags[i] = tag[1:]
				}
			} else {
				body.Tags = make([]string, 0)
			}
			if sendBodyToWebhook(body, sub.Webhook) {
				history.Save()
			}
		}
	}
}

func sendBodyToWebhook(body webhookBody, webhook string) bool {
	b := new(bytes.Buffer)
	err := json.NewEncoder(b).Encode(body)
	if err != nil {
		return false
	}

	req, err := http.NewRequest(http.MethodPost, webhook, b)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", config.UserAgent)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := util.HttpClient.Do(req)
	if err != nil {
		return false
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	return resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices
}

// CheckAdmin check user is admin of group/channel
func CheckAdmin(upd *tb.Update) bool {
	var msg *tb.Message
	if upd.Message != nil {
		msg = upd.Message
	} else if upd.Callback != nil {
		msg = upd.Callback.Message
	} else {
		return false
	}
	if !HasAdminType(msg.Chat.Type) {
		return true
	}
	err := isAdminOfChat(msg.Sender.ID, msg.Chat)
	if errors.Is(err, ErrBotNotChannelAdmin) {
		return true
	}
	return err == nil
}

// IsUserAllowed check user is allowed to use bot
func isUserAllowed(upd *tb.Update) bool {
	if upd == nil {
		return false
	}
	if len(config.AllowUsers) == 0 {
		return true
	}

	var userID int64
	if upd.Message != nil {
		userID = int64(upd.Message.Sender.ID)
	} else if upd.Callback != nil {
		userID = int64(upd.Callback.Sender.ID)
	} else {
		return false
	}

	for _, allowUserID := range config.AllowUsers {
		if allowUserID == userID {
			return true
		}
	}

	zap.S().Infow("user not allowed", "userID", userID)
	return false
}

// HasAdminType check if the message is sent in the group/channel environment
func HasAdminType(t tb.ChatType) bool {
	hasAdmin := []tb.ChatType{tb.ChatGroup, tb.ChatSuperGroup, tb.ChatChannel, tb.ChatChannelPrivate}
	for _, n := range hasAdmin {
		if t == n {
			return true
		}
	}
	return false
}

// GetMentionFromMessage get message mention
func GetMentionFromMessage(m *tb.Message) (mention string) {
	var entities []tb.MessageEntity
	if m.Text != "" {
		entities = m.Entities
	} else {
		entities = m.CaptionEntities
	}
	for _, entity := range entities {
		if entity.Type == tb.EntityMention {
			return m.Text[entity.Offset : entity.Offset+entity.Length]
		}
	}
	return
}

var relaxUrlMatcher = regexp.MustCompile(`^(https?://.*?)($| )`)

// GetURLAndMentionFromMessage get URL and mention from message
func GetURLAndMentionFromMessage(m *tb.Message) (url string, mention string) {
	for _, entity := range m.Entities {
		switch entity.Type {
		case tb.EntityMention:
			if mention == "" {
				mention = m.Text[entity.Offset : entity.Offset+entity.Length]
			}
		case tb.EntityURL:
			if url == "" {
				url = m.Text[entity.Offset : entity.Offset+entity.Length]
			}
		case tb.EntityTextLink:
			if url == "" {
				url = entity.URL
			}
		}
	}

	var payloadMatching = relaxUrlMatcher.FindStringSubmatch(m.Payload)
	if url == "" && len(payloadMatching) > 0 && payloadMatching[0] != "" {
		url = payloadMatching[0]
	}

	return
}

func NewPutIoClient(token string) *putio.Client {
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	oauthClient := oauth2.NewClient(context.Background(), tokenSource)
	return putio.NewClient(oauthClient)
}

func IsTorrentUrl(torrentUrl string) bool {
	_, err := url.ParseRequestURI(torrentUrl)
	if err != nil {
		return false
	}
	req, err := http.NewRequest(http.MethodHead, torrentUrl, nil)
	if err != nil {
		return false
	}
	resp, err := util.HttpClient.Do(req)
	if err != nil {
		return false
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return false
	}
	contentType := resp.Header.Get(util.ContentTypeHeader)
	return strings.HasPrefix(contentType, util.TorrentContentType)
}
