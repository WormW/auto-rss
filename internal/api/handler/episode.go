package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/episode"
	"github.com/WormW/auto-rss/internal/service/task"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type EpisodeHandler struct {
	subscriptionRepo repository.SubscriptionRepository
	episodeRepo      repository.EpisodeRepository
	episodeService   *episode.Service
	replacement      ReplacementActions
	tasks            episodeTaskStarter
}

type ReplacementActions interface {
	Replace(ctx context.Context, candidateID uint) error
	RetryCleanup(ctx context.Context, candidateID uint) error
}

type episodeTaskStarter interface {
	StartTask(taskType task.TaskType, subscriptionID uint, name string, fn func(context.Context, *task.Task) error) (*task.Task, error)
}

type EpisodeListItem struct {
	model.SubscriptionEpisode
	ActionRequiredCandidateCount int64 `json:"action_required_candidate_count"`
}

type UpdateEpisodeStatusRequest struct {
	Episodes []int  `json:"episodes"`
	Status   string `json:"status"`
}

const maxEpisodeStatusBatchSize = 500

func NewEpisodeHandler(
	subscriptionRepo repository.SubscriptionRepository,
	episodeRepo repository.EpisodeRepository,
	episodeService *episode.Service,
	replacement ReplacementActions,
) *EpisodeHandler {
	return newEpisodeHandler(subscriptionRepo, episodeRepo, episodeService, replacement, task.GetManager())
}

func newEpisodeHandler(
	subscriptionRepo repository.SubscriptionRepository,
	episodeRepo repository.EpisodeRepository,
	episodeService *episode.Service,
	replacement ReplacementActions,
	tasks episodeTaskStarter,
) *EpisodeHandler {
	return &EpisodeHandler{
		subscriptionRepo: subscriptionRepo,
		episodeRepo:      episodeRepo,
		episodeService:   episodeService,
		replacement:      replacement,
		tasks:            tasks,
	}
}

func (h *EpisodeHandler) List(c *gin.Context) {
	subscriptionID, ok := parsePositiveID(c, "id", "subscription")
	if !ok {
		return
	}
	if !h.requireSubscription(c, subscriptionID) {
		return
	}

	ledgers, err := h.episodeRepo.ListWithCandidateCounts(subscriptionID)
	if err != nil {
		episodeAPIError(c, http.StatusInternalServerError, "Failed to list episodes", "")
		return
	}
	items := make([]EpisodeListItem, len(ledgers))
	for index, ledger := range ledgers {
		items[index] = EpisodeListItem{
			SubscriptionEpisode:          ledger.SubscriptionEpisode,
			ActionRequiredCandidateCount: ledger.ActionRequiredCandidateCount,
		}
	}
	episodeAPISuccess(c, items)
}

func (h *EpisodeHandler) UpdateStatus(c *gin.Context) {
	subscriptionID, ok := parsePositiveID(c, "id", "subscription")
	if !ok {
		return
	}
	var request UpdateEpisodeStatusRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		episodeAPIError(c, http.StatusBadRequest, "Invalid request body", "")
		return
	}
	if err := validateStatusRequest(request); err != nil {
		episodeAPIError(c, http.StatusBadRequest, err.Error(), "")
		return
	}
	if !h.requireSubscription(c, subscriptionID) {
		return
	}
	if h.episodeService == nil {
		episodeAPIError(c, http.StatusInternalServerError, "Episode service is unavailable", "")
		return
	}
	if err := h.episodeService.SetUserStatus(subscriptionID, request.Episodes, request.Status); err != nil {
		if errors.Is(err, repository.ErrActiveDownloadMustBeResolved) {
			episodeAPIError(c, http.StatusConflict, "Active download must be resolved before marking the episode missing", "active_download_must_be_resolved")
			return
		}
		episodeAPIError(c, http.StatusInternalServerError, "Failed to update episode status", "")
		return
	}
	episodeAPISuccess(c, gin.H{"episodes": request.Episodes, "status": request.Status})
}

func (h *EpisodeHandler) ListCandidates(c *gin.Context) {
	subscriptionID, ok := parsePositiveID(c, "id", "subscription")
	if !ok {
		return
	}
	episodeNumber, ok := parseEpisodeNumber(c)
	if !ok {
		return
	}
	offset, limit, ok := parseCandidatePagination(c)
	if !ok {
		return
	}
	if _, err := h.episodeRepo.GetBySubscriptionAndEpisode(subscriptionID, episodeNumber); err != nil {
		handleEpisodeLookupError(c, err, "Episode not found")
		return
	}
	candidates, err := h.episodeRepo.ListCandidatesByScope(subscriptionID, episodeNumber, offset, limit)
	if err != nil {
		episodeAPIError(c, http.StatusInternalServerError, "Failed to list episode candidates", "")
		return
	}
	episodeAPISuccess(c, candidates)
}

