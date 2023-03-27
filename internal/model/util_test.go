package model

import "testing"

func Test_genHashID(t *testing.T) {
	type args struct {
		sLink string
		id    string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{"case1", args{"http://www.ruanyifeng.com/blog/atom.xml", "tag:www.ruanyifeng.com,2019:/blog//1.2054"}, "96b2e254"},
		{"case2", args{"https://rsshub.app/guokr/scientific", "https://www.guokr.com/article/445877/"}, "770fff44"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := genHashID(tt.args.sLink, tt.args.id); got != tt.want {
				t.Errorf("genHashID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetTorrentInfoHash(t *testing.T) {
	tests := []struct {
		name       string
		torrentUrl string
		infoHash   string
	}{
		{"case1", "https://nyaa.si/download/1495283.torrent", "ef38da8258c9a8b5628daf72e62a814e8a4124b7"},
		{"case2", "https://mikanani.me/Download/20220225/22c6d340d0cb30922212d15ea410bf28a31f6662.torrent", "22c6d340d0cb30922212d15ea410bf28a31f6662"},
		{"case3", "http://v2.uploadbt.com/?r=down&hash=2062d66a0a7f3140f1d2328655d28539e74f5fb3", "2062d66a0a7f3140f1d2328655d28539e74f5fb3"},
		{"case4", "https://bangumi.moe/download/torrent/6218c0c28937cd0007954f1a/[Lilith-Raws] Fate_Grand Order - 神聖圓桌領域卡美洛 - 02 - Paladin；Agateram [Baha][WEB-DL][1080p][AVC AAC][CHT][MP4].torrent", "92fb173ea76d006b833565eb4ac2b030775556da"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, _ := getTorrentInfoHash(tt.torrentUrl); got != tt.infoHash {
				t.Errorf("getTorrentInfoHash() = %s, want %s", got, tt.infoHash)
			}
		})
	}
}
