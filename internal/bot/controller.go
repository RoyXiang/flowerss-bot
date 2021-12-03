package bot

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"html/template"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/indes/flowerss-bot/internal/bot/fsm"
	"github.com/indes/flowerss-bot/internal/config"
	"github.com/indes/flowerss-bot/internal/model"
	"github.com/indes/flowerss-bot/internal/util"

	"go.uber.org/zap"
	tb "gopkg.in/tucnak/telebot.v2"
)

const (
	actionToggleNotice    = "toggleNotice"
	actionToggleTelegraph = "toggleTelegraph"
	actionToggleDownload  = "toggleDownload"
	actionToggleUpdate    = "toggleUpdate"
	limitPerPage          = 10
)

var (
	feedSettingTmpl = `
订阅<b>设置</b>
[id] {{ .sub.ID }}
[标题] <a href="{{.source.Link }}">{{ .source.Title }}</a>
[抓取更新] {{if ge .source.ErrorCount .Count }}暂停{{else if lt .source.ErrorCount .Count }}抓取中{{end}}
[抓取频率] {{ .sub.Interval }}分钟
[通知] {{if eq .sub.EnableNotification 0}}关闭{{else if eq .sub.EnableNotification 1}}开启{{end}}
[下载任务] {{if eq .sub.EnableDownload 0}}关闭{{else if eq .sub.EnableDownload 1}}开启{{end}}
[Telegraph] {{if eq .sub.EnableTelegraph 0}}关闭{{else if eq .sub.EnableTelegraph 1}}开启{{end}}
[Tag] {{if .sub.Tag}}{{ .sub.Tag }}{{else}}无{{end}}
{{if .sub.Webhook}}[Webhook] {{ .sub.Webhook }}{{end}}
`
)

func toggleCtrlButtons(c *tb.Callback, action string) {
	data := strings.Split(c.Data, ":")
	if len(data) < 2 {
		_, _ = B.Edit(c.Message, "内部错误：回调数据不正确")
		return
	}

	user, err := getMentionedUser(c.Message, data[0], c.Sender)
	if err != nil {
		_ = B.Respond(c, &tb.CallbackResponse{
			Text: err.Error(),
		})
		return
	}

	msg := strings.Split(c.Message.Text, "\n")
	subID, err := strconv.Atoi(strings.Split(msg[1], " ")[1])
	if err != nil {
		_ = B.Respond(c, &tb.CallbackResponse{
			Text: "error",
		})
		return
	}
	sub, err := model.GetSubscribeByID(subID)
	if sub == nil || err != nil {
		_ = B.Respond(c, &tb.CallbackResponse{
			Text: "error",
		})
		return
	}

	source, _ := model.GetSourceById(sub.SourceID)
	t := template.New("setting template")
	_, _ = t.Parse(feedSettingTmpl)

	switch action {
	case actionToggleNotice:
		err = sub.ToggleNotification()
	case actionToggleTelegraph:
		err = sub.ToggleTelegraph()
	case actionToggleDownload:
		err = sub.ToggleDownload()
		if sub.EnableDownload == 1 {
			user, err := model.FindOrCreateUserByTelegramID(sub.UserID)
			if err != nil || user.Token == "" {
				_ = B.Respond(c, &tb.CallbackResponse{
					Text: "请先通过 /set_token 设置Put.io的token",
				})
				return
			}
		}
	case actionToggleUpdate:
		err = source.ToggleEnabled()
	}

	if err != nil {
		_ = B.Respond(c, &tb.CallbackResponse{
			Text: "error",
		})
		return
	}
	sub.Save()

	text := new(bytes.Buffer)
	_ = t.Execute(text, map[string]interface{}{
		"source": source,
		"sub":    sub,
		"Count":  config.ErrorThreshold,
	})
	_ = B.Respond(c, &tb.CallbackResponse{
		Text: "修改成功",
	})

	textStr := fmt.Sprintf("%s%s", getUserHtml(user, c.Message.Chat, ""), strings.TrimSpace(text.String()))
	_, _ = B.Edit(c.Message, textStr, &tb.SendOptions{
		DisableWebPagePreview: true,
		ParseMode:             tb.ModeHTML,
	}, &tb.ReplyMarkup{
		InlineKeyboard: genFeedSetBtn(c.Data, sub, source),
	})
}

func startCmdCtr(m *tb.Message) {
	user, _ := model.FindOrCreateUserByTelegramID(m.Chat.ID)
	zap.S().Infof("/start user_id: %d telegram_id: %d", user.ID, user.TelegramID)
	_, _ = B.Reply(m, fmt.Sprintf("你好，欢迎使用flowerss。"))
}

