package handler

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsCollector Prometheus 指标收集器
type MetricsCollector struct {
	// RSS 相关指标
	RSSFetchTotal      *prometheus.CounterVec
	RSSFetchDuration   prometheus.Histogram
	RSSEntryTotal      prometheus.Counter

	// 下载相关指标
	DownloadTotal      *prometheus.CounterVec
	DownloadDuration   prometheus.Histogram
	DownloadBytesTotal *prometheus.CounterVec

	// qBittorrent 相关指标
	QBittorrentRequestTotal    *prometheus.CounterVec
	QBittorrentRequestDuration prometheus.Histogram
	QBittorrentActiveTorrents  prometheus.Gauge

	// 系统指标
	ActiveSubscriptions prometheus.Gauge
	ActiveDownloads     prometheus.Gauge
	SchedulerTasksTotal *prometheus.CounterVec

	// HTTP 指标
	HTTPRequestTotal    *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec
}

// NewMetricsCollector 创建新的指标收集器
func NewMetricsCollector() *MetricsCollector {
	mc := &MetricsCollector{
		// RSS 抓取指标
		RSSFetchTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "autorss",
				Subsystem: "rss",
				Name:      "fetch_total",
				Help:      "Total number of RSS fetch attempts",
			},
			[]string{"source", "status"}, // source: 订阅源, status: success/error
		),
		RSSFetchDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Namespace: "autorss",
				Subsystem: "rss",
				Name:      "fetch_duration_seconds",
				Help:      "RSS fetch duration in seconds",
				Buckets:   prometheus.DefBuckets,
			},
		),
		RSSEntryTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: "autorss",
				Subsystem: "rss",
				Name:      "entry_total",
				Help:      "Total number of RSS entries processed",
			},
		),

		// 下载指标
		DownloadTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "autorss",
				Subsystem: "download",
				Name:      "total",
				Help:      "Total number of download attempts",
			},
			[]string{"status"}, // status: pending/downloading/completed/failed
		),
		DownloadDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Namespace: "autorss",
				Subsystem: "download",
				Name:      "duration_seconds",
				Help:      "Download duration in seconds",
				Buckets:   []float64{1, 5, 10, 30, 60, 120, 300, 600, 1800},
			},
		),
		DownloadBytesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "autorss",
				Subsystem: "download",
				Name:      "bytes_total",
				Help:      "Total bytes downloaded",
			},
			[]string{"status"},
		),

		// qBittorrent 指标
		QBittorrentRequestTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "autorss",
				Subsystem: "qbittorrent",
				Name:      "request_total",
				Help:      "Total qBittorrent API requests",
			},
			[]string{"endpoint", "status"},
		),
		QBittorrentRequestDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Namespace: "autorss",
				Subsystem: "qbittorrent",
				Name:      "request_duration_seconds",
				Help:      "qBittorrent API request duration",
				Buckets:   prometheus.DefBuckets,
			},
		),
		QBittorrentActiveTorrents: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "autorss",
				Subsystem: "qbittorrent",
				Name:      "active_torrents",
				Help:      "Number of active torrents in qBittorrent",
			},
		),

		// 系统指标
		ActiveSubscriptions: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "autorss",
				Subsystem: "subscriptions",
				Name:      "active",
				Help:      "Number of active subscriptions",
			},
		),
		ActiveDownloads: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "autorss",
				Subsystem: "downloads",
				Name:      "active",
				Help:      "Number of active downloads",
			},
		),
		SchedulerTasksTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "autorss",
				Subsystem: "scheduler",
				Name:      "tasks_total",
				Help:      "Total scheduler tasks executed",
			},
			[]string{"task_type", "status"},
		),

		// HTTP 指标
		HTTPRequestTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "autorss",
				Subsystem: "http",
				Name:      "requests_total",
				Help:      "Total HTTP requests",
			},
			[]string{"method", "endpoint", "status"},
		),
		HTTPRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "autorss",
				Subsystem: "http",
				Name:      "request_duration_seconds",
				Help:      "HTTP request duration",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"method", "endpoint"},
		),
	}

	// 注册所有指标
	mc.register()

	return mc
}

