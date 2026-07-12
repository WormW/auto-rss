package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/episode"
	"github.com/WormW/auto-rss/internal/service/task"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type episodeAPIResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Reason  string          `json:"reason"`
	Data    json.RawMessage `json:"data"`
}

func setupEpisodeHandlerTest(t *testing.T) (*gorm.DB, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{},
		&model.Download{},
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
	))

	subscriptionRepo := repository.NewSubscriptionRepository(db)
	episodeRepo := repository.NewEpisodeRepository(db)
	handler := NewEpisodeHandler(subscriptionRepo, episodeRepo, episode.NewService(episodeRepo), nil)
	router := gin.New()
	router.GET("/subscriptions/:id/episodes", handler.List)
	router.PUT("/subscriptions/:id/episodes/status", handler.UpdateStatus)
	router.GET("/subscriptions/:id/episodes/:episode/candidates", handler.ListCandidates)
	router.POST("/subscriptions/:id/episodes/:episode/candidates/:candidate_id/keep", handler.KeepCandidate)
	return db, router
}

type fakeReplacementActions struct {
	mu                sync.Mutex
	prepareReplaceIDs []uint
	replaceIDs        []uint
	prepareCleanupIDs []uint
	cleanupIDs        []uint
	prepareReplaceErr error
	replaceErr        error
	prepareCleanupErr error
	cleanupErr        error
}

func (f *fakeReplacementActions) PrepareReplace(candidateID uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepareReplaceIDs = append(f.prepareReplaceIDs, candidateID)
	return f.prepareReplaceErr
}

func (f *fakeReplacementActions) ContinueReplace(_ context.Context, candidateID uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replaceIDs = append(f.replaceIDs, candidateID)
	return f.replaceErr
}

func (f *fakeReplacementActions) PrepareRetryCleanup(candidateID uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepareCleanupIDs = append(f.prepareCleanupIDs, candidateID)
	return f.prepareCleanupErr
}

func (f *fakeReplacementActions) ContinueRetryCleanup(_ context.Context, candidateID uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleanupIDs = append(f.cleanupIDs, candidateID)
	return f.cleanupErr
}

func (f *fakeReplacementActions) prepareReplaceCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.prepareReplaceIDs)
}

func (f *fakeReplacementActions) lastReplaceID() uint {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.replaceIDs) == 0 {
		return 0
	}
	return f.replaceIDs[len(f.replaceIDs)-1]
}

func (f *fakeReplacementActions) lastCleanupID() uint {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.cleanupIDs) == 0 {
		return 0
	}
	return f.cleanupIDs[len(f.cleanupIDs)-1]
}

type fakeEpisodeTaskStarter struct {
	startErr error
	started  chan *task.Task
	results  chan error
}

func newFakeEpisodeTaskStarter() *fakeEpisodeTaskStarter {
	return &fakeEpisodeTaskStarter{
		started: make(chan *task.Task, 8),
		results: make(chan error, 8),
	}
}

func (f *fakeEpisodeTaskStarter) StartPreparedTask(taskType task.TaskType, subscriptionID uint, name string, prepare func() error, fn func(context.Context, *task.Task) error) (*task.Task, error) {
	if f.startErr != nil {
		return nil, f.startErr
	}
	if err := prepare(); err != nil {
		return nil, err
	}
	started := &task.Task{
		ID:             fmt.Sprintf("%s-test", taskType),
		Type:           taskType,
		Status:         task.TaskStatusRunning,
		SubscriptionID: subscriptionID,
		Name:           name,
	}
	f.started <- started
	go func() {
		f.results <- fn(context.Background(), started)
	}()
	return started, nil
}

type replacementHandlerFixture struct {
	db          *gorm.DB
	router      *gin.Engine
	replacement *fakeReplacementActions
	tasks       *fakeEpisodeTaskStarter
}

func newReplacementHandlerFixture(t *testing.T, replacement ReplacementActions) *replacementHandlerFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{},
		&model.Download{},
		&model.SubscriptionEpisode{},
		&model.EpisodeResourceCandidate{},
	))
	return newReplacementHandlerFixtureWithDB(t, db, replacement)
}

