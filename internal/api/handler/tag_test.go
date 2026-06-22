package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTagHandlerTest(t *testing.T) (*gin.Engine, repository.SubscriptionRepository, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Subscription{},
		&model.SubscriptionTag{},
		&model.SubscriptionTagRelation{},
	))

	subscriptionRepo := repository.NewSubscriptionRepository(db)
	handler := NewTagHandler(subscriptionRepo)

	router := gin.New()
	router.GET("/tags", handler.List)
	router.POST("/tags", handler.Create)
	router.PUT("/tags/:id", handler.Update)
	router.DELETE("/tags/:id", handler.Delete)
	router.GET("/subscriptions/:id/tags", handler.GetSubscriptionTags)
	router.POST("/subscriptions/:id/tags", handler.AddTagsToSubscription)
	router.DELETE("/subscriptions/:id/tags/:tag_id", handler.RemoveTagFromSubscription)

	return router, subscriptionRepo, db
}

func performTagRequest(router *gin.Engine, method, target string, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeTagResponse(t *testing.T, recorder *httptest.ResponseRecorder) struct {
	Code    int                   `json:"code"`
	Message string                `json:"message"`
	Data    model.SubscriptionTag `json:"data"`
} {
	t.Helper()
	var response struct {
		Code    int                   `json:"code"`
		Message string                `json:"message"`
		Data    model.SubscriptionTag `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func decodeTagListResponse(t *testing.T, recorder *httptest.ResponseRecorder) struct {
	Code int                     `json:"code"`
	Data []model.SubscriptionTag `json:"data"`
} {
	t.Helper()
	var response struct {
		Code int                     `json:"code"`
		Data []model.SubscriptionTag `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func createTestTag(t *testing.T, repo repository.SubscriptionRepository, name string, sortOrder int) model.SubscriptionTag {
	t.Helper()
	tag := model.SubscriptionTag{Name: name, Color: "#123456", SortOrder: sortOrder}
	require.NoError(t, repo.CreateTag(&tag))
	return tag
}

func createTestSubscription(t *testing.T, repo repository.SubscriptionRepository, name string) model.Subscription {
	t.Helper()
	subscription := model.Subscription{Name: name, RssURL: "https://example.com/" + name + ".xml", Season: 1, Enabled: true}
	require.NoError(t, repo.Create(&subscription))
	return subscription
}

func TestTagHandler_ListReturnsTagsInRepositoryOrder(t *testing.T) {
	router, repo, _ := setupTagHandlerTest(t)
	createTestTag(t, repo, "second", 20)
	createTestTag(t, repo, "first", 10)

	recorder := performTagRequest(router, http.MethodGet, "/tags", "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	response := decodeTagListResponse(t, recorder)
	require.Equal(t, 0, response.Code)
	require.Len(t, response.Data, 2)
	require.Equal(t, "first", response.Data[0].Name)
	require.Equal(t, "second", response.Data[1].Name)
}

func TestTagHandler_CreateValidatesAndRejectsDuplicateName(t *testing.T) {
	router, repo, _ := setupTagHandlerTest(t)

	recorder := performTagRequest(router, http.MethodPost, "/tags", `{"name":"new","description":"desc"}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	created := decodeTagResponse(t, recorder)
	require.Equal(t, "new", created.Data.Name)
	require.Equal(t, "#18a058", created.Data.Color)
	require.Equal(t, "desc", created.Data.Description)

	duplicate := performTagRequest(router, http.MethodPost, "/tags", `{"name":"new","color":"#ffffff"}`)
	require.Equal(t, http.StatusConflict, duplicate.Code, duplicate.Body.String())

	missingName := performTagRequest(router, http.MethodPost, "/tags", `{"color":"#ffffff"}`)
	require.Equal(t, http.StatusBadRequest, missingName.Code, missingName.Body.String())

	tag, err := repo.GetTagByName("new")
	require.NoError(t, err)
	require.Equal(t, created.Data.ID, tag.ID)
}

func TestTagHandler_UpdateHandlesSuccessDuplicateAndNotFound(t *testing.T) {
	router, repo, _ := setupTagHandlerTest(t)
	tag := createTestTag(t, repo, "old", 0)
	createTestTag(t, repo, "taken", 1)
	sortOrder := 9

	recorder := performTagRequest(router, http.MethodPut, "/tags/"+uintToString(tag.ID), `{"name":"updated","color":"#abcdef","description":"updated desc","sort_order":9}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	updated := decodeTagResponse(t, recorder)
	require.Equal(t, "updated", updated.Data.Name)
	require.Equal(t, "#abcdef", updated.Data.Color)
	require.Equal(t, "updated desc", updated.Data.Description)
	require.Equal(t, sortOrder, updated.Data.SortOrder)

	duplicate := performTagRequest(router, http.MethodPut, "/tags/"+uintToString(tag.ID), `{"name":"taken"}`)
	require.Equal(t, http.StatusConflict, duplicate.Code, duplicate.Body.String())

	notFound := performTagRequest(router, http.MethodPut, "/tags/99999", `{"name":"missing"}`)
	require.Equal(t, http.StatusNotFound, notFound.Code, notFound.Body.String())

	badID := performTagRequest(router, http.MethodPut, "/tags/not-a-number", `{"name":"bad"}`)
	require.Equal(t, http.StatusBadRequest, badID.Code, badID.Body.String())
}

func TestTagHandler_DeleteRemovesTagAndRelations(t *testing.T) {
	router, repo, db := setupTagHandlerTest(t)
	subscription := createTestSubscription(t, repo, "delete-anime")
	tag := createTestTag(t, repo, "delete-me", 0)
	require.NoError(t, repo.AddTagsToSubscription(subscription.ID, []uint{tag.ID}))

	recorder := performTagRequest(router, http.MethodDelete, "/tags/"+uintToString(tag.ID), "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	_, err := repo.GetTagByID(tag.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	var relationCount int64
	require.NoError(t, db.Model(&model.SubscriptionTagRelation{}).Where("tag_id = ?", tag.ID).Count(&relationCount).Error)
	require.Zero(t, relationCount)

	notFound := performTagRequest(router, http.MethodDelete, "/tags/99999", "")
	require.Equal(t, http.StatusNotFound, notFound.Code, notFound.Body.String())

	badID := performTagRequest(router, http.MethodDelete, "/tags/bad", "")
	require.Equal(t, http.StatusBadRequest, badID.Code, badID.Body.String())
}

func TestTagHandler_GetSubscriptionTagsHandlesSuccessAndMissingSubscription(t *testing.T) {
	router, repo, _ := setupTagHandlerTest(t)
	subscription := createTestSubscription(t, repo, "tagged-anime")
	second := createTestTag(t, repo, "second", 20)
	first := createTestTag(t, repo, "first", 10)
	require.NoError(t, repo.AddTagsToSubscription(subscription.ID, []uint{second.ID, first.ID}))

	recorder := performTagRequest(router, http.MethodGet, "/subscriptions/"+uintToString(subscription.ID)+"/tags", "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	response := decodeTagListResponse(t, recorder)
	require.Len(t, response.Data, 2)
	require.Equal(t, "first", response.Data[0].Name)
	require.Equal(t, "second", response.Data[1].Name)

	notFound := performTagRequest(router, http.MethodGet, "/subscriptions/99999/tags", "")
	require.Equal(t, http.StatusNotFound, notFound.Code, notFound.Body.String())

	badID := performTagRequest(router, http.MethodGet, "/subscriptions/bad/tags", "")
	require.Equal(t, http.StatusBadRequest, badID.Code, badID.Body.String())
}

func TestTagHandler_AddTagsToSubscriptionValidatesAndIsIdempotent(t *testing.T) {
	router, repo, db := setupTagHandlerTest(t)
	subscription := createTestSubscription(t, repo, "add-tags-anime")
	tagOne := createTestTag(t, repo, "tag-one", 0)
	tagTwo := createTestTag(t, repo, "tag-two", 0)
	body := `{"tag_ids":[` + uintToString(tagOne.ID) + `,` + uintToString(tagTwo.ID) + `]}`

	recorder := performTagRequest(router, http.MethodPost, "/subscriptions/"+uintToString(subscription.ID)+"/tags", body)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	duplicate := performTagRequest(router, http.MethodPost, "/subscriptions/"+uintToString(subscription.ID)+"/tags", body)
	require.Equal(t, http.StatusOK, duplicate.Code, duplicate.Body.String())

	var relationCount int64
	require.NoError(t, db.Model(&model.SubscriptionTagRelation{}).Where("subscription_id = ?", subscription.ID).Count(&relationCount).Error)
	require.Equal(t, int64(2), relationCount)

	emptyIDs := performTagRequest(router, http.MethodPost, "/subscriptions/"+uintToString(subscription.ID)+"/tags", `{"tag_ids":[]}`)
	require.Equal(t, http.StatusBadRequest, emptyIDs.Code, emptyIDs.Body.String())

	missingBody := performTagRequest(router, http.MethodPost, "/subscriptions/"+uintToString(subscription.ID)+"/tags", `{}`)
	require.Equal(t, http.StatusBadRequest, missingBody.Code, missingBody.Body.String())

	notFound := performTagRequest(router, http.MethodPost, "/subscriptions/99999/tags", body)
	require.Equal(t, http.StatusNotFound, notFound.Code, notFound.Body.String())

	badID := performTagRequest(router, http.MethodPost, "/subscriptions/bad/tags", body)
	require.Equal(t, http.StatusBadRequest, badID.Code, badID.Body.String())
}

func TestTagHandler_RemoveTagFromSubscriptionRemovesOnlyRequestedTag(t *testing.T) {
	router, repo, _ := setupTagHandlerTest(t)
	subscription := createTestSubscription(t, repo, "remove-tags-anime")
	removed := createTestTag(t, repo, "removed", 0)
	kept := createTestTag(t, repo, "kept", 0)
	require.NoError(t, repo.AddTagsToSubscription(subscription.ID, []uint{removed.ID, kept.ID}))

	recorder := performTagRequest(router, http.MethodDelete, "/subscriptions/"+uintToString(subscription.ID)+"/tags/"+uintToString(removed.ID), "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	tags, err := repo.GetSubscriptionTags(subscription.ID)
	require.NoError(t, err)
	require.Len(t, tags, 1)
	require.Equal(t, kept.ID, tags[0].ID)

	notFound := performTagRequest(router, http.MethodDelete, "/subscriptions/99999/tags/"+uintToString(kept.ID), "")
	require.Equal(t, http.StatusNotFound, notFound.Code, notFound.Body.String())

	badSubscriptionID := performTagRequest(router, http.MethodDelete, "/subscriptions/bad/tags/"+uintToString(kept.ID), "")
	require.Equal(t, http.StatusBadRequest, badSubscriptionID.Code, badSubscriptionID.Body.String())

	badTagID := performTagRequest(router, http.MethodDelete, "/subscriptions/"+uintToString(subscription.ID)+"/tags/bad", "")
	require.Equal(t, http.StatusBadRequest, badTagID.Code, badTagID.Body.String())
}

func uintToString(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}