// register 注册指标到 Prometheus
func (mc *MetricsCollector) register() {
	prometheus.MustRegister(
		mc.RSSFetchTotal,
		mc.RSSFetchDuration,
		mc.RSSEntryTotal,
		mc.DownloadTotal,
		mc.DownloadDuration,
		mc.DownloadBytesTotal,
		mc.QBittorrentRequestTotal,
		mc.QBittorrentRequestDuration,
		mc.QBittorrentActiveTorrents,
		mc.ActiveSubscriptions,
		mc.ActiveDownloads,
		mc.SchedulerTasksTotal,
		mc.HTTPRequestTotal,
		mc.HTTPRequestDuration,
	)
}

// RecordRSSFetch 记录 RSS 抓取指标
func (mc *MetricsCollector) RecordRSSFetch(source string, success bool, duration time.Duration) {
	status := "success"
	if !success {
		status = "error"
	}
	mc.RSSFetchTotal.WithLabelValues(source, status).Inc()
	mc.RSSFetchDuration.Observe(duration.Seconds())
}

// RecordRSSEntry 记录处理的 RSS 条目数
func (mc *MetricsCollector) RecordRSSEntry(count int) {
	mc.RSSEntryTotal.Add(float64(count))
}

// RecordDownload 记录下载指标
func (mc *MetricsCollector) RecordDownload(status string, duration time.Duration, bytes int64) {
	mc.DownloadTotal.WithLabelValues(status).Inc()
	mc.DownloadDuration.Observe(duration.Seconds())
	mc.DownloadBytesTotal.WithLabelValues(status).Add(float64(bytes))
}

// RecordQBittorrentRequest 记录 qBittorrent 请求指标
func (mc *MetricsCollector) RecordQBittorrentRequest(endpoint string, success bool, duration time.Duration) {
	status := "success"
	if !success {
		status = "error"
	}
	mc.QBittorrentRequestTotal.WithLabelValues(endpoint, status).Inc()
	mc.QBittorrentRequestDuration.Observe(duration.Seconds())
}

// SetQBittorrentActiveTorrents 设置活跃种子数
func (mc *MetricsCollector) SetQBittorrentActiveTorrents(count int) {
	mc.QBittorrentActiveTorrents.Set(float64(count))
}

// SetActiveSubscriptions 设置活跃订阅数
func (mc *MetricsCollector) SetActiveSubscriptions(count int) {
	mc.ActiveSubscriptions.Set(float64(count))
}

// SetActiveDownloads 设置活跃下载数
func (mc *MetricsCollector) SetActiveDownloads(count int) {
	mc.ActiveDownloads.Set(float64(count))
}

// RecordSchedulerTask 记录调度器任务
func (mc *MetricsCollector) RecordSchedulerTask(taskType string, success bool) {
	status := "success"
	if !success {
		status = "error"
	}
	mc.SchedulerTasksTotal.WithLabelValues(taskType, status).Inc()
}

// RecordHTTPRequest 记录 HTTP 请求指标
func (mc *MetricsCollector) RecordHTTPRequest(method, endpoint string, statusCode int, duration time.Duration) {
	status := strconv.Itoa(statusCode)
	mc.HTTPRequestTotal.WithLabelValues(method, endpoint, status).Inc()
	mc.HTTPRequestDuration.WithLabelValues(method, endpoint).Observe(duration.Seconds())
}

// MetricsHandler 返回 gin 处理函数
func MetricsHandler() gin.HandlerFunc {
	return gin.WrapH(promhttp.Handler())
}

// MetricsMiddleware 创建 HTTP 指标中间件
func MetricsMiddleware(mc *MetricsCollector) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		
		c.Next()
		
		duration := time.Since(start)
		mc.RecordHTTPRequest(
			c.Request.Method,
			c.FullPath(),
			c.Writer.Status(),
			duration,
		)
	}
}

// DefaultCollector 默认指标收集器实例
var DefaultCollector *MetricsCollector

// InitMetrics 初始化全局指标收集器
func InitMetrics() {
	DefaultCollector = NewMetricsCollector()
}

// GetDefaultCollector 获取默认指标收集器
func GetDefaultCollector() *MetricsCollector {
	if DefaultCollector == nil {
		InitMetrics()
	}
	return DefaultCollector
}