func (h *EpisodeHandler) KeepCandidate(c *gin.Context) {
	subscriptionID, ok := parsePositiveID(c, "id", "subscription")
	if !ok {
		return
	}
	episodeNumber, ok := parseEpisodeNumber(c)
	if !ok {
		return
	}
	candidateID, ok := parsePositiveID(c, "candidate_id", "candidate")
	if !ok {
		return
	}
	candidate, err := h.episodeRepo.KeepCandidate(subscriptionID, episodeNumber, candidateID)
	if err != nil {
		if errors.Is(err, repository.ErrCandidateStateConflict) {
			episodeAPIError(c, http.StatusConflict, "Candidate can only be kept while pending", "candidate_state_conflict")
			return
		}
		handleEpisodeLookupError(c, err, "Candidate not found")
		return
	}
	episodeAPISuccess(c, candidate)
}

func (h *EpisodeHandler) Replace(c *gin.Context) {
	h.startReplacementTask(c, []string{model.CandidateStatusPending, model.CandidateStatusFailed}, true, "替换剧集资源", h.callReplace)
}

func (h *EpisodeHandler) RetryCleanup(c *gin.Context) {
	h.startReplacementTask(c, []string{model.CandidateStatusAcceptedCleanupFailed}, false, "重试替换清理", h.callRetryCleanup)
}

func (h *EpisodeHandler) startReplacementTask(c *gin.Context, allowedStatuses []string, rejectSiblingReplacement bool, name string, action func(context.Context, uint) error) {
	subscriptionID, ok := parsePositiveID(c, "id", "subscription")
	if !ok {
		return
	}
	episodeNumber, ok := parseEpisodeNumber(c)
	if !ok {
		return
	}
	candidateID, ok := parsePositiveID(c, "candidate_id", "candidate")
	if !ok {
		return
	}
	if h.replacement == nil || h.tasks == nil {
		episodeAPIError(c, http.StatusInternalServerError, "Replacement service is unavailable", "")
		return
	}
	candidate, ok := h.requireCandidateScope(c, subscriptionID, episodeNumber, candidateID)
	if !ok {
		return
	}
	if !candidateStatusAllowed(candidate.Status, allowedStatuses) {
		episodeAPIError(c, http.StatusConflict, "Candidate state does not allow this action", "candidate_state_conflict")
		return
	}
	if rejectSiblingReplacement && !h.requireNoSiblingReplacement(c, subscriptionID, episodeNumber, candidateID) {
		return
	}

	started, err := h.tasks.StartTask(task.TaskTypeReplacement, subscriptionID, name, func(ctx context.Context, _ *task.Task) error {
		return action(ctx, candidateID)
	})
	if err != nil {
		episodeAPIError(c, http.StatusInternalServerError, "Failed to create replacement task", "")
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"code":    0,
		"message": "Accepted",
		"data": gin.H{
			"task_id": started.ID,
			"status":  task.TaskStatusRunning,
		},
	})
}

func (h *EpisodeHandler) requireNoSiblingReplacement(c *gin.Context, subscriptionID uint, episodeNumber int, candidateID uint) bool {
	for offset := 0; ; offset += repository.MaxEpisodeCandidateLimit {
		candidates, err := h.episodeRepo.ListCandidatesByScope(subscriptionID, episodeNumber, offset, repository.MaxEpisodeCandidateLimit)
		if err != nil {
			episodeAPIError(c, http.StatusInternalServerError, "Failed to inspect replacement state", "")
			return false
		}
		for _, candidate := range candidates {
			if candidate.ID != candidateID && candidate.Status == model.CandidateStatusReplacing {
				episodeAPIError(c, http.StatusConflict, "Another candidate replacement is already in progress", "replacement_in_progress")
				return false
			}
		}
		if len(candidates) < repository.MaxEpisodeCandidateLimit {
			return true
		}
	}
}

func (h *EpisodeHandler) requireCandidateScope(c *gin.Context, subscriptionID uint, episodeNumber int, candidateID uint) (*model.EpisodeResourceCandidate, bool) {
	if h.episodeRepo == nil {
		episodeAPIError(c, http.StatusInternalServerError, "Episode repository is unavailable", "")
		return nil, false
	}
	ledger, err := h.episodeRepo.GetBySubscriptionAndEpisode(subscriptionID, episodeNumber)
	if err != nil {
		handleEpisodeLookupError(c, err, "Candidate not found")
		return nil, false
	}
	candidate, err := h.episodeRepo.GetCandidateByID(candidateID)
	if err != nil {
		handleEpisodeLookupError(c, err, "Candidate not found")
		return nil, false
	}
	if candidate.SubscriptionEpisodeID != ledger.ID {
		episodeAPIError(c, http.StatusNotFound, "Candidate not found", "")
		return nil, false
	}
	return candidate, true
}

