package mcpserver

import (
	"context"
	"fmt"

	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/WormW/auto-rss/internal/service/mikan"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) searchMikan(ctx context.Context, req *mcp.CallToolRequest, input SearchMikanInput) (*mcp.CallToolResult, any, error) {
	if len(input.Query) < 2 {
		return nil, nil, fmt.Errorf("query must be at least 2 characters")
	}
	s.setProxy()
	result, err := s.mikanService.Search(input.Query)
	if err != nil {
		return nil, nil, fmt.Errorf("Mikan search failed: %w", err)
	}
	s.markMikanExisting(result)
	return resultWithText[any](result)
}

func (s *Server) getMikanSeason(ctx context.Context, req *mcp.CallToolRequest, input GetMikanSeasonInput) (*mcp.CallToolResult, any, error) {
	if input.Year <= 0 {
		return nil, nil, fmt.Errorf("year is required")
	}
	if input.Season == "" {
		return nil, nil, fmt.Errorf("season is required")
	}
	s.setProxy()
	result, err := s.mikanService.GetBySeason(input.Year, input.Season)
	if err != nil {
		return nil, nil, fmt.Errorf("Mikan season lookup failed: %w", err)
	}
	s.markMikanExisting(result)
	return resultWithText[any](result)
}

func (s *Server) getMikanFansubs(ctx context.Context, req *mcp.CallToolRequest, input GetMikanFansubsInput) (*mcp.CallToolResult, any, error) {
	if input.URL == "" {
		return nil, nil, fmt.Errorf("url is required")
	}
	s.setProxy()
	groups, err := s.mikanService.GetFansubGroups(input.URL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get Mikan fansub groups: %w", err)
	}
	return resultWithText[any](groups)
}

func (s *Server) searchBangumi(ctx context.Context, req *mcp.CallToolRequest, input SearchBangumiInput) (*mcp.CallToolResult, any, error) {
	if input.Query == "" {
		return nil, nil, fmt.Errorf("query is required")
	}
	s.setProxy()
	if input.BestOnly {
		subject, err := s.bangumiService.SearchByName(input.Query)
		if err != nil {
			return nil, nil, fmt.Errorf("Bangumi best match lookup failed: %w", err)
		}
		return resultWithText[any](subject)
	}
	result, err := s.bangumiService.Search(input.Query, bangumi.SubjectTypeAnime)
	if err != nil {
		return nil, nil, fmt.Errorf("Bangumi search failed: %w", err)
	}
	return resultWithText[any](result)
}

func (s *Server) getBangumiSubject(ctx context.Context, req *mcp.CallToolRequest, input GetBangumiSubjectInput) (*mcp.CallToolResult, any, error) {
	if input.ID <= 0 {
		return nil, nil, fmt.Errorf("id must be greater than 0")
	}
	s.setProxy()
	subject, err := s.bangumiService.GetSubject(input.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("Bangumi subject lookup failed: %w", err)
	}
	return resultWithText[any](subject)
}

func (s *Server) getCalendar(ctx context.Context, req *mcp.CallToolRequest, input GetCalendarInput) (*mcp.CallToolResult, any, error) {
	if input.TodayOnly {
		items, err := s.calendarService.GetTodaySchedule()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get today's calendar: %w", err)
		}
		return resultWithText[any](items)
	}
	schedule, err := s.calendarService.GetWeekSchedule(input.WeekOffset)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get calendar: %w", err)
	}
	return resultWithText[any](schedule)
}

func (s *Server) getDiskStatus(ctx context.Context, req *mcp.CallToolRequest, input GetDiskStatusInput) (*mcp.CallToolResult, any, error) {
	status, err := s.currentDiskStatus()
	if err != nil {
		return nil, nil, err
	}
	return resultWithText[any](status)
}

func (s *Server) listLogs(ctx context.Context, req *mcp.CallToolRequest, input ListLogsInput) (*mcp.CallToolResult, ListLogsOutput, error) {
	offset, err := decodeCursor(input.Cursor)
	if err != nil {
		return nil, ListLogsOutput{}, err
	}
	limit := normalizeLimit(input.Limit)
	page := offset/limit + 1

	logs, total, err := s.logRepo.List(page, limit, input.Level, input.Module)
	if err != nil {
		return nil, ListLogsOutput{}, fmt.Errorf("failed to list logs: %w", err)
	}

	out := ListLogsOutput{
		Items: make([]LogSummary, 0, len(logs)),
		PageInfo: PageInfo{
			Total:      total,
			Limit:      limit,
			NextCursor: nextCursor(offset, len(logs), total),
		},
	}
	for _, log := range logs {
		out.Items = append(out.Items, summarizeLog(log))
	}
	return resultWithText(out)
}

func (s *Server) currentDiskStatus() (any, error) {
	downloadPath := "/downloads"
	if s.configRepo != nil {
		if cfg, err := s.configRepo.Get("download_path"); err == nil && cfg != nil && cfg.Value != "" {
			downloadPath = cfg.Value
		}
	}

	info, err := s.diskMonitor.GetDiskInfo(downloadPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get disk status for %s: %w", downloadPath, err)
	}
	totalBytes := int64(info.TotalGB * 1024 * 1024 * 1024)
	freeBytes := int64(info.FreeGB * 1024 * 1024 * 1024)
	usedBytes := int64(info.UsedGB * 1024 * 1024 * 1024)

	return map[string]any{
		"path":          info.Path,
		"download_path": downloadPath,
		"total_bytes":   totalBytes,
		"free_bytes":    freeBytes,
		"used_bytes":    usedBytes,
		"total_gb":      info.TotalGB,
		"free_gb":       info.FreeGB,
		"used_gb":       info.UsedGB,
		"usage_percent": info.UsagePercent,
		"status":        info.Status,
	}, nil
}

func (s *Server) markMikanExisting(result *mikan.SearchResult) {
	subscriptions, _, err := s.subscriptionRepo.List(0, 9999)
	if err != nil {
		return
	}
	existingNames := make(map[string]bool, len(subscriptions))
	for _, sub := range subscriptions {
		existingNames[sub.Name] = true
	}

	if result == nil {
		return
	}
	for _, group := range result.Groups {
		for _, item := range group.Items {
			if existingNames[item.Title] {
				item.Exists = true
			}
		}
	}
}