func subCmdCtr(m *tb.Message) {
	url, mention := GetURLAndMentionFromMessage(m)
	user, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Reply(m, err.Error())
		return
	}

	if url == "" {
		if user.ID == m.Chat.ID {
			_, err := B.Reply(m, "请回复RSS URL", &tb.ReplyMarkup{ForceReply: true})
			if err == nil {
				UserState[m.Chat.ID] = fsm.Sub
			}
		} else {
			_, _ = B.Reply(m, "频道订阅请使用' /sub @ChannelID URL ' 命令")
		}
		return
	}

	registerFeed(m, user, url)
}

func exportCmdCtr(m *tb.Message) {
	mention := GetMentionFromMessage(m)
	user, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Reply(m, err.Error())
		return
	}
	sourceList, _, _, err := model.GetSourcesByUserID(user.ID, 0, 0)
	if err != nil {
		zap.S().Errorf(err.Error())
		_, _ = B.Reply(m, fmt.Sprintf("导出失败"))
		return
	}

	if len(sourceList) == 0 {
		_, _ = B.Reply(m, fmt.Sprintf("订阅列表为空"))
		return
	}

	opmlStr, err := ToOPML(sourceList)

	if err != nil {
		_, _ = B.Reply(m, fmt.Sprintf("导出失败"))
		return
	}
	opmlFile := &tb.Document{File: tb.FromReader(strings.NewReader(opmlStr))}
	opmlFile.FileName = fmt.Sprintf("subscriptions_%d.opml", time.Now().Unix())
	_, err = B.Reply(m, opmlFile)

	if err != nil {
		_, _ = B.Reply(m, fmt.Sprintf("导出失败"))
		zap.S().Errorf("send opml file failed, err:%+v", err)
	}
}

func listCmdCtr(m *tb.Message) {
	mention := GetMentionFromMessage(m)
	chat, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Reply(m, err.Error())
		return
	}

	user, err := model.FindOrCreateUserByTelegramID(chat.ID)
	if err != nil {
		_, _ = B.Reply(m, "内部错误：无法找到对应的用户")
		return
	}
	subSourceMap, err := user.GetSubSourceMap()
	if err != nil {
		_, _ = B.Reply(m, "内部错误：无法查询用户订阅列表")
		return
	}

	rspMessage := getUserHtml(chat, m.Chat, "当前")
	if len(subSourceMap) == 0 {
		rspMessage += "订阅列表为空"
	} else {
		rspMessage += "订阅列表：\n"
		var subs []model.Subscribe
		for sub, _ := range subSourceMap {
			subs = append(subs, sub)
		}
		sort.SliceStable(subs, func(i, j int) bool {
			return subs[i].ID < subs[j].ID
		})
		for _, sub := range subs {
			source := subSourceMap[sub]
			rspMessage += fmt.Sprintf("[%d] <a href=\"%s\">%s</a>\n", sub.ID, source.Link, html.EscapeString(source.Title))
		}
	}
	_, _ = B.Reply(m, rspMessage, &tb.SendOptions{
		DisableWebPagePreview: true,
		ParseMode:             tb.ModeHTML,
	})
}

func checkCmdCtr(m *tb.Message) {
	mention := GetMentionFromMessage(m)
	user, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Reply(m, err.Error())
		return
	}
	sources, _ := model.GetErrorSourcesByUserID(user.ID)
	message := getUserHtml(user, m.Chat, "")
	if len(sources) > 0 {
		message += "失效订阅的列表：\n"
		for _, source := range sources {
			message += fmt.Sprintf("[%d] <a href=\"%s\">%s</a>\n", source.ID, source.Link, html.EscapeString(source.Title))
		}
	} else {
		message += "所有订阅正常"
	}
	_, _ = B.Reply(m, message, &tb.SendOptions{
		DisableWebPagePreview: true,
		ParseMode:             tb.ModeHTML,
	})
}

func setCmdCtr(m *tb.Message) {
	mention := GetMentionFromMessage(m)
	user, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Reply(m, err.Error())
		return
	}

	msg, _ := B.Reply(m, "处理中...")
	setFeedItemsCurrentPage(msg, user, 1)
}

func setFeedItemPageCtr(c *tb.Callback) {
	data := strings.Split(c.Data, ":")
	if len(data) < 2 {
		_, _ = B.Edit(c.Message, "内部错误：回调数据不正确")
		return
	}

	user, err := getMentionedUser(c.Message, data[0], c.Sender)
	if err != nil {
		_ = B.Respond(c, &tb.CallbackResponse{
			Text: err.Error(),
		})
		return
	}

	page, _ := strconv.Atoi(data[1])
	setFeedItemsCurrentPage(c.Message, user, page)
}

