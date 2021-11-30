package bot

import (
	"strings"
	"time"

	"github.com/indes/flowerss-bot/bot/fsm"
	"github.com/indes/flowerss-bot/config"
	"github.com/indes/flowerss-bot/util"
	"go.uber.org/zap"
	tb "gopkg.in/tucnak/telebot.v2"
)

var (
	// UserState 用户状态，用于标示当前用户操作所在状态
	UserState map[int64]fsm.UserStatus = make(map[int64]fsm.UserStatus)

	// B telebot
	B *tb.Bot
)

func init() {
	if config.RunMode == config.TestMode {
		return
	}
	var poller tb.Poller
	if config.TelegramWebhookEndpoint == "" {
		poller = &tb.LongPoller{Timeout: 10 * time.Second}
	} else {
		poller = &tb.Webhook{
			Listen: ":5000",
			Endpoint: &tb.WebhookEndpoint{
				PublicURL: strings.TrimSuffix(config.TelegramWebhookEndpoint, "/") + "/" + config.BotToken,
			},
		}
	}
	spamProtected := tb.NewMiddlewarePoller(poller, func(upd *tb.Update) bool {
		if !isUserAllowed(upd) {
			// 检查用户是否可以使用bot
			return false
		}

		if !CheckAdmin(upd) {
			return false
		}
		return true
	})
	zap.S().Infow("init telegram bot",
		"token", config.BotToken,
		"endpoint", config.TelegramEndpoint,
	)

	// create bot
	var err error

	B, err = tb.NewBot(tb.Settings{
		URL:    config.TelegramEndpoint,
		Token:  config.BotToken,
		Poller: spamProtected,
		Client: util.HttpClient,
	})

	if err != nil {
		zap.S().Fatal(err)
		return
	}
}

//Start bot
func Start() {
	if config.RunMode != config.TestMode {
		zap.S().Infof("bot start %s", config.AppVersionInfo())
		setCommands()
		setHandle()
		B.Start()
	}
}

func setCommands() {
	// 设置bot命令提示信息
	commands := []tb.Command{
		{"start", "开始使用"},
		{"list", "查看当前订阅的RSS源"},
		{"sub", "[url] 订阅RSS源 (url 为可选)"},
		{"unsub", "[url] 退订RSS源 (url 为可选)"},
		{"unsub_all", "退订所有rss源"},

		{"set", "对RSS订阅进行设置"},
		{"set_feed_tag", "[sub id] [tag1] [tag2] 设置RSS订阅的标签 (最多设置三个tag，以空格分隔)"},
		{"set_interval", "[interval] [sub id] 设置RSS订阅的抓取间隔 (可同时对多个sub id进行设置，以空格分隔)"},
		{"set_webhook", "[sub id] [webhook] 设置RSS订阅的webhook"},
		{"set_token", "[token] 设置Put.io的token"},

		{"export", "导出订阅为OPML文件"},
		{"import", "从OPML文件导入订阅"},

		{"check", "检查RSS订阅的当前状态"},
		{"pause_all", "停止抓取订阅更新"},
		{"active_all", "开启抓取订阅更新"},

		{"help", "使用帮助"},
		{"version", "bot版本"},
	}

	zap.S().Debugf("set bot command %+v", commands)

	if err := B.SetCommands(commands); err != nil {
		zap.S().Errorw("set bot commands failed", "error", err.Error())
	}
}

func setHandle() {
	B.Handle(&tb.InlineButton{Unique: "set_feed_item_btn"}, setFeedItemBtnCtr)

	B.Handle(&tb.InlineButton{Unique: "set_toggle_notice_btn"}, setToggleNoticeBtnCtr)

	B.Handle(&tb.InlineButton{Unique: "set_toggle_telegraph_btn"}, setToggleTelegraphBtnCtr)

	B.Handle(&tb.InlineButton{Unique: "set_toggle_update_btn"}, setToggleUpdateBtnCtr)

	B.Handle(&tb.InlineButton{Unique: "set_set_sub_tag_btn"}, setSubTagBtnCtr)

	B.Handle(&tb.InlineButton{Unique: "unsub_all_confirm_btn"}, unsubAllConfirmBtnCtr)

	B.Handle(&tb.InlineButton{Unique: "unsub_all_cancel_btn"}, unsubAllCancelBtnCtr)

	B.Handle(&tb.InlineButton{Unique: "unsub_feed_item_btn"}, unsubFeedItemBtnCtr)

	B.Handle("/start", startCmdCtr)

	B.Handle("/export", exportCmdCtr)

	B.Handle("/sub", subCmdCtr)

	B.Handle("/list", listCmdCtr)

	B.Handle("/set", setCmdCtr)

	B.Handle("/unsub", unsubCmdCtr)

	B.Handle("/unsub_all", unsubAllCmdCtr)

	B.Handle("/ping", pingCmdCtr)

	B.Handle("/help", helpCmdCtr)

	B.Handle("/import", importCmdCtr)

	B.Handle("/set_feed_tag", setFeedTagCmdCtr)

	B.Handle("/set_webhook", setWebhookCmdCtr)

	B.Handle("/set_token", setTokenCmdCtr)

	B.Handle("/set_interval", setIntervalCmdCtr)

	B.Handle("/check", checkCmdCtr)

	B.Handle("/active_all", activeAllCmdCtr)

	B.Handle("/pause_all", pauseAllCmdCtr)

	B.Handle("/version", versionCmdCtr)

	B.Handle(tb.OnText, textCtr)

	B.Handle(tb.OnDocument, docCtr)
}
