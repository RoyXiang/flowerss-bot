package bot

import (
	"bytes"
	"fmt"
	"html"
	"html/template"
	"strconv"
	"strings"
	"time"

	"github.com/indes/flowerss-bot/bot/fsm"
	"github.com/indes/flowerss-bot/config"
	"github.com/indes/flowerss-bot/model"
	"go.uber.org/zap"

	tb "gopkg.in/tucnak/telebot.v2"
)

var (
	feedSettingTmpl = `
订阅<b>设置</b>
[id] {{ .sub.ID }}
[标题] {{ .source.Title }}
[Link] {{.source.Link }}
[抓取更新] {{if ge .source.ErrorCount .Count }}暂停{{else if lt .source.ErrorCount .Count }}抓取中{{end}}
[抓取频率] {{ .sub.Interval }}分钟
[通知] {{if eq .sub.EnableNotification 0}}关闭{{else if eq .sub.EnableNotification 1}}开启{{end}}
[Telegraph] {{if eq .sub.EnableTelegraph 0}}关闭{{else if eq .sub.EnableTelegraph 1}}开启{{end}}
[Tag] {{if .sub.Tag}}{{ .sub.Tag }}{{else}}无{{end}}
`
)

func toggleCtrlButtons(c *tb.Callback, action string) {
	data := strings.Split(c.Data, ":")
	if len(data) != 2 {
		_, _ = B.Edit(c.Message, "内部错误：回调数据不正确")
		return
	}

	_, err := getMentionedUser(c.Message, data[0], c.Sender)
	if err != nil {
		_, _ = B.Edit(c.Message, err.Error())
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
	case "toggleNotice":
		err = sub.ToggleNotification()
	case "toggleTelegraph":
		err = sub.ToggleTelegraph()
	case "toggleUpdate":
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

	_ = t.Execute(text, map[string]interface{}{"source": source, "sub": sub, "Count": config.ErrorThreshold})
	_ = B.Respond(c, &tb.CallbackResponse{
		Text: "修改成功",
	})
	_, _ = B.Edit(c.Message, text.String(), &tb.SendOptions{
		ParseMode: tb.ModeHTML,
	}, &tb.ReplyMarkup{
		InlineKeyboard: genFeedSetBtn(c, sub, source),
	})
}

func startCmdCtr(m *tb.Message) {
	user, _ := model.FindOrCreateUserByTelegramID(m.Chat.ID)
	zap.S().Infof("/start user_id: %d telegram_id: %d", user.ID, user.TelegramID)
	_, _ = B.Send(m.Chat, fmt.Sprintf("你好，欢迎使用flowerss。"))
}

func subCmdCtr(m *tb.Message) {
	url, mention := GetURLAndMentionFromMessage(m)
	user, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Send(m.Chat, err.Error())
		return
	}
	registerFeed(m.Chat, user, url)
}

func exportCmdCtr(m *tb.Message) {
	mention := GetMentionFromMessage(m)
	user, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Send(m.Chat, err.Error())
		return
	}
	sourceList, err := model.GetSourcesByUserID(user.ID)
	if err != nil {
		zap.S().Errorf(err.Error())
		_, _ = B.Send(m.Chat, fmt.Sprintf("导出失败"))
		return
	}

	if len(sourceList) == 0 {
		_, _ = B.Send(m.Chat, fmt.Sprintf("订阅列表为空"))
		return
	}

	opmlStr, err := ToOPML(sourceList)

	if err != nil {
		_, _ = B.Send(m.Chat, fmt.Sprintf("导出失败"))
		return
	}
	opmlFile := &tb.Document{File: tb.FromReader(strings.NewReader(opmlStr))}
	opmlFile.FileName = fmt.Sprintf("subscriptions_%d.opml", time.Now().Unix())
	_, err = B.Send(m.Chat, opmlFile)

	if err != nil {
		_, _ = B.Send(m.Chat, fmt.Sprintf("导出失败"))
		zap.S().Errorf("send opml file failed, err:%+v", err)
	}
}