func setFeedItemsCurrentPage(m *tb.Message, user *tb.Chat, page int) {
	sources, hasPrev, hasNext, _ := model.GetSourcesByUserID(user.ID, page, limitPerPage)

	var setFeedItemBtns [][]tb.InlineButton
	// 配置按钮
	for _, source := range sources {
		// 添加按钮
		setFeedItemBtns = append(setFeedItemBtns, []tb.InlineButton{{
			Unique: "set_feed_item_btn",
			Text:   fmt.Sprintf("[%d] %s", source.ID, source.Title),
			Data:   fmt.Sprintf("%d:%d:%d", user.ID, source.ID, page),
		}})
	}

	var lastRow []tb.InlineButton
	if hasPrev {
		lastRow = append(lastRow, tb.InlineButton{
			Unique: "set_feed_item_page",
			Text:   "上一页",
			Data:   fmt.Sprintf("%d:%d", user.ID, page-1),
		})
	}
	lastRow = append(lastRow, tb.InlineButton{
		Unique: "cancel_btn",
		Text:   "取消",
	})
	if hasNext {
		lastRow = append(lastRow, tb.InlineButton{
			Unique: "set_feed_item_page",
			Text:   "下一页",
			Data:   fmt.Sprintf("%d:%d", user.ID, page+1),
		})
	}
	setFeedItemBtns = append(setFeedItemBtns, lastRow)

	_, _ = B.Edit(m, "请选择你要设置的源", &tb.ReplyMarkup{
		InlineKeyboard: setFeedItemBtns,
	})
}

func setFeedItemBtnCtr(c *tb.Callback) {
	data := strings.Split(c.Data, ":")
	if len(data) < 2 {
		_, _ = B.Edit(c.Message, "内部错误：回调数据不正确")
		return
	}

	user, err := getMentionedUser(c.Message, data[0], c.Sender)
	if err != nil {
		_ = B.Respond(c, &tb.CallbackResponse{
			Text: err.Error(),
		})
		return
	}

	sourceID, _ := strconv.Atoi(data[1])
	source, err := model.GetSourceById(uint(sourceID))
	if err != nil {
		_, _ = B.Edit(c.Message, "找不到该订阅源，错误代码01。")
		return
	}

	sub, err := model.GetSubscribeByUserIDAndSourceID(user.ID, source.ID)
	if err != nil {
		_, _ = B.Edit(c.Message, "用户未订阅该rss，错误代码02。")
		return
	}

	t := template.New("setting template")
	_, _ = t.Parse(feedSettingTmpl)
	text := new(bytes.Buffer)
	_ = t.Execute(text, map[string]interface{}{
		"source": source,
		"sub":    sub,
		"Count":  config.ErrorThreshold,
	})

	textStr := fmt.Sprintf("%s%s", getUserHtml(user, c.Message.Chat, ""), strings.TrimSpace(text.String()))
	_, _ = B.Edit(c.Message, textStr, &tb.SendOptions{
		DisableWebPagePreview: true,
		ParseMode:             tb.ModeHTML,
	}, &tb.ReplyMarkup{
		InlineKeyboard: genFeedSetBtn(c.Data, sub, source),
	})
}

func setSubTagBtnCtr(c *tb.Callback) {
	data := strings.Split(c.Data, ":")
	if len(data) < 2 {
		_ = B.Respond(c, &tb.CallbackResponse{Text: "内部错误：回调数据不正确"})
		return
	}

	user, err := getMentionedUser(c.Message, data[0], c.Sender)
	if err != nil {
		_ = B.Respond(c, &tb.CallbackResponse{
			Text: err.Error(),
		})
		return
	}

	sourceID, _ := strconv.Atoi(data[1])
	sub, err := model.GetSubscribeByUserIDAndSourceID(user.ID, uint(sourceID))
	if err != nil {
		_ = B.Respond(c, &tb.CallbackResponse{Text: "系统错误，代码04"})
		return
	}

	msg := fmt.Sprintf(
		"请使用`/set_feed_tag %d tags`命令为该订阅设置标签，tags为需要设置的标签，以空格分隔。（最多设置三个标签） \n"+
			"例如：`/set_feed_tag %d 科技 苹果`",
		sub.ID, sub.ID)
	_, _ = B.Edit(c.Message, msg, &tb.SendOptions{ParseMode: tb.ModeMarkdown})
}