func (h *EpisodeHandler) callReplace(ctx context.Context, candidateID uint) error {
	return h.replacement.Replace(ctx, candidateID)
}

func (h *EpisodeHandler) callRetryCleanup(ctx context.Context, candidateID uint) error {
	return h.replacement.RetryCleanup(ctx, candidateID)
}

func candidateStatusAllowed(status string, allowed []string) bool {
	for _, candidateStatus := range allowed {
		if status == candidateStatus {
			return true
		}
	}
	return false
}

func (h *EpisodeHandler) requireSubscription(c *gin.Context, subscriptionID uint) bool {
	if h.subscriptionRepo == nil {
		episodeAPIError(c, http.StatusInternalServerError, "Subscription repository is unavailable", "")
		return false
	}
	_, err := h.subscriptionRepo.GetByID(subscriptionID)
	if err == nil {
		return true
	}
	handleEpisodeLookupError(c, err, "Subscription not found")
	return false
}

func validateStatusRequest(request UpdateEpisodeStatusRequest) error {
	if len(request.Episodes) == 0 {
		return errors.New("episodes must not be empty")
	}
	if len(request.Episodes) > maxEpisodeStatusBatchSize {
		return fmt.Errorf("episodes must not contain more than %d items", maxEpisodeStatusBatchSize)
	}
	switch request.Status {
	case model.EpisodeStatusMissing, model.EpisodeStatusMarkedDownloaded, model.EpisodeStatusIgnored:
	default:
		return errors.New("status must be missing, marked_downloaded, or ignored")
	}
	seen := make(map[int]struct{}, len(request.Episodes))
	for _, episodeNumber := range request.Episodes {
		if episodeNumber <= 0 || episodeNumber > model.MaxSubscriptionEpisodes {
			return fmt.Errorf("episode must be between 1 and %d", model.MaxSubscriptionEpisodes)
		}
		if _, exists := seen[episodeNumber]; exists {
			return fmt.Errorf("duplicate episode %d", episodeNumber)
		}
		seen[episodeNumber] = struct{}{}
	}
	return nil
}

func parseCandidatePagination(c *gin.Context) (int, int, bool) {
	limit := repository.DefaultEpisodeCandidateLimit
	if rawLimit := c.Query("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed <= 0 || parsed > repository.MaxEpisodeCandidateLimit {
			episodeAPIError(c, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", repository.MaxEpisodeCandidateLimit), "")
			return 0, 0, false
		}
		limit = parsed
	}
	offset := 0
	if rawOffset := c.Query("offset"); rawOffset != "" {
		parsed, err := strconv.Atoi(rawOffset)
		if err != nil || parsed < 0 {
			episodeAPIError(c, http.StatusBadRequest, "offset must be a non-negative integer", "")
			return 0, 0, false
		}
		offset = parsed
	}
	return offset, limit, true
}

func parseEpisodeNumber(c *gin.Context) (int, bool) {
	value, err := strconv.Atoi(c.Param("episode"))
	if err != nil || value <= 0 || value > model.MaxSubscriptionEpisodes {
		episodeAPIError(c, http.StatusBadRequest, fmt.Sprintf("Episode must be between 1 and %d", model.MaxSubscriptionEpisodes), "")
		return 0, false
	}
	return value, true
}

func parsePositiveID(c *gin.Context, parameter, resource string) (uint, bool) {
	value, err := strconv.ParseUint(c.Param(parameter), 10, 32)
	if err != nil || value == 0 {
		episodeAPIError(c, http.StatusBadRequest, "Invalid "+resource+" ID", "")
		return 0, false
	}
	return uint(value), true
}

func handleEpisodeLookupError(c *gin.Context, err error, notFoundMessage string) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		episodeAPIError(c, http.StatusNotFound, notFoundMessage, "")
		return
	}
	episodeAPIError(c, http.StatusInternalServerError, "Episode repository operation failed", "")
}

func episodeAPISuccess(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "Success", "data": data})
}

func episodeAPIError(c *gin.Context, status int, message, reason string) {
	response := gin.H{"code": status, "message": message}
	if reason != "" {
		response["reason"] = reason
	}
	c.JSON(status, response)
}
