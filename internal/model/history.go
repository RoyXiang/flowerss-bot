package model

type HistoryType int

const (
	HistoryTelegramMessage HistoryType = iota
	HistoryTorrentTransfer
)

type History struct {
	Type      HistoryType `gorm:"index"`
	TriggerId string      `gorm:"index"`
	TargetId  string      `gorm:"index"`
}

func (h *History) IsSaved() bool {
	var result History
	err := db.Where(h).First(&result).Error
	return err == nil
}

func (h *History) Save() {
	db.Create(h)
}