func genFeedSetBtn(data string, sub *model.Subscribe, source *model.Source) [][]tb.InlineButton {
	setSubTagKey := tb.InlineButton{
		Unique: "set_set_sub_tag_btn",
		Text:   "标签设置",
		Data:   data,
	}

	toggleDownloadKey := tb.InlineButton{
		Unique: "set_toggle_download_btn",
		Text:   "开启下载",
		Data:   data,
	}
	if sub.EnableDownload == 1 {
		toggleDownloadKey.Text = "关闭下载"
	}

	toggleNoticeKey := tb.InlineButton{
		Unique: "set_toggle_notice_btn",
		Text:   "开启通知",
		Data:   data,
	}
	if sub.EnableNotification == 1 {
		toggleNoticeKey.Text = "关闭通知"
	}

	toggleTelegraphKey := tb.InlineButton{
		Unique: "set_toggle_telegraph_btn",
		Text:   "开启 Telegraph 转码",
		Data:   data,
	}
	if sub.EnableTelegraph == 1 {
		toggleTelegraphKey.Text = "关闭 Telegraph 转码"
	}

	toggleEnabledKey := tb.InlineButton{
		Unique: "set_toggle_update_btn",
		Text:   "暂停更新",
		Data:   data,
	}
	if source.ErrorCount >= config.ErrorThreshold {
		toggleEnabledKey.Text = "重启更新"
	}

	parts := strings.Split(data, ":")
	if len(parts) < 3 {
		parts = append(parts, "1")
	}
	backKey := tb.InlineButton{
		Unique: "set_feed_item_page",
		Text:   "返回",
		Data:   fmt.Sprintf("%s:%s", parts[0], parts[2]),
	}

	feedSettingKeys := [][]tb.InlineButton{
		{
			toggleEnabledKey,
			toggleNoticeKey,
		},
		{
			toggleTelegraphKey,
			setSubTagKey,
		},
		{
			toggleDownloadKey,
			backKey,
		},
	}
	return feedSettingKeys
}

func setToggleNoticeBtnCtr(c *tb.Callback) {
	toggleCtrlButtons(c, actionToggleNotice)
}

func setToggleTelegraphBtnCtr(c *tb.Callback) {
	toggleCtrlButtons(c, actionToggleTelegraph)
}

func setToggleDownloadBtnCtr(c *tb.Callback) {
	toggleCtrlButtons(c, actionToggleDownload)
}

func setToggleUpdateBtnCtr(c *tb.Callback) {
	toggleCtrlButtons(c, actionToggleUpdate)
}

func unsubCmdCtr(m *tb.Message) {
	url, mention := GetURLAndMentionFromMessage(m)
	user, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Reply(m, err.Error())
		return
	}

	if url != "" {
		source, err := model.GetSourceByUrl(url)
		if err != nil {
			_, _ = B.Reply(m, "未订阅该RSS源")
			return
		}
		err = model.UnsubByUserIDAndSource(user.ID, source)
		if err != nil {
			_, _ = B.Reply(m, fmt.Sprintf("退订失败：%s", err.Error()))
			return
		}
		text := getUserHtml(user, m.Chat, "")
		text += fmt.Sprintf("退订 <a href=\"%s\">%s</a> 成功！", source.Link, html.EscapeString(source.Title))
		_, _ = B.Reply(m, text, &tb.SendOptions{
			DisableWebPagePreview: true,
			ParseMode:             tb.ModeHTML,
		})
		zap.S().Infof("%d unsubscribe [%d]%s %s", user.ID, source.ID, source.Title, source.Link)
	} else {
		msg, _ := B.Reply(m, "处理中...")
		unsubFeedItemsCurrentPage(msg, user, 1)
	}
}

func unsubFeedItemPageCtr(c *tb.Callback) {
	data := strings.Split(c.Data, ":")
	if len(data) != 2 {
		_, _ = B.Edit(c.Message, "内部错误：回调数据不正确")
		return
	}

	user, err := getMentionedUser(c.Message, data[0], c.Sender)
	if err != nil {
		_ = B.Respond(c, &tb.CallbackResponse{
			Text: err.Error(),
		})
		return
	}

	page, _ := strconv.Atoi(data[1])
	unsubFeedItemsCurrentPage(c.Message, user, page)
}

func unsubFeedItemsCurrentPage(m *tb.Message, user *tb.Chat, page int) {
	subs, hasPrev, hasNext, _ := model.GetSubsByUserIdByPage(user.ID, page, limitPerPage)

	var unsubFeedItemBtns [][]tb.InlineButton
	for _, sub := range subs {
		source, err := model.GetSourceById(sub.SourceID)
		if err != nil {
			continue
		}

		unsubFeedItemBtns = append(unsubFeedItemBtns, []tb.InlineButton{{
			Unique: "unsub_feed_item_btn",
			Text:   fmt.Sprintf("[%d] %s", sub.SourceID, source.Title),
			Data:   fmt.Sprintf("%d:%d:%d", sub.UserID, sub.ID, source.ID),
		}})
	}

	var lastRow []tb.InlineButton
	if hasPrev {
		lastRow = append(lastRow, tb.InlineButton{
			Unique: "unsub_feed_item_page",
			Text:   "上一页",
			Data:   fmt.Sprintf("%d:%d", user.ID, page-1),
		})
	}
	lastRow = append(lastRow, tb.InlineButton{
		Unique: "cancel_btn",
		Text:   "取消",
	})
	if hasNext {
		lastRow = append(lastRow, tb.InlineButton{
			Unique: "unsub_feed_item_page",
			Text:   "下一页",
			Data:   fmt.Sprintf("%d:%d", user.ID, page+1),
		})
	}
	unsubFeedItemBtns = append(unsubFeedItemBtns, lastRow)

	_, _ = B.Edit(m, "请选择你要退订的源", &tb.ReplyMarkup{
		InlineKeyboard: unsubFeedItemBtns,
	})
}

