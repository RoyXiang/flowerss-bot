package log

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type DbLogger struct {
	logger.Config
}

func (l *DbLogger) LogMode(level logger.LogLevel) logger.Interface {
	l.LogLevel = level
	l.IgnoreRecordNotFoundError = true
	l.SlowThreshold = time.Second
	l.Colorful = false
	return l
}

func (l DbLogger) Info(_ context.Context, msg string, data ...interface{}) {
	if l.LogLevel < logger.Info {
		return
	}
	Logger.Info(fmt.Sprintf(msg, append([]interface{}{}, data...)...))
}

func (l DbLogger) Warn(_ context.Context, msg string, data ...interface{}) {
	if l.LogLevel < logger.Warn {
		return
	}
	Logger.Warn(fmt.Sprintf(msg, append([]interface{}{}, data...)...))
}

func (l DbLogger) Error(_ context.Context, msg string, data ...interface{}) {
	if l.LogLevel < logger.Error {
		return
	}
	Logger.Error(fmt.Sprintf(msg, append([]interface{}{}, data...)...))
}

func (l DbLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.LogLevel <= logger.Silent {
		return
	}
	elapsed := time.Since(begin)
	switch {
	case err != nil && l.LogLevel >= logger.Error && (!errors.Is(err, gorm.ErrRecordNotFound) || !l.IgnoreRecordNotFoundError):
		sql, rows := fc()
		if rows == -1 {
			Logger.Debug(fmt.Sprintf("%s [%.3fms] [rows:%v] %s", err, float64(elapsed.Nanoseconds())/1e6, "-", sql))
		} else {
			Logger.Debug(fmt.Sprintf("%s [%.3fms] [rows:%v] %s", err, float64(elapsed.Nanoseconds())/1e6, rows, sql))
		}
	case elapsed > l.SlowThreshold && l.SlowThreshold != 0 && l.LogLevel >= logger.Warn:
		sql, rows := fc()
		slowLog := fmt.Sprintf("SLOW SQL >= %v", l.SlowThreshold)
		if rows == -1 {
			Logger.Debug(fmt.Sprintf("%s [%.3fms] [rows:%v] %s", slowLog, float64(elapsed.Nanoseconds())/1e6, "-", sql))
		} else {
			Logger.Debug(fmt.Sprintf("%s [%.3fms] [rows:%v] %s", slowLog, float64(elapsed.Nanoseconds())/1e6, rows, sql))
		}
	case l.LogLevel == logger.Info:
		sql, rows := fc()
		if rows == -1 {
			Logger.Debug(fmt.Sprintf("[%.3fms] [rows:%v] %s", float64(elapsed.Nanoseconds())/1e6, "-", sql))
		} else {
			Logger.Debug(fmt.Sprintf("[%.3fms] [rows:%v] %s", float64(elapsed.Nanoseconds())/1e6, rows, sql))
		}
	}
}