func listCmdCtr(m *tb.Message) {
	mention := GetMentionFromMessage(m)
	chat, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Send(m.Chat, err.Error())
		return
	}

	user, err := model.FindOrCreateUserByTelegramID(chat.ID)
	if err != nil {
		_, _ = B.Send(m.Chat, "内部错误：无法找到对应的用户")
		return
	}
	subSourceMap, err := user.GetSubSourceMap()
	if err != nil {
		_, _ = B.Send(m.Chat, "内部错误：无法查询用户订阅列表")
		return
	}

	rspMessage := getUserHtml(chat, m.Chat, "当前")
	if len(subSourceMap) == 0 {
		rspMessage += "订阅列表为空"
	} else {
		rspMessage += "订阅列表：\n"
		for sub, source := range subSourceMap {
			rspMessage += fmt.Sprintf("[%d] <a href=\"%s\">%s</a>\n", sub.ID, source.Link, html.EscapeString(source.Title))
		}
	}
	_, _ = B.Send(m.Chat, rspMessage, &tb.SendOptions{
		DisableWebPagePreview: true,
		ParseMode:             tb.ModeHTML,
	})
}

func checkCmdCtr(m *tb.Message) {
	mention := GetMentionFromMessage(m)
	user, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Send(m.Chat, err.Error())
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
	_, _ = B.Send(m.Chat, message, &tb.SendOptions{
		DisableWebPagePreview: true,
		ParseMode:             tb.ModeHTML,
	})
}

func setCmdCtr(m *tb.Message) {
	mention := GetMentionFromMessage(m)
	user, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Send(m.Chat, err.Error())
		return
	}

	sources, err := model.GetSourcesByUserID(user.ID)
	if len(sources) <= 0 {
		text := fmt.Sprintf("%s没有订阅源", getUserHtml(user, m.Chat, "当前"))
		_, _ = B.Send(m.Chat, text, &tb.SendOptions{
			DisableWebPagePreview: true,
			ParseMode:             tb.ModeHTML,
		})
		return
	}

	var replyButton []tb.ReplyButton
	var replyKeys [][]tb.ReplyButton
	var setFeedItemBtns [][]tb.InlineButton

	// 配置按钮
	for _, source := range sources {
		// 添加按钮
		text := fmt.Sprintf("%s %s", source.Title, source.Link)
		replyButton = []tb.ReplyButton{{Text: text}}
		replyKeys = append(replyKeys, replyButton)

		setFeedItemBtns = append(setFeedItemBtns, []tb.InlineButton{{
			Unique: "set_feed_item_btn",
			Text:   fmt.Sprintf("[%d] %s", source.ID, source.Title),
			Data:   fmt.Sprintf("%d:%d", user.ID, source.ID),
		}})
	}

	_, _ = B.Send(m.Chat, "请选择你要设置的源", &tb.ReplyMarkup{
		InlineKeyboard: setFeedItemBtns,
	})
}

