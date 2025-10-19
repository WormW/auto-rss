package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var log *zap.SugaredLogger

// Init 初始化日志
func Init(level string) error {
	var zapLevel zapcore.Level
	switch level {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "info":
		zapLevel = zapcore.InfoLevel
	case "warn":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		zapLevel = zapcore.InfoLevel
	}

	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(zapLevel)
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.Encoding = "json"

	logger, err := config.Build()
	if err != nil {
		return err
	}

	log = logger.Sugar()
	return nil
}

// Sync 刷新日志缓冲区
func Sync() {
	if log != nil {
		_ = log.Sync()
	}
}

// Debug 输出 Debug 级别日志
func Debug(msg string, keysAndValues ...interface{}) {
	if log != nil {
		log.Debugw(msg, keysAndValues...)
	}
}

// Info 输出 Info 级别日志
func Info(msg string, keysAndValues ...interface{}) {
	if log != nil {
		log.Infow(msg, keysAndValues...)
	}
}

// Warn 输出 Warn 级别日志
func Warn(msg string, keysAndValues ...interface{}) {
	if log != nil {
		log.Warnw(msg, keysAndValues...)
	}
}

// Error 输出 Error 级别日志
func Error(msg string, keysAndValues ...interface{}) {
	if log != nil {
		log.Errorw(msg, keysAndValues...)
	}
}

// Fatal 输出 Fatal 级别日志并退出程序
func Fatal(msg string, keysAndValues ...interface{}) {
	if log != nil {
		log.Fatalw(msg, keysAndValues...)
	}
}
