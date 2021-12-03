package util

import (
	"strings"
)

func init() {
	clientInit()
}

func GetMagnetLink(str string) (link string) {
	if !strings.HasPrefix(str, PrefixMagnet) {
		return
	}
	str = str[20:]
	idx := strings.IndexByte(str, '&')
	if idx != -1 {
		str = str[:idx]
	}
	if len(str) == 40 {
		link = PrefixMagnet + strings.ToLower(str)
	}
	return
}
