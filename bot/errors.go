package bot

import (
	"errors"
)

var (
	ErrNoPermission       = errors.New("无权进行此项操作")
	ErrChatNotFound       = errors.New("指定的会话不存在")
	ErrNotGroupAdmin      = errors.New("仅群组管理员可执行此项操作")
	ErrNotChannelAdmin    = errors.New("仅频道管理员可执行此项操作")
	ErrBotNotChannelAdmin = errors.New("请将 bot 添加为频道的管理员")
)