func newReplacementHandlerFixtureWithDB(t *testing.T, db *gorm.DB, replacement ReplacementActions) *replacementHandlerFixture {
	t.Helper()
	replacementFake, _ := replacement.(*fakeReplacementActions)
	tasks := newFakeEpisodeTaskStarter()
	episodeRepo := repository.NewEpisodeRepository(db)
	h := newEpisodeHandler(
		repository.NewSubscriptionRepository(db),
		episodeRepo,
		episode.NewService(episodeRepo),
		replacement,
		tasks,
	)
	router := gin.New()
	router.POST("/subscriptions/:id/episodes/:episode/candidates/:candidate_id/replace", h.Replace)
	router.POST("/subscriptions/:id/episodes/:episode/candidates/:candidate_id/retry-cleanup", h.RetryCleanup)
	return &replacementHandlerFixture{db: db, router: router, replacement: replacementFake, tasks: tasks}
}

func (fx *replacementHandlerFixture) seedCandidate(t *testing.T, subscriptionID uint, episodeNumber int, status string) model.EpisodeResourceCandidate {
	t.Helper()
	ledger := model.SubscriptionEpisode{
		SubscriptionID: subscriptionID,
		Episode:        episodeNumber,
		Status:         model.EpisodeStatusDownloaded,
		StatusSource:   model.EpisodeStatusSourceAutomatic,
	}
	require.NoError(t, fx.db.Create(&ledger).Error)
	candidate := model.EpisodeResourceCandidate{
		SubscriptionEpisodeID: ledger.ID,
		ResourceKey:           fmt.Sprintf("hash:%d:%d:%s", subscriptionID, episodeNumber, status),
		Status:                status,
	}
	require.NoError(t, fx.db.Create(&candidate).Error)
	return candidate
}

func awaitTaskResult(t *testing.T, results <-chan error) error {
	t.Helper()
	select {
	case err := <-results:
		return err
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for replacement task")
		return nil
	}
}