func unsubFeedItemBtnCtr(c *tb.Callback) {
	data := strings.Split(c.Data, ":")
	if len(data) != 3 {
		_, _ = B.Edit(c.Message, "内部错误：回调数据不正确")
		return
	}
	user, err := getMentionedUser(c.Message, data[0], c.Sender)
	if err != nil {
		_ = B.Respond(c, &tb.CallbackResponse{
			Text: err.Error(),
		})
		return
	}
	sourceID, _ := strconv.Atoi(data[2])
	source, err := model.GetSourceById(uint(sourceID))
	if err != nil {
		_, _ = B.Edit(c.Message, "未订阅该RSS源")
		return
	}
	subID, _ := strconv.Atoi(data[1])
	err = model.UnsubByUserIDAndSubID(user.ID, uint(subID))
	if err != nil {
		_, _ = B.Edit(c.Message, fmt.Sprintf("退订失败：%s", err.Error()))
		return
	}
	text := getUserHtml(user, c.Message.Chat, "")
	text += fmt.Sprintf("退订 <a href=\"%s\">%s</a> 成功！", source.Link, html.EscapeString(source.Title))
	_, _ = B.Edit(c.Message, text, &tb.SendOptions{
		DisableWebPagePreview: true,
		ParseMode:             tb.ModeHTML,
	})
	zap.S().Infof("%d unsubscribe [%d]%s %s", user.ID, source.ID, source.Title, source.Link)
}

func unsubAllCmdCtr(m *tb.Message) {
	mention := GetMentionFromMessage(m)
	user, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Reply(m, err.Error())
		return
	}

	var confirmKeys [][]tb.InlineButton
	confirmKeys = append(confirmKeys, []tb.InlineButton{
		{
			Unique: "unsub_all_confirm_btn",
			Text:   "确认",
			Data:   strconv.FormatInt(user.ID, 10),
		},
		{
			Unique: "unsub_all_cancel_btn",
			Text:   "取消",
		},
	})

	msg := fmt.Sprintf("是否退订%s的所有订阅？", getUserHtml(user, m.Chat, "当前用户"))
	_, _ = B.Reply(m, msg, &tb.SendOptions{
		DisableWebPagePreview: true,
		ParseMode:             tb.ModeHTML,
	}, &tb.ReplyMarkup{
		InlineKeyboard: confirmKeys,
	})
}

func cancelBtnCtr(c *tb.Callback) {
	_, _ = B.Edit(c.Message, "操作已取消。")
	UserState[c.Message.Chat.ID] = fsm.None
}

func cancelCmdCtr(m *tb.Message) {
	_, _ = B.Reply(m, "当前操作已取消。", &tb.ReplyMarkup{
		ReplyKeyboardRemove: true,
	})
	UserState[m.Chat.ID] = fsm.None
}

func unsubAllConfirmBtnCtr(c *tb.Callback) {
	if c.Data == "" {
		_, _ = B.Edit(c.Message, "内部错误：回调内容不正确")
		return
	}
	user, err := getMentionedUser(c.Message, c.Data, c.Sender)
	if err != nil {
		_ = B.Respond(c, &tb.CallbackResponse{
			Text: err.Error(),
		})
		return
	}

	var msg string
	success, fail, err := model.UnsubAllByUserID(user.ID)
	if err != nil {
		msg = "退订失败"
	} else {
		msg = fmt.Sprintf("退订成功：%d\n退订失败：%d", success, fail)
	}
	_, _ = B.Edit(c.Message, msg)
}

func pingCmdCtr(m *tb.Message) {
	_, _ = B.Reply(m, "pong")
	zap.S().Debugw(
		"pong",
		"telegram msg", m,
	)
}