func setFeedItemBtnCtr(c *tb.Callback) {
	data := strings.Split(c.Data, ":")
	if len(data) != 2 {
		_, _ = B.Edit(c.Message, "内部错误：回调数据不正确")
		return
	}

	user, err := getMentionedUser(c.Message, data[0], c.Sender)
	if err != nil {
		_, _ = B.Edit(c.Message, err.Error())
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
	_ = t.Execute(text, map[string]interface{}{"source": source, "sub": sub, "Count": config.ErrorThreshold})

	_, _ = B.Edit(
		c.Message,
		text.String(),
		&tb.SendOptions{
			ParseMode: tb.ModeHTML,
		}, &tb.ReplyMarkup{
			InlineKeyboard: genFeedSetBtn(c, sub, source),
		},
	)
}

func setSubTagBtnCtr(c *tb.Callback) {
	data := strings.Split(c.Data, ":")
	if len(data) != 2 {
		_, _ = B.Edit(c.Message, "内部错误：回调数据不正确")
		return
	}

	user, err := getMentionedUser(c.Message, data[0], c.Sender)
	if err != nil {
		_, _ = B.Edit(c.Message, err.Error())
		return
	}

	sourceID, _ := strconv.Atoi(data[1])
	sub, err := model.GetSubscribeByUserIDAndSourceID(user.ID, uint(sourceID))
	if err != nil {
		_, _ = B.Send(
			c.Message.Chat,
			"系统错误，代码04",
		)
		return
	}
	msg := fmt.Sprintf(
		"请使用`/setfeedtag %d tags`命令为该订阅设置标签，tags为需要设置的标签，以空格分隔。（最多设置三个标签） \n"+
			"例如：`/setfeedtag %d 科技 苹果`",
		sub.ID, sub.ID)

	_ = B.Delete(c.Message)

	_, _ = B.Send(
		c.Message.Chat,
		msg,
		&tb.SendOptions{ParseMode: tb.ModeMarkdown},
	)
}

func genFeedSetBtn(c *tb.Callback, sub *model.Subscribe, source *model.Source) [][]tb.InlineButton {
	setSubTagKey := tb.InlineButton{
		Unique: "set_set_sub_tag_btn",
		Text:   "标签设置",
		Data:   c.Data,
	}

	toggleNoticeKey := tb.InlineButton{
		Unique: "set_toggle_notice_btn",
		Text:   "开启通知",
		Data:   c.Data,
	}
	if sub.EnableNotification == 1 {
		toggleNoticeKey.Text = "关闭通知"
	}

	toggleTelegraphKey := tb.InlineButton{
		Unique: "set_toggle_telegraph_btn",
		Text:   "开启 Telegraph 转码",
		Data:   c.Data,
	}
	if sub.EnableTelegraph == 1 {
		toggleTelegraphKey.Text = "关闭 Telegraph 转码"
	}

	toggleEnabledKey := tb.InlineButton{
		Unique: "set_toggle_update_btn",
		Text:   "暂停更新",
		Data:   c.Data,
	}

	if source.ErrorCount >= config.ErrorThreshold {
		toggleEnabledKey.Text = "重启更新"
	}

	feedSettingKeys := [][]tb.InlineButton{
		[]tb.InlineButton{
			toggleEnabledKey,
			toggleNoticeKey,
		},
		[]tb.InlineButton{
			toggleTelegraphKey,
			setSubTagKey,
		},
	}
	return feedSettingKeys
}

func setToggleNoticeBtnCtr(c *tb.Callback) {
	toggleCtrlButtons(c, "toggleNotice")
}

func setToggleTelegraphBtnCtr(c *tb.Callback) {
	toggleCtrlButtons(c, "toggleTelegraph")
}

func setToggleUpdateBtnCtr(c *tb.Callback) {
	toggleCtrlButtons(c, "toggleUpdate")
}

func unsubCmdCtr(m *tb.Message) {
	url, mention := GetURLAndMentionFromMessage(m)
	user, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Send(m.Chat, err.Error())
		return
	}

	if url != "" {
		source, err := model.GetSourceByUrl(url)
		if err != nil {
			_, _ = B.Send(m.Chat, "未订阅该RSS源")
			return
		}
		err = model.UnsubByUserIDAndSource(user.ID, source)
		if err != nil {
			_, _ = B.Send(m.Chat, fmt.Sprintf("退订失败：%s", err.Error()))
			return
		}
		text := getUserHtml(user, m.Chat, "")
		text += fmt.Sprintf("退订 <a href=\"%s\">%s</a> 成功！", source.Link, html.EscapeString(source.Title))
		_, _ = B.Send(m.Chat, text, &tb.SendOptions{
			DisableWebPagePreview: true,
			ParseMode:             tb.ModeHTML,
		})
		zap.S().Infof("%d unsubscribe [%d]%s %s", user.ID, source.ID, source.Title, source.Link)
	} else {
		subs, err := model.GetSubsByUserID(user.ID)
		if err != nil {
			_, _ = B.Send(m.Chat, "内部错误：查询订阅列表失败")
			return
		}

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
		if len(unsubFeedItemBtns) <= 0 {
			_, _ = B.Send(m.Chat, "订阅列表为空")
			return
		}

		_, _ = B.Send(m.Chat, "请选择你要退订的源", &tb.ReplyMarkup{
			InlineKeyboard: unsubFeedItemBtns,
		})
	}
}

