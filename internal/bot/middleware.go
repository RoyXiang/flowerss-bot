package bot

import (
	"encoding/json"
	"strconv"

	"github.com/pkg/errors"
	tb "gopkg.in/tucnak/telebot.v2"
)

func getChatByUserId(userId int64) (*tb.Chat, error) {
	params := map[string]int64{
		"chat_id": userId,
	}

	data, err := B.Raw("getChat", params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Result *tb.Chat
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, "telebot")
	}
	if resp.Result.Type == tb.ChatChannel && resp.Result.Username == "" {
		resp.Result.Type = tb.ChatChannelPrivate
	}
	return resp.Result, nil
}

func isAdminOfChat(senderId int, chat *tb.Chat) error {
	isChannel := chat.Type == tb.ChatChannel || chat.Type == tb.ChatChannelPrivate
	isBotAdmin := false
	if adminList, err := B.AdminsOf(chat); err == nil {
		isSenderAdmin := false
		for _, admin := range adminList {
			if admin.User.ID == senderId {
				isSenderAdmin = true
			} else if admin.User.ID == B.Me.ID {
				isBotAdmin = true
			}
		}
		if isSenderAdmin && isBotAdmin {
			return nil
		} else if !isChannel && isSenderAdmin {
			return nil
		}
	}
	if isChannel {
		if isBotAdmin {
			return ErrNotChannelAdmin
		}
		return ErrBotNotChannelAdmin
	} else if chat.Type == tb.ChatGroup || chat.Type == tb.ChatSuperGroup {
		return ErrNotGroupAdmin
	}
	return ErrNoPermission
}

func getMentionedUser(msg *tb.Message, mention string, sender *tb.User) (user *tb.Chat, err error) {
	if mention == "" {
		user = msg.Chat
		return
	}
	var chat *tb.Chat
	if userId, err := strconv.Atoi(mention); err != nil {
		chat, _ = B.ChatByID(mention)
	} else {
		chat, _ = getChatByUserId(int64(userId))
	}
	if chat == nil {
		err = ErrChatNotFound
		return
	}
	if sender == nil {
		sender = msg.Sender
	}
	if !HasAdminType(chat.Type) {
		if int64(sender.ID) == msg.Chat.ID {
			user = msg.Chat
		} else {
			err = ErrNoPermission
		}
		return
	}
	err = isAdminOfChat(sender.ID, chat)
	if err == nil {
		user = chat
	}
	return
}