func helpCmdCtr(m *tb.Message) {
	message := `
命令：
/sub 订阅源
/unsub  取消订阅
/list 查看当前订阅源
/set 设置订阅
/check 检查当前订阅
/set_feed_tag 设置订阅标签
/set_interval 设置订阅刷新频率
/active_all 开启所有订阅
/pause_all 暂停所有订阅
/help 帮助
/import 导入 OPML 文件
/export 导出 OPML 文件
/unsub_all 取消所有订阅
详细使用方法请看：https://github.com/indes/flowerss-bot
`

	_, _ = B.Reply(m, message)
}

func versionCmdCtr(m *tb.Message) {
	_, _ = B.Reply(m, config.AppVersionInfo())
}

func importCmdCtr(m *tb.Message) {
	message := `请直接发送OPML文件，
如果需要为channel导入OPML，请在发送文件的时候附上channel id，例如@telegram
`
	_, _ = B.Reply(m, message)
}

func setFeedTagCmdCtr(m *tb.Message) {
	args := strings.Split(m.Payload, " ")
	if len(args) < 1 {
		_, _ = B.Reply(m, "/set_feed_tag [sub id] [tag1] [tag2] 设置订阅标签（最多设置三个Tag，以空格分割）")
		return
	}

	mention := GetMentionFromMessage(m)
	if mention != "" {
		args = args[1:]
	}
	// 截短参数
	if len(args) > 4 {
		args = args[:4]
	}
	subID, err := strconv.Atoi(args[0])
	if err != nil {
		_, _ = B.Reply(m, "请输入正确的订阅ID！")
		return
	}
	sub, err := model.GetSubscribeByID(subID)
	if err != nil {
		_, _ = B.Reply(m, "请输入正确的订阅ID！")
		return
	}
	_, err = getMentionedUser(m, strconv.FormatInt(sub.UserID, 10), nil)
	if err != nil {
		_, _ = B.Reply(m, err.Error())
		return
	}

	err = sub.SetTag(args[1:])
	if err != nil {
		_, _ = B.Reply(m, "订阅标签设置失败！")
		return
	}
	_, _ = B.Reply(m, "订阅标签设置成功！")
}

func setWebhookCmdCtr(m *tb.Message) {
	args := strings.Split(m.Payload, " ")
	if len(args) < 1 {
		_, _ = B.Reply(m, "/set_webhook [sub id] [webhook]")
		return
	}

	url, mention := GetURLAndMentionFromMessage(m)
	if mention != "" {
		args = args[1:]
	}
	subID, err := strconv.Atoi(args[0])
	if err != nil {
		_, _ = B.Reply(m, "请输入正确的订阅ID！")
		return
	}
	sub, err := model.GetSubscribeByID(subID)
	if err != nil {
		_, _ = B.Reply(m, "请输入正确的订阅ID！")
		return
	}
	_, err = getMentionedUser(m, strconv.FormatInt(sub.UserID, 10), nil)
	if err != nil {
		_, _ = B.Reply(m, err.Error())
		return
	}

	err = sub.SetWebhook(url)
	if err != nil {
		_, _ = B.Reply(m, "订阅webhook设置失败！")
		return
	}
	_, _ = B.Reply(m, "订阅webhook设置成功！")
}

func setTokenCmdCtr(m *tb.Message) {
	args := strings.Split(m.Payload, " ")
	mention := GetMentionFromMessage(m)
	if mention != "" {
		args = args[1:]
	}
	if len(args) != 1 {
		_, _ = B.Reply(m, "/set_token [token] 设置Put.io的token")
		return
	}
	token := args[0]

	user, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Reply(m, err.Error())
		return
	}

	client := NewPutIoClient(token)
	info, err := client.Account.Info(context.Background())
	if err != nil {
		_, _ = B.Reply(m, "无效的token")
		return
	}
	text := getUserHtml(user, m.Chat, "")
	err = model.SaveTokenByUserId(user.ID, token)
	if err != nil {
		_, _ = B.Reply(m, fmt.Sprintf("%s保存token失败", text))
		return
	}
	_, _ = B.Reply(m, fmt.Sprintf("%s成功保存了 %s 的 token", text, info.Username), &tb.SendOptions{
		DisableWebPagePreview: true,
		ParseMode:             tb.ModeHTML,
	})
}

func setIntervalCmdCtr(m *tb.Message) {
	args := strings.Split(m.Payload, " ")
	if len(args) < 1 {
		_, _ = B.Reply(m, "/set_interval [interval] [sub id] 设置订阅刷新频率（可设置多个sub id，以空格分割）")
		return
	}

	interval, err := strconv.Atoi(args[0])
	if interval <= 0 || err != nil {
		_, _ = B.Reply(m, "请输入正确的抓取频率")
		return
	}

	var success, failed, wrong int
	for _, id := range args[1:] {
		subID, err := strconv.Atoi(id)
		if err != nil {
			wrong++
			continue
		}
		sub, err := model.GetSubscribeByID(subID)
		if err != nil || sub == nil {
			wrong++
			continue
		}
		_, err = getMentionedUser(m, strconv.FormatInt(sub.UserID, 10), nil)
		if err != nil {
			wrong++
			continue
		}
		err = sub.SetInterval(interval)
		if err != nil {
			failed++
		} else {
			success++
		}
	}
	_, _ = B.Reply(m, fmt.Sprintf("抓取频率设置成功%d个，失败%d个，错误%d个！", success, failed, wrong))
}