func unsubFeedItemBtnCtr(c *tb.Callback) {
	data := strings.Split(c.Data, ":")
	if len(data) != 3 {
		_, _ = B.Edit(c.Message, "内部错误：回调数据不正确")
		return
	}
	user, err := getMentionedUser(c.Message, data[0], c.Sender)
	if err != nil {
		_, _ = B.Edit(c.Message, err.Error())
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
		_, _ = B.Send(m.Chat, err.Error())
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
	_, _ = B.Send(
		m.Chat,
		msg,
		&tb.SendOptions{
			DisableWebPagePreview: true,
			ParseMode:             tb.ModeHTML,
		},
		&tb.ReplyMarkup{
			InlineKeyboard: confirmKeys,
		},
	)
}

func unsubAllCancelBtnCtr(c *tb.Callback) {
	_, _ = B.Edit(c.Message, "操作取消")
}

func unsubAllConfirmBtnCtr(c *tb.Callback) {
	if c.Data == "" {
		_, _ = B.Edit(c.Message, "内部错误：回调内容不正确")
		return
	}
	user, err := getMentionedUser(c.Message, c.Data, c.Sender)
	if err != nil {
		_, _ = B.Edit(c.Message, err.Error())
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
	_, _ = B.Send(m.Chat, "pong")
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
/setfeedtag 设置订阅标签
/setinterval 设置订阅刷新频率
/activeall 开启所有订阅
/pauseall 暂停所有订阅
/help 帮助
/import 导入 OPML 文件
/export 导出 OPML 文件
/unsuball 取消所有订阅
详细使用方法请看：https://github.com/indes/flowerss-bot
`

	_, _ = B.Send(m.Chat, message)
}

func versionCmdCtr(m *tb.Message) {
	_, _ = B.Send(m.Chat, config.AppVersionInfo())
}

func importCmdCtr(m *tb.Message) {
	message := `请直接发送OPML文件，
如果需要为channel导入OPML，请在发送文件的时候附上channel id，例如@telegram
`
	_, _ = B.Send(m.Chat, message)
}

func setFeedTagCmdCtr(m *tb.Message) {
	args := strings.Split(m.Payload, " ")
	if len(args) < 1 {
		_, _ = B.Send(m.Chat, "/setfeedtag [sub id] [tag1] [tag2] 设置订阅标签（最多设置三个Tag，以空格分割）")
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
		_, _ = B.Send(m.Chat, "请输入正确的订阅ID！")
		return
	}
	sub, err := model.GetSubscribeByID(subID)
	if err != nil {
		_, _ = B.Send(m.Chat, "请输入正确的订阅ID！")
		return
	}
	_, err = getMentionedUser(m, strconv.FormatInt(sub.UserID, 10), nil)
	if err != nil {
		_, _ = B.Send(m.Chat, err.Error())
		return
	}

	err = sub.SetTag(args[1:])
	if err != nil {
		_, _ = B.Send(m.Chat, "订阅标签设置失败！")
		return
	}
	_, _ = B.Send(m.Chat, "订阅标签设置成功！")
}

func setWebhookCmdCtr(m *tb.Message) {
	args := strings.Split(m.Payload, " ")
	if len(args) < 1 {
		_, _ = B.Send(m.Chat, "/setwebhook [sub id] [webhook]")
		return
	}

	url, mention := GetURLAndMentionFromMessage(m)
	if mention != "" {
		args = args[1:]
	}
	subID, err := strconv.Atoi(args[0])
	if err != nil {
		_, _ = B.Send(m.Chat, "请输入正确的订阅ID！")
		return
	}
	sub, err := model.GetSubscribeByID(subID)
	if err != nil {
		_, _ = B.Send(m.Chat, "请输入正确的订阅ID！")
		return
	}
	_, err = getMentionedUser(m, strconv.FormatInt(sub.UserID, 10), nil)
	if err != nil {
		_, _ = B.Send(m.Chat, err.Error())
		return
	}

	err = sub.SetWebhook(url)
	if err != nil {
		_, _ = B.Send(m.Chat, "订阅webhook设置失败！")
		return
	}
	_, _ = B.Send(m.Chat, "订阅webhook设置成功！")
}

func setIntervalCmdCtr(m *tb.Message) {
	args := strings.Split(m.Payload, " ")
	if len(args) < 1 {
		_, _ = B.Send(m.Chat, "/setinterval [interval] [sub id] 设置订阅刷新频率（可设置多个sub id，以空格分割）")
		return
	}

	interval, err := strconv.Atoi(args[0])
	if interval <= 0 || err != nil {
		_, _ = B.Send(m.Chat, "请输入正确的抓取频率")
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
	_, _ = B.Send(m.Chat, fmt.Sprintf("抓取频率设置成功%d个，失败%d个，错误%d个！", success, failed, wrong))
}

func activeAllCmdCtr(m *tb.Message) {
	mention := GetMentionFromMessage(m)
	user, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Send(m.Chat, err.Error())
		return
	}
	_ = model.ActiveSourcesByUserID(user.ID)
	message := fmt.Sprintf("%s订阅已全部开启", getUserHtml(user, m.Chat, ""))
	_, _ = B.Send(m.Chat, message, &tb.SendOptions{
		DisableWebPagePreview: true,
		ParseMode:             tb.ModeHTML,
	})
}

func pauseAllCmdCtr(m *tb.Message) {
	mention := GetMentionFromMessage(m)
	user, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Send(m.Chat, err.Error())
		return
	}
	_ = model.PauseSourcesByUserID(user.ID)
	message := fmt.Sprintf("%s订阅已全部暂停", getUserHtml(user, m.Chat, ""))
	_, _ = B.Send(m.Chat, message, &tb.SendOptions{
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
				_, _ = B.Send(m.Chat, "请选择正确的指令！")
			} else {

				var sourceID uint
				if _, err := fmt.Sscanf(str[0], "[%d]", &sourceID); err != nil {
					_, _ = B.Send(m.Chat, "请选择正确的指令！")
					return
				}

				source, err := model.GetSourceById(sourceID)

				if err != nil {
					_, _ = B.Send(m.Chat, "请选择正确的指令！")
					return
				}

				err = model.UnsubByUserIDAndSource(m.Chat.ID, source)

				if err != nil {
					_, _ = B.Send(m.Chat, "请选择正确的指令！")
					return
				}

				_, _ = B.Send(
					m.Chat,
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
				_, _ = B.Send(m.Chat, "请回复正确的URL", &tb.ReplyMarkup{ForceReply: true})
				return
			}

			registerFeed(m.Chat, m.Chat, url[0])
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
				_, _ = B.Send(m.Chat, "请选择正确的指令！")
			} else {
				source, err := model.GetSourceByUrl(url)

				if err != nil {
					_, _ = B.Send(m.Chat, "请选择正确的指令！")
					return
				}
				sub, err := model.GetSubscribeByUserIDAndSourceID(m.Chat.ID, source.ID)
				if err != nil {
					_, _ = B.Send(m.Chat, "请选择正确的指令！")
					return
				}
				t := template.New("setting template")
				_, _ = t.Parse(feedSettingTmpl)

				toggleNoticeKey := tb.InlineButton{
					Unique: "set_toggle_notice_btn",
					Text:   "开启通知",
				}
				if sub.EnableNotification == 1 {
					toggleNoticeKey.Text = "关闭通知"
				}

				toggleTelegraphKey := tb.InlineButton{
					Unique: "set_toggle_telegraph_btn",
					Text:   "开启 Telegraph 转码",
				}
				if sub.EnableTelegraph == 1 {
					toggleTelegraphKey.Text = "关闭 Telegraph 转码"
				}

				toggleEnabledKey := tb.InlineButton{
					Unique: "set_toggle_update_btn",
					Text:   "暂停更新",
				}

				if source.ErrorCount >= config.ErrorThreshold {
					toggleEnabledKey.Text = "重启更新"
				}

				feedSettingKeys := [][]tb.InlineButton{
					[]tb.InlineButton{
						toggleEnabledKey,
						toggleNoticeKey,
						toggleTelegraphKey,
					},
				}

				text := new(bytes.Buffer)

				_ = t.Execute(text, map[string]interface{}{"source": source, "sub": sub, "Count": config.ErrorThreshold})

				// send null message to remove old keyboard
				delKeyMessage, err := B.Send(m.Chat, "processing", &tb.ReplyMarkup{ReplyKeyboardRemove: true})
				err = B.Delete(delKeyMessage)

				_, _ = B.Send(
					m.Chat,
					text.String(),
					&tb.SendOptions{
						ParseMode: tb.ModeHTML,
					}, &tb.ReplyMarkup{
						InlineKeyboard: feedSettingKeys,
					},
				)
				UserState[m.Chat.ID] = fsm.None
			}
		}
	}
}

// docCtr Document handler
func docCtr(m *tb.Message) {
	url, _ := B.FileURLByID(m.Document.FileID)
	if !strings.HasSuffix(url, ".opml") {
		_, _ = B.Send(m.Chat, "如果需要导入订阅，请发送正确的 OPML 文件。")
		return
	}

	opml, err := GetOPMLByURL(url)
	if err != nil {
		if err.Error() == "fetch opml file error" {
			_, _ = B.Send(m.Chat,
				"下载 OPML 文件失败，请检查 bot 服务器能否正常连接至 Telegram 服务器或稍后尝试导入。错误代码 02")

		} else {
			_, _ = B.Send(
				m.Chat,
				fmt.Sprintf(
					"如果需要导入订阅，请发送正确的 OPML 文件。错误代码 01，doc mimetype: %s",
					m.Document.MIME),
			)
		}
		return
	}

	mention := GetMentionFromMessage(m)
	user, err := getMentionedUser(m, mention, nil)
	if err != nil {
		_, _ = B.Send(m.Chat, err.Error())
		return
	}
	userID := user.ID

	message, _ := B.Send(m.Chat, "处理中，请稍后...")
	outlines, _ := opml.GetFlattenOutlines()
	var failImportList []Outline
	var successImportList []Outline

	for _, outline := range outlines {
		source, err := model.FindOrNewSourceByUrl(outline.XMLURL)
		if err != nil {
			failImportList = append(failImportList, outline)
			continue
		}
		err = model.RegistFeed(userID, source.ID)
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
