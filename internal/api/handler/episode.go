package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/episode"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type EpisodeHandler struct {
	subscriptionRepo repository.SubscriptionRepository
	episodeRepo      repository.EpisodeRepository
	episodeService   *episode.Service
}

type EpisodeListItem struct {
	model.SubscriptionEpisode
	ActionRequiredCandidateCount int64 `json:"action_required_candidate_count"`
}

type UpdateEpisodeStatusRequest struct {
	Episodes []int  `json:"episodes"`
	Status   string `json:"status"`
}

func NewEpisodeHandler(
	subscriptionRepo repository.SubscriptionRepository,
	episodeRepo repository.EpisodeRepository,
	episodeService *episode.Service,
) *EpisodeHandler {
	return &EpisodeHandler{
		subscriptionRepo: subscriptionRepo,
		episodeRepo:      episodeRepo,
		episodeService:   episodeService,
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
	if _, err := h.episodeRepo.GetBySubscriptionAndEpisode(subscriptionID, episodeNumber); err != nil {
		handleEpisodeLookupError(c, err, "Episode not found")
		return
	}
	candidates, err := h.episodeRepo.ListCandidatesByScope(subscriptionID, episodeNumber)
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
		handleEpisodeLookupError(c, err, "Candidate not found")
		return
	}
	episodeAPISuccess(c, candidate)
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