func activeAllCmdCtr(m *tb.Message) {
	mention := GetMentionFromMessage(m)
	user, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Reply(m, err.Error())
		return
	}
	_ = model.ActiveSourcesByUserID(user.ID)
	message := fmt.Sprintf("%s订阅已全部开启", getUserHtml(user, m.Chat, ""))
	_, _ = B.Reply(m, message, &tb.SendOptions{
		DisableWebPagePreview: true,
		ParseMode:             tb.ModeHTML,
	})
}

func pauseAllCmdCtr(m *tb.Message) {
	mention := GetMentionFromMessage(m)
	user, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Reply(m, err.Error())
		return
	}
	_ = model.PauseSourcesByUserID(user.ID)
	message := fmt.Sprintf("%s订阅已全部暂停", getUserHtml(user, m.Chat, ""))
	_, _ = B.Reply(m, message, &tb.SendOptions{
		DisableWebPagePreview: true,
		ParseMode:             tb.ModeHTML,
	})
}

func textCtr(m *tb.Message) {
	switch UserState[m.Chat.ID] {
	case fsm.UnSub:
		{
			str := strings.Split(m.Text, " ")

			if len(str) < 2 && (strings.HasPrefix(str[0], "[") && strings.HasSuffix(str[0], "]")) {
				_, _ = B.Reply(m, "请选择正确的指令！")
			} else {

				var sourceID uint
				if _, err := fmt.Sscanf(str[0], "[%d]", &sourceID); err != nil {
					_, _ = B.Reply(m, "请选择正确的指令！")
					return
				}

				source, err := model.GetSourceById(sourceID)

				if err != nil {
					_, _ = B.Reply(m, "请选择正确的指令！")
					return
				}

				err = model.UnsubByUserIDAndSource(m.Chat.ID, source)

				if err != nil {
					_, _ = B.Reply(m, "请选择正确的指令！")
					return
				}

				_, _ = B.Reply(
					m,
					fmt.Sprintf("<a href=\"%s\">%s</a> 退订成功", source.Link, html.EscapeString(source.Title)),
					&tb.SendOptions{
						ParseMode: tb.ModeHTML,
					}, &tb.ReplyMarkup{
						ReplyKeyboardRemove: true,
					},
				)
				UserState[m.Chat.ID] = fsm.None
				return
			}
		}
	case fsm.Sub:
		{
			url := strings.Split(m.Text, " ")
			if !CheckURL(url[0]) {
				_, _ = B.Reply(m, "请回复正确的URL", &tb.ReplyMarkup{ForceReply: true})
				return
			}

			registerFeed(m, m.Chat, url[0])
			UserState[m.Chat.ID] = fsm.None
		}
	case fsm.SetSubTag:
		{
			return
		}
	case fsm.Set:
		{
			str := strings.Split(m.Text, " ")
			url := str[len(str)-1]
			if len(str) != 2 && !CheckURL(url) {
				_, _ = B.Reply(m, "请选择正确的指令！")
			} else {
				source, err := model.GetSourceByUrl(url)

				if err != nil {
					_, _ = B.Reply(m, "请选择正确的指令！")
					return
				}
				sub, err := model.GetSubscribeByUserIDAndSourceID(m.Chat.ID, source.ID)
				if err != nil {
					_, _ = B.Reply(m, "请选择正确的指令！")
					return
				}

				t := template.New("setting template")
				_, _ = t.Parse(feedSettingTmpl)
				text := new(bytes.Buffer)
				_ = t.Execute(text, map[string]interface{}{
					"source": source,
					"sub":    sub,
					"Count":  config.ErrorThreshold,
				})

				data := fmt.Sprintf("%d:%d", m.Chat.ID, source.ID)
				textStr := fmt.Sprintf("%s%s", getUserHtml(m.Chat, m.Chat, ""), strings.TrimSpace(text.String()))
				_, _ = B.Reply(m, textStr, &tb.SendOptions{
					DisableWebPagePreview: true,
					ParseMode:             tb.ModeHTML,
				}, &tb.ReplyMarkup{
					InlineKeyboard:      genFeedSetBtn(data, sub, source),
					ReplyKeyboardRemove: true,
				})
				UserState[m.Chat.ID] = fsm.None
			}
		}
	default:
		urlMap := make(map[string]string, len(m.Entities))
		for _, entity := range m.Entities {
			if entity.Type != tb.EntityURL && entity.Type != tb.EntityTextLink {
				continue
			}
			url := entity.URL
			if url == "" {
				url = m.Text[entity.Offset : entity.Offset+entity.Length]
			}
			if IsTorrentUrl(url) {
				urlMap[url] = ""
			}
		}

		parts := strings.Split(m.Text, " ")
		for _, part := range parts {
			if strings.HasPrefix(part, util.PrefixMagnet) {
				urlMap[part] = ""
			}
		}

		total := len(urlMap)
		if total <= 0 {
			return
		}
		mention := GetMentionFromMessage(m)
		tgUser, err := getMentionedUser(m, mention, nil)
		if err != nil {
			return
		}
		user, _ := model.FindOrCreateUserByTelegramID(tgUser.ID)
		if user.Token == "" {
			return
		}
		count := AddPutIoTransfer(user.Token, urlMap)
		_, _ = B.Reply(m, fmt.Sprintf("发现%d条链接，成功添加%d个下载任务", total, count))
	}
}