func TestEpisodeHandlerReplaceStartsReplacementTask(t *testing.T) {
	for _, status := range []string{model.CandidateStatusPending, model.CandidateStatusFailed} {
		t.Run(status, func(t *testing.T) {
			replacement := &fakeReplacementActions{}
			fx := newReplacementHandlerFixture(t, replacement)
			sub := seedEpisodeSubscription(t, fx.db, 1)
			candidate := fx.seedCandidate(t, sub.ID, 1, status)

			recorder := performEpisodeRequest(fx.router, http.MethodPost, fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/%d/replace", sub.ID, candidate.ID), "")
			require.Equal(t, http.StatusAccepted, recorder.Code, recorder.Body.String())
			response := decodeEpisodeResponse(t, recorder)
			var data struct {
				TaskID string `json:"task_id"`
			}
			require.NoError(t, json.Unmarshal(response.Data, &data))
			assert.NotEmpty(t, data.TaskID)
			require.NoError(t, awaitTaskResult(t, fx.tasks.results))
			assert.Equal(t, candidate.ID, replacement.lastReplaceID())
		})
	}
}

func TestEpisodeHandlerReplaceMapsDisallowedStatusesToConflict(t *testing.T) {
	for _, status := range []string{
		model.CandidateStatusReplacing,
		model.CandidateStatusAcceptedCleanupFailed,
		model.CandidateStatusAccepted,
		model.CandidateStatusKeptExisting,
	} {
		t.Run(status, func(t *testing.T) {
			replacement := &fakeReplacementActions{}
			fx := newReplacementHandlerFixture(t, replacement)
			sub := seedEpisodeSubscription(t, fx.db, 1)
			candidate := fx.seedCandidate(t, sub.ID, 1, status)

			recorder := performEpisodeRequest(fx.router, http.MethodPost, fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/%d/replace", sub.ID, candidate.ID), "")
			assert.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
			assert.EqualValues(t, 0, replacement.lastReplaceID())
		})
	}
}

func TestEpisodeHandlerReplaceConflictsWithReplacingCandidateInSameEpisode(t *testing.T) {
	replacement := &fakeReplacementActions{prepareReplaceErr: episode.ErrReplacementInProgress}
	fx := newReplacementHandlerFixture(t, replacement)
	sub := seedEpisodeSubscription(t, fx.db, 1)
	target := fx.seedCandidate(t, sub.ID, 1, model.CandidateStatusPending)
	sibling := model.EpisodeResourceCandidate{
		SubscriptionEpisodeID: target.SubscriptionEpisodeID,
		ResourceKey:           "hash:replacing-sibling",
		Status:                model.CandidateStatusReplacing,
	}
	require.NoError(t, fx.db.Create(&sibling).Error)

	recorder := performEpisodeRequest(fx.router, http.MethodPost, fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/%d/replace", sub.ID, target.ID), "")
	assert.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	assert.EqualValues(t, 0, replacement.lastReplaceID())
}

func TestEpisodeHandlerReplacePrepareConflictReturns409WithoutTask(t *testing.T) {
	replacement := &fakeReplacementActions{prepareReplaceErr: episode.ErrReplacementInProgress}
	fx := newReplacementHandlerFixture(t, replacement)
	sub := seedEpisodeSubscription(t, fx.db, 1)
	candidate := fx.seedCandidate(t, sub.ID, 1, model.CandidateStatusPending)

	recorder := performEpisodeRequest(fx.router, http.MethodPost, fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/%d/replace", sub.ID, candidate.ID), "")
	assert.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	assert.Equal(t, 1, replacement.prepareReplaceCount())
	assert.EqualValues(t, 0, replacement.lastReplaceID())
	select {
	case <-fx.tasks.started:
		t.Fatal("claim conflict must not create a task")
	default:
	}
}

type racingReplacementActions struct {
	repo          repository.EpisodeRepository
	prepareCalled chan struct{}
	otherClaimed  chan struct{}
}

func (a *racingReplacementActions) PrepareReplace(candidateID uint) error {
	close(a.prepareCalled)
	<-a.otherClaimed
	_, err := a.repo.ClaimCandidateForReplacement(candidateID)
	return err
}

func (*racingReplacementActions) ContinueReplace(context.Context, uint) error { return nil }
func (*racingReplacementActions) PrepareRetryCleanup(uint) error              { return nil }
func (*racingReplacementActions) ContinueRetryCleanup(context.Context, uint) error {
	return nil
}

func TestEpisodeHandlerReplaceCASConflictAfterScopePreflightReturns409(t *testing.T) {
	prepareCalled := make(chan struct{})
	otherClaimed := make(chan struct{})
	claimResult := make(chan error, 1)
	fx := newReplacementHandlerFixture(t, nil)
	repo := repository.NewEpisodeRepository(fx.db)
	actions := &racingReplacementActions{repo: repo, prepareCalled: prepareCalled, otherClaimed: otherClaimed}
	fx = newReplacementHandlerFixtureWithDB(t, fx.db, actions)
	sub := seedEpisodeSubscription(t, fx.db, 1)
	candidate := fx.seedCandidate(t, sub.ID, 1, model.CandidateStatusPending)
	go func() {
		<-prepareCalled
		_, err := repo.ClaimCandidateForReplacement(candidate.ID)
		claimResult <- err
		close(otherClaimed)
	}()

	recorder := performEpisodeRequest(fx.router, http.MethodPost, fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/%d/replace", sub.ID, candidate.ID), "")
	require.NoError(t, <-claimResult)
	assert.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	select {
	case <-fx.tasks.started:
		t.Fatal("CAS conflict must not create a task")
	default:
	}
}

func TestEpisodeHandlerRetryCleanupOnlyAcceptsCleanupFailedCandidate(t *testing.T) {
	replacement := &fakeReplacementActions{}
	fx := newReplacementHandlerFixture(t, replacement)
	sub := seedEpisodeSubscription(t, fx.db, 2)
	pending := fx.seedCandidate(t, sub.ID, 1, model.CandidateStatusPending)
	cleanupFailed := fx.seedCandidate(t, sub.ID, 2, model.CandidateStatusAcceptedCleanupFailed)

	rejected := performEpisodeRequest(fx.router, http.MethodPost, fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/%d/retry-cleanup", sub.ID, pending.ID), "")
	assert.Equal(t, http.StatusConflict, rejected.Code, rejected.Body.String())

	accepted := performEpisodeRequest(fx.router, http.MethodPost, fmt.Sprintf("/subscriptions/%d/episodes/2/candidates/%d/retry-cleanup", sub.ID, cleanupFailed.ID), "")
	require.Equal(t, http.StatusAccepted, accepted.Code, accepted.Body.String())
	require.NoError(t, awaitTaskResult(t, fx.tasks.results))
	assert.Equal(t, cleanupFailed.ID, replacement.lastCleanupID())
}

type exclusiveCleanupActions struct {
	mu      sync.Mutex
	claimed bool
	release chan struct{}
}

func (*exclusiveCleanupActions) PrepareReplace(uint) error { return nil }
func (*exclusiveCleanupActions) ContinueReplace(context.Context, uint) error {
	return nil
}
func (a *exclusiveCleanupActions) PrepareRetryCleanup(uint) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.claimed {
		return repository.ErrCandidateStateConflict
	}
	a.claimed = true
	return nil
}
func (a *exclusiveCleanupActions) ContinueRetryCleanup(context.Context, uint) error {
	<-a.release
	return nil
}

func TestEpisodeHandlerConcurrentRetryCleanupReturnsOneAcceptedAndOneConflict(t *testing.T) {
	actions := &exclusiveCleanupActions{release: make(chan struct{})}
	fx := newReplacementHandlerFixture(t, actions)
	sub := seedEpisodeSubscription(t, fx.db, 1)
	candidate := fx.seedCandidate(t, sub.ID, 1, model.CandidateStatusAcceptedCleanupFailed)
	path := fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/%d/retry-cleanup", sub.ID, candidate.ID)
	start := make(chan struct{})
	responses := make(chan int, 2)
	for range 2 {
		go func() {
			<-start
			responses <- performEpisodeRequest(fx.router, http.MethodPost, path, "").Code
		}()
	}
	close(start)
	statuses := []int{<-responses, <-responses}
	close(actions.release)
	require.NoError(t, awaitTaskResult(t, fx.tasks.results))
	assert.ElementsMatch(t, []int{http.StatusAccepted, http.StatusConflict}, statuses)
}

func TestEpisodeHandlerRetryCleanupBusyAfterClaimReturnsConflict(t *testing.T) {
	replacement := &fakeReplacementActions{}
	fx := newReplacementHandlerFixture(t, replacement)
	fx.tasks.startErr = task.ErrTaskRunning
	sub := seedEpisodeSubscription(t, fx.db, 1)
	candidate := fx.seedCandidate(t, sub.ID, 1, model.CandidateStatusAcceptedCleanupFailed)
	require.NoError(t, fx.db.Model(&candidate).Update("replacement_stage", episode.ReplacementStageCleanupQueued).Error)

	recorder := performEpisodeRequest(fx.router, http.MethodPost, fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/%d/retry-cleanup", sub.ID, candidate.ID), "")
	assert.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	assert.Empty(t, replacement.prepareCleanupIDs)
}

func TestEpisodeHandlerReplacementActionsValidateIDsAndStrictScope(t *testing.T) {
	replacement := &fakeReplacementActions{}
	fx := newReplacementHandlerFixture(t, replacement)
	sub := seedEpisodeSubscription(t, fx.db, 2)
	otherSub := seedEpisodeSubscription(t, fx.db, 1)
	candidate := fx.seedCandidate(t, sub.ID, 1, model.CandidateStatusPending)

	tests := []struct {
		name   string
		path   string
		status int
	}{
		{name: "malformed subscription", path: fmt.Sprintf("/subscriptions/nope/episodes/1/candidates/%d/replace", candidate.ID), status: http.StatusBadRequest},
		{name: "malformed episode", path: fmt.Sprintf("/subscriptions/%d/episodes/nope/candidates/%d/replace", sub.ID, candidate.ID), status: http.StatusBadRequest},
		{name: "malformed candidate", path: fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/nope/replace", sub.ID), status: http.StatusBadRequest},
		{name: "wrong episode", path: fmt.Sprintf("/subscriptions/%d/episodes/2/candidates/%d/replace", sub.ID, candidate.ID), status: http.StatusNotFound},
		{name: "wrong subscription", path: fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/%d/replace", otherSub.ID, candidate.ID), status: http.StatusNotFound},
		{name: "missing candidate", path: fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/99999/replace", sub.ID), status: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := performEpisodeRequest(fx.router, http.MethodPost, test.path, "")
			assert.Equal(t, test.status, recorder.Code, recorder.Body.String())
		})
	}
	assert.EqualValues(t, 0, replacement.lastReplaceID())
}

func TestEpisodeHandlerReplacementActionsMapMissingDependencyAndTaskCreationFailure(t *testing.T) {
	t.Run("missing replacement service", func(t *testing.T) {
		fx := newReplacementHandlerFixture(t, nil)
		sub := seedEpisodeSubscription(t, fx.db, 1)
		candidate := fx.seedCandidate(t, sub.ID, 1, model.CandidateStatusPending)

		recorder := performEpisodeRequest(fx.router, http.MethodPost, fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/%d/replace", sub.ID, candidate.ID), "")
		assert.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	})

	t.Run("task creation failure", func(t *testing.T) {
		replacement := &fakeReplacementActions{}
		fx := newReplacementHandlerFixture(t, replacement)
		fx.tasks.startErr = task.ErrTaskRunning
		sub := seedEpisodeSubscription(t, fx.db, 1)
		candidate := fx.seedCandidate(t, sub.ID, 1, model.CandidateStatusPending)

		recorder := performEpisodeRequest(fx.router, http.MethodPost, fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/%d/replace", sub.ID, candidate.ID), "")
		assert.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
		assert.Zero(t, replacement.prepareReplaceCount())
		assert.EqualValues(t, 0, replacement.lastReplaceID())
	})
}

func TestEpisodeHandlerReplacementServiceErrorFailsTaskCallback(t *testing.T) {
	replacement := &fakeReplacementActions{replaceErr: episode.ErrReplacementInProgress}
	fx := newReplacementHandlerFixture(t, replacement)
	sub := seedEpisodeSubscription(t, fx.db, 1)
	candidate := fx.seedCandidate(t, sub.ID, 1, model.CandidateStatusPending)

	recorder := performEpisodeRequest(fx.router, http.MethodPost, fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/%d/replace", sub.ID, candidate.ID), "")
	require.Equal(t, http.StatusAccepted, recorder.Code, recorder.Body.String())
	assert.ErrorIs(t, awaitTaskResult(t, fx.tasks.results), episode.ErrReplacementInProgress)
}

func performEpisodeRequest(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeEpisodeResponse(t *testing.T, recorder *httptest.ResponseRecorder) episodeAPIResponse {
	t.Helper()
	var response episodeAPIResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
	return response
}

func seedEpisodeSubscription(t *testing.T, db *gorm.DB, total int) model.Subscription {
	t.Helper()
	sub := model.Subscription{Name: "ledger", RssURL: "https://example.test/feed", TotalEpisodes: total}
	require.NoError(t, db.Create(&sub).Error)
	return sub
}

func TestEpisodeHandlerListReturnsLedgerWithBatchedActionCounts(t *testing.T) {
	db, router := setupEpisodeHandlerTest(t)
	sub := seedEpisodeSubscription(t, db, 2)
	episodes := []model.SubscriptionEpisode{
		{SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloaded, StatusSource: model.EpisodeStatusSourceAutomatic},
		{SubscriptionID: sub.ID, Episode: 2, Status: model.EpisodeStatusMissing, StatusSource: model.EpisodeStatusSourceAutomatic},
	}
	require.NoError(t, db.Create(&episodes).Error)
	for index, status := range []string{
		model.CandidateStatusPending,
		model.CandidateStatusFailed,
		model.CandidateStatusAcceptedCleanupFailed,
		model.CandidateStatusAccepted,
		model.CandidateStatusKeptExisting,
	} {
		require.NoError(t, db.Create(&model.EpisodeResourceCandidate{
			SubscriptionEpisodeID: episodes[0].ID,
			ResourceKey:           fmt.Sprintf("hash:%d", index),
			Status:                status,
		}).Error)
	}

	recorder := performEpisodeRequest(router, http.MethodGet, fmt.Sprintf("/subscriptions/%d/episodes", sub.ID), "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	response := decodeEpisodeResponse(t, recorder)
	var items []repository.EpisodeWithCandidateCount
	require.NoError(t, json.Unmarshal(response.Data, &items))
	require.Len(t, items, 2)
	assert.Equal(t, 1, items[0].Episode)
	assert.EqualValues(t, 3, items[0].ActionRequiredCandidateCount)
	assert.EqualValues(t, 0, items[1].ActionRequiredCandidateCount)
}

func TestEpisodeHandlerUpdateStatusCreatesEpisodesAndRefreshesProgress(t *testing.T) {
	db, router := setupEpisodeHandlerTest(t)
	sub := seedEpisodeSubscription(t, db, 3)
	require.NoError(t, db.Create(&model.SubscriptionEpisode{
		SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloaded, StatusSource: model.EpisodeStatusSourceAutomatic,
	}).Error)

	recorder := performEpisodeRequest(router, http.MethodPut, fmt.Sprintf("/subscriptions/%d/episodes/status", sub.ID), `{"episodes":[2,3],"status":"marked_downloaded"}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var ledgers []model.SubscriptionEpisode
	require.NoError(t, db.Where("subscription_id = ?", sub.ID).Order("episode").Find(&ledgers).Error)
	require.Len(t, ledgers, 3)
	assert.Equal(t, model.EpisodeStatusMarkedDownloaded, ledgers[1].Status)
	assert.Equal(t, model.EpisodeStatusSourceUser, ledgers[1].StatusSource)
	assert.Equal(t, model.EpisodeStatusMarkedDownloaded, ledgers[2].Status)
	require.NoError(t, db.First(&sub, sub.ID).Error)
	assert.Equal(t, 3, sub.CurrentEpisode)
	require.NotNil(t, sub.CompletedAt)

	recorder = performEpisodeRequest(router, http.MethodPut, fmt.Sprintf("/subscriptions/%d/episodes/status", sub.ID), `{"episodes":[2],"status":"missing"}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var reopened model.Subscription
	require.NoError(t, db.First(&reopened, sub.ID).Error)
	assert.Equal(t, 1, reopened.CurrentEpisode)
	assert.Nil(t, reopened.CompletedAt)
}

func TestEpisodeHandlerUpdateStatusRejectsActiveDownload(t *testing.T) {
	db, router := setupEpisodeHandlerTest(t)
	sub := seedEpisodeSubscription(t, db, 1)
	for _, status := range []string{
		model.DownloadStatusPending,
		model.DownloadStatusDownloading,
		model.DownloadStatusStalled,
		model.DownloadStatusOrganizing,
	} {
		t.Run(status, func(t *testing.T) {
			require.NoError(t, db.Where("subscription_id = ?", sub.ID).Delete(&model.SubscriptionEpisode{}).Error)
			require.NoError(t, db.Where("subscription_id = ?", sub.ID).Delete(&model.Download{}).Error)
			download := model.Download{SubscriptionID: sub.ID, Episode: 1, Title: status, TorrentURL: "magnet:" + status, TorrentHash: "hash-" + status, Status: status}
			require.NoError(t, db.Create(&download).Error)
			require.NoError(t, db.Create(&model.SubscriptionEpisode{
				SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloading,
				StatusSource: model.EpisodeStatusSourceAutomatic, ActiveDownloadID: &download.ID,
			}).Error)

			recorder := performEpisodeRequest(router, http.MethodPut, fmt.Sprintf("/subscriptions/%d/episodes/status", sub.ID), `{"episodes":[1],"status":"missing"}`)
			require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
			response := decodeEpisodeResponse(t, recorder)
			assert.Equal(t, http.StatusConflict, response.Code)
			assert.Equal(t, "active_download_must_be_resolved", response.Reason)

			var persisted model.SubscriptionEpisode
			require.NoError(t, db.Where("subscription_id = ? AND episode = ?", sub.ID, 1).First(&persisted).Error)
			assert.Equal(t, model.EpisodeStatusDownloading, persisted.Status)
			assert.NotNil(t, persisted.ActiveDownloadID)
		})
	}
}

func TestEpisodeHandlerUpdateStatusValidation(t *testing.T) {
	db, router := setupEpisodeHandlerTest(t)
	sub := seedEpisodeSubscription(t, db, 1)
	path := fmt.Sprintf("/subscriptions/%d/episodes/status", sub.ID)
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed JSON", body: `{`},
		{name: "missing episodes", body: `{"status":"ignored"}`},
		{name: "empty episodes", body: `{"episodes":[],"status":"ignored"}`},
		{name: "unknown status", body: `{"episodes":[1],"status":"downloaded"}`},
		{name: "zero episode", body: `{"episodes":[0],"status":"ignored"}`},
		{name: "negative episode", body: `{"episodes":[-1],"status":"ignored"}`},
		{name: "duplicate episode", body: `{"episodes":[1,1],"status":"ignored"}`},
		{name: "episode above limit", body: fmt.Sprintf(`{"episodes":[%d],"status":"ignored"}`, model.MaxSubscriptionEpisodes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := performEpisodeRequest(router, http.MethodPut, path, test.body)
			assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
		})
	}

	for _, path := range []string{
		"/subscriptions/not-an-id/episodes/status",
		"/subscriptions/0/episodes/status",
	} {
		recorder := performEpisodeRequest(router, http.MethodPut, path, `{"episodes":[1],"status":"ignored"}`)
		assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestEpisodeHandlerUpdateStatusRejectsOversizedBatchWithoutWriting(t *testing.T) {
	db, router := setupEpisodeHandlerTest(t)
	sub := seedEpisodeSubscription(t, db, model.MaxSubscriptionEpisodes)
	episodes := make([]int, 501)
	for index := range episodes {
		episodes[index] = index + 1
	}
	body, err := json.Marshal(UpdateEpisodeStatusRequest{Episodes: episodes, Status: model.EpisodeStatusIgnored})
	require.NoError(t, err)

	recorder := performEpisodeRequest(router, http.MethodPut, fmt.Sprintf("/subscriptions/%d/episodes/status", sub.ID), string(body))
	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())

	var count int64
	require.NoError(t, db.Model(&model.SubscriptionEpisode{}).Where("subscription_id = ?", sub.ID).Count(&count).Error)
	assert.Zero(t, count)
}

func TestEpisodeHandlerCandidateListAndKeepAreStrictlyScopedAndIdempotent(t *testing.T) {
	db, router := setupEpisodeHandlerTest(t)
	sub := seedEpisodeSubscription(t, db, 2)
	otherSub := seedEpisodeSubscription(t, db, 1)
	episodes := []model.SubscriptionEpisode{
		{SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloaded, StatusSource: model.EpisodeStatusSourceAutomatic},
		{SubscriptionID: sub.ID, Episode: 2, Status: model.EpisodeStatusDownloaded, StatusSource: model.EpisodeStatusSourceAutomatic},
		{SubscriptionID: otherSub.ID, Episode: 1, Status: model.EpisodeStatusDownloaded, StatusSource: model.EpisodeStatusSourceAutomatic},
	}
	require.NoError(t, db.Create(&episodes).Error)
	candidates := []model.EpisodeResourceCandidate{
		{SubscriptionEpisodeID: episodes[0].ID, ResourceKey: "hash:one", TorrentHash: "one", Status: model.CandidateStatusPending},
		{SubscriptionEpisodeID: episodes[1].ID, ResourceKey: "hash:two", TorrentHash: "two", Status: model.CandidateStatusPending},
		{SubscriptionEpisodeID: episodes[2].ID, ResourceKey: "hash:other", TorrentHash: "other", Status: model.CandidateStatusPending},
	}
	require.NoError(t, db.Create(&candidates).Error)

	listPath := fmt.Sprintf("/subscriptions/%d/episodes/1/candidates", sub.ID)
	recorder := performEpisodeRequest(router, http.MethodGet, listPath, "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	response := decodeEpisodeResponse(t, recorder)
	var listed []model.EpisodeResourceCandidate
	require.NoError(t, json.Unmarshal(response.Data, &listed))
	require.Len(t, listed, 1)
	assert.Equal(t, candidates[0].ID, listed[0].ID)

	keepPath := fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/%d/keep", sub.ID, candidates[0].ID)
	for range 2 {
		recorder = performEpisodeRequest(router, http.MethodPost, keepPath, "")
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		response = decodeEpisodeResponse(t, recorder)
		var kept model.EpisodeResourceCandidate
		require.NoError(t, json.Unmarshal(response.Data, &kept))
		assert.Equal(t, model.CandidateStatusKeptExisting, kept.Status)
	}

	wrongPaths := []string{
		fmt.Sprintf("/subscriptions/%d/episodes/2/candidates/%d/keep", sub.ID, candidates[0].ID),
		fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/%d/keep", otherSub.ID, candidates[0].ID),
		fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/%d/keep", sub.ID, candidates[2].ID),
	}
	for _, wrongPath := range wrongPaths {
		recorder = performEpisodeRequest(router, http.MethodPost, wrongPath, "")
		assert.Equal(t, http.StatusNotFound, recorder.Code, recorder.Body.String())
	}

	var untouched model.EpisodeResourceCandidate
	require.NoError(t, db.First(&untouched, candidates[1].ID).Error)
	assert.Equal(t, model.CandidateStatusPending, untouched.Status)
}

func TestEpisodeHandlerKeepCandidateMapsStateConflicts(t *testing.T) {
	db, router := setupEpisodeHandlerTest(t)
	sub := seedEpisodeSubscription(t, db, 1)
	ledger := model.SubscriptionEpisode{
		SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloaded, StatusSource: model.EpisodeStatusSourceAutomatic,
	}
	require.NoError(t, db.Create(&ledger).Error)

	for _, status := range []string{
		model.CandidateStatusFailed,
		model.CandidateStatusReplacing,
		model.CandidateStatusAccepted,
	} {
		t.Run(status, func(t *testing.T) {
			candidate := model.EpisodeResourceCandidate{
				SubscriptionEpisodeID: ledger.ID,
				ResourceKey:           "hash:" + status,
				Status:                status,
			}
			require.NoError(t, db.Create(&candidate).Error)
			path := fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/%d/keep", sub.ID, candidate.ID)

			recorder := performEpisodeRequest(router, http.MethodPost, path, "")
			require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
			response := decodeEpisodeResponse(t, recorder)
			assert.Equal(t, "candidate_state_conflict", response.Reason)

			var persisted model.EpisodeResourceCandidate
			require.NoError(t, db.First(&persisted, candidate.ID).Error)
			assert.Equal(t, status, persisted.Status)
		})
	}
}

func TestEpisodeHandlerListCandidatesAppliesBoundedPagination(t *testing.T) {
	db, router := setupEpisodeHandlerTest(t)
	sub := seedEpisodeSubscription(t, db, 1)
	ledger := model.SubscriptionEpisode{
		SubscriptionID: sub.ID, Episode: 1, Status: model.EpisodeStatusDownloaded, StatusSource: model.EpisodeStatusSourceAutomatic,
	}
	require.NoError(t, db.Create(&ledger).Error)
	candidates := make([]model.EpisodeResourceCandidate, 501)
	for index := range candidates {
		candidates[index] = model.EpisodeResourceCandidate{
			SubscriptionEpisodeID: ledger.ID,
			ResourceKey:           fmt.Sprintf("hash:%03d", index),
			Status:                model.CandidateStatusPending,
		}
	}
	require.NoError(t, db.CreateInBatches(&candidates, 100).Error)
	path := fmt.Sprintf("/subscriptions/%d/episodes/1/candidates", sub.ID)

	recorder := performEpisodeRequest(router, http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	response := decodeEpisodeResponse(t, recorder)
	var listed []model.EpisodeResourceCandidate
	require.NoError(t, json.Unmarshal(response.Data, &listed))
	assert.Len(t, listed, 100)
	assert.Equal(t, candidates[0].ID, listed[0].ID)

	recorder = performEpisodeRequest(router, http.MethodGet, path+"?limit=500&offset=1", "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	response = decodeEpisodeResponse(t, recorder)
	require.NoError(t, json.Unmarshal(response.Data, &listed))
	assert.Len(t, listed, 500)
	assert.Equal(t, candidates[1].ID, listed[0].ID)

	for _, query := range []string{"?limit=0", "?limit=501", "?limit=bad", "?offset=-1", "?offset=bad"} {
		recorder = performEpisodeRequest(router, http.MethodGet, path+query, "")
		assert.Equal(t, http.StatusBadRequest, recorder.Code, query+": "+recorder.Body.String())
	}
}

func TestEpisodeHandlerMapsUnknownResourcesMalformedIDsAndRepositoryErrors(t *testing.T) {
	db, router := setupEpisodeHandlerTest(t)
	sub := seedEpisodeSubscription(t, db, 1)
	ledger := model.SubscriptionEpisode{
		SubscriptionID: sub.ID, Episode: 2, Status: model.EpisodeStatusDownloaded, StatusSource: model.EpisodeStatusSourceAutomatic,
	}
	require.NoError(t, db.Create(&ledger).Error)
	candidate := model.EpisodeResourceCandidate{
		SubscriptionEpisodeID: ledger.ID, ResourceKey: "hash:db-error", Status: model.CandidateStatusPending,
	}
	require.NoError(t, db.Create(&candidate).Error)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{name: "unknown subscription list", method: http.MethodGet, path: "/subscriptions/99999/episodes", status: http.StatusNotFound},
		{name: "unknown subscription status", method: http.MethodPut, path: "/subscriptions/99999/episodes/status", body: `{"episodes":[1],"status":"ignored"}`, status: http.StatusNotFound},
		{name: "unknown episode candidates", method: http.MethodGet, path: fmt.Sprintf("/subscriptions/%d/episodes/1/candidates", sub.ID), status: http.StatusNotFound},
		{name: "malformed subscription", method: http.MethodGet, path: "/subscriptions/nope/episodes", status: http.StatusBadRequest},
		{name: "malformed episode", method: http.MethodGet, path: fmt.Sprintf("/subscriptions/%d/episodes/nope/candidates", sub.ID), status: http.StatusBadRequest},
		{name: "zero episode", method: http.MethodGet, path: fmt.Sprintf("/subscriptions/%d/episodes/0/candidates", sub.ID), status: http.StatusBadRequest},
		{name: "episode above limit", method: http.MethodGet, path: fmt.Sprintf("/subscriptions/%d/episodes/%d/candidates", sub.ID, model.MaxSubscriptionEpisodes+1), status: http.StatusBadRequest},
		{name: "malformed candidate", method: http.MethodPost, path: fmt.Sprintf("/subscriptions/%d/episodes/1/candidates/nope/keep", sub.ID), status: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := performEpisodeRequest(router, test.method, test.path, test.body)
			assert.Equal(t, test.status, recorder.Code, recorder.Body.String())
		})
	}

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	recorder := performEpisodeRequest(router, http.MethodGet, fmt.Sprintf("/subscriptions/%d/episodes", sub.ID), "")
	assert.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	recorder = performEpisodeRequest(router, http.MethodPost, fmt.Sprintf("/subscriptions/%d/episodes/2/candidates/%d/keep", sub.ID, candidate.ID), "")
	assert.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
}