// docCtr Document handler
func docCtr(m *tb.Message) {
	mention := GetMentionFromMessage(m)
	user, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Reply(m, err.Error())
		return
	}

	url, _ := B.FileURLByID(m.Document.FileID)
	switch m.Document.MIME {
	case util.ContentTypeOpml:
		importOpmlFile(m, user.ID, url)
	case util.ContentTypeTorrent:
		startTorrentFileTransfer(m, user.ID, url)
	}
}

func importOpmlFile(m *tb.Message, userID int64, url string) {
	opml, err := GetOPMLByURL(url)
	if err != nil {
		var text string
		if err.Error() == "fetch opml file error" {
			text = "下载 OPML 文件失败，请检查 bot 服务器能否正常连接至 Telegram 服务器或稍后尝试导入。错误代码 02"
		} else {
			text = fmt.Sprintf("如果需要导入订阅，请发送正确的 OPML 文件。错误代码 01，doc mimetype: %s", m.Document.MIME)
		}
		_, _ = B.Reply(m, text)
		return
	}

	message, _ := B.Reply(m, "处理中，请稍候...")
	outlines, _ := opml.GetFlattenOutlines()
	var failImportList []Outline
	var successImportList []Outline

	for _, outline := range outlines {
		source, err := model.RegistFeed(userID, outline.XMLURL)
		if err != nil {
			failImportList = append(failImportList, outline)
			continue
		}
		zap.S().Infof("%d subscribe [%d]%s %s", m.Chat.ID, source.ID, source.Title, source.Link)
		successImportList = append(successImportList, outline)
	}

	importReport := fmt.Sprintf("<b>导入成功：%d，导入失败：%d</b>", len(successImportList), len(failImportList))
	if len(successImportList) != 0 {
		successReport := "\n\n<b>以下订阅源导入成功:</b>"
		for i, line := range successImportList {
			if line.Text != "" {
				successReport += fmt.Sprintf("\n[%d] <a href=\"%s\">%s</a>", i+1, line.XMLURL, line.Text)
			} else {
				successReport += fmt.Sprintf("\n[%d] %s", i+1, line.XMLURL)
			}
		}
		importReport += successReport
	}

	if len(failImportList) != 0 {
		failReport := "\n\n<b>以下订阅源导入失败:</b>"
		for i, line := range failImportList {
			if line.Text != "" {
				failReport += fmt.Sprintf("\n[%d] <a href=\"%s\">%s</a>", i+1, line.XMLURL, line.Text)
			} else {
				failReport += fmt.Sprintf("\n[%d] %s", i+1, line.XMLURL)
			}
		}
		importReport += failReport
	}

	_, _ = B.Edit(message, importReport, &tb.SendOptions{
		DisableWebPagePreview: true,
		ParseMode:             tb.ModeHTML,
	})
}

func startTorrentFileTransfer(msg *tb.Message, userId int64, url string) {
	user, _ := model.FindOrCreateUserByTelegramID(userId)
	if user.Token == "" {
		_, _ = B.Reply(msg, "请先通过 `/set_token` 设置Put.io的token", &tb.SendOptions{
			ParseMode: tb.ModeMarkdown,
		})
		return
	}
	urlMap := map[string]string{url: ""}
	count := AddPutIoTransfer(user.Token, urlMap)
	if count <= 0 {
		_, _ = B.Reply(msg, "添加下载任务失败")
		return
	}
	_, _ = B.Reply(msg, "成功添加下载任务")
}
