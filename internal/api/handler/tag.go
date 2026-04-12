package handler

import (
	"net/http"
	"strconv"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// TagHandler 标签处理器
type TagHandler struct {
	repo repository.SubscriptionRepository
}

// NewTagHandler 创建标签处理器实例
func NewTagHandler(repo repository.SubscriptionRepository) *TagHandler {
	return &TagHandler{repo: repo}
}

// List 获取所有标签
// GET /api/v1/tags
func (h *TagHandler) List(c *gin.Context) {
	tags, err := h.repo.ListTags()
	if err != nil {
		logger.Error("Failed to list tags", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to get tags",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    tags,
	})
}

// CreateTagRequest 创建标签请求
type CreateTagRequest struct {
	Name        string `json:"name" binding:"required"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

// Create 创建标签
// POST /api/v1/tags
func (h *TagHandler) Create(c *gin.Context) {
	var req CreateTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("Invalid tag create request",
			"error", err.Error(),
			"client_ip", c.ClientIP())
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request body: " + err.Error(),
		})
		return
	}

	// 检查标签名称是否已存在
	existing, err := h.repo.GetTagByName(req.Name)
	if err == nil && existing != nil {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": "Tag name already exists",
		})
		return
	}

	// 设置默认颜色
	color := req.Color
	if color == "" {
		color = "#18a058"
	}

	tag := model.SubscriptionTag{
		Name:        req.Name,
		Color:       color,
		Description: req.Description,
	}

	logger.Info("Creating tag",
		"name", req.Name,
		"client_ip", c.ClientIP())

	if err := h.repo.CreateTag(&tag); err != nil {
		logger.Error("Failed to create tag",
			"name", req.Name,
			"error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to create tag",
		})
		return
	}

	logger.Info("Tag created successfully",
		"id", tag.ID,
		"name", tag.Name)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    tag,
	})
}

// UpdateTagRequest 更新标签请求
type UpdateTagRequest struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
	SortOrder   *int   `json:"sort_order"`
}

// Update 更新标签
// PUT /api/v1/tags/:id
func (h *TagHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		logger.Warn("Invalid tag ID in update request",
			"id_param", c.Param("id"),
			"error", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid tag ID",
		})
		return
	}

	existing, err := h.repo.GetTagByID(uint(id))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "Tag not found",
			})
			return
		}
		logger.Error("Failed to get tag",
			"id", id,
			"error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to get tag",
		})
		return
	}

	var req UpdateTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("Invalid tag update request",
			"id", id,
			"error", err.Error(),
			"client_ip", c.ClientIP())
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request body: " + err.Error(),
		})
		return
	}

	// 如果更新名称，检查是否与其他标签冲突
	if req.Name != "" && req.Name != existing.Name {
		other, err := h.repo.GetTagByName(req.Name)
		if err == nil && other != nil && other.ID != existing.ID {
			c.JSON(http.StatusConflict, gin.H{
				"code":    409,
				"message": "Tag name already exists",
			})
			return
		}
		existing.Name = req.Name
	}

	// 更新其他字段
	if req.Color != "" {
		existing.Color = req.Color
	}
	if req.Description != "" {
		existing.Description = req.Description
	}
	if req.SortOrder != nil {
		existing.SortOrder = *req.SortOrder
	}

	logger.Info("Updating tag",
		"id", id,
		"name", existing.Name,
		"client_ip", c.ClientIP())

	if err := h.repo.UpdateTag(existing); err != nil {
		logger.Error("Failed to update tag",
			"id", id,
			"error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to update tag",
		})
		return
	}

	logger.Info("Tag updated successfully",
		"id", id,
		"name", existing.Name)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    existing,
	})
}

// Delete 删除标签
// DELETE /api/v1/tags/:id
func (h *TagHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		logger.Warn("Invalid tag ID in delete request",
			"id_param", c.Param("id"),
			"error", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid tag ID",
		})
		return
	}

	// 检查标签是否存在
	tag, err := h.repo.GetTagByID(uint(id))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "Tag not found",
			})
			return
		}
		logger.Error("Failed to get tag",
			"id", id,
			"error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to get tag",
		})
		return
	}

	logger.Info("Deleting tag",
		"id", id,
		"name", tag.Name,
		"client_ip", c.ClientIP())

	if err := h.repo.DeleteTag(uint(id)); err != nil {
		logger.Error("Failed to delete tag",
			"id", id,
			"error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to delete tag",
		})
		return
	}

	logger.Info("Tag deleted successfully",
		"id", id,
		"name", tag.Name)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
	})
}

// GetSubscriptionTags 获取订阅的标签列表
// GET /api/v1/subscriptions/:id/tags
func (h *TagHandler) GetSubscriptionTags(c *gin.Context) {
	subscriptionID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		logger.Warn("Invalid subscription ID in get tags request",
			"id_param", c.Param("id"),
			"error", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid subscription ID",
		})
		return
	}

	// 检查订阅是否存在
	_, err = h.repo.GetByID(uint(subscriptionID))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "Subscription not found",
			})
			return
		}
		logger.Error("Failed to get subscription",
			"id", subscriptionID,
			"error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to get subscription",
		})
		return
	}

	tags, err := h.repo.GetSubscriptionTags(uint(subscriptionID))
	if err != nil {
		logger.Error("Failed to get subscription tags",
			"subscription_id", subscriptionID,
			"error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to get subscription tags",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
		"data":    tags,
	})
}

// AddTagsToSubscriptionRequest 为订阅添加标签请求
type AddTagsToSubscriptionRequest struct {
	TagIDs []uint `json:"tag_ids" binding:"required"`
}

// AddTagsToSubscription 为订阅添加标签
// POST /api/v1/subscriptions/:id/tags
func (h *TagHandler) AddTagsToSubscription(c *gin.Context) {
	subscriptionID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		logger.Warn("Invalid subscription ID in add tags request",
			"id_param", c.Param("id"),
			"error", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid subscription ID",
		})
		return
	}

	var req AddTagsToSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("Invalid add tags request",
			"subscription_id", subscriptionID,
			"error", err.Error(),
			"client_ip", c.ClientIP())
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid request body: " + err.Error(),
		})
		return
	}

	if len(req.TagIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Tag IDs are required",
		})
		return
	}

	// 检查订阅是否存在
	_, err = h.repo.GetByID(uint(subscriptionID))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "Subscription not found",
			})
			return
		}
		logger.Error("Failed to get subscription",
			"id", subscriptionID,
			"error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to get subscription",
		})
		return
	}

	logger.Info("Adding tags to subscription",
		"subscription_id", subscriptionID,
		"tag_ids", req.TagIDs,
		"client_ip", c.ClientIP())

	if err := h.repo.AddTagsToSubscription(uint(subscriptionID), req.TagIDs); err != nil {
		logger.Error("Failed to add tags to subscription",
			"subscription_id", subscriptionID,
			"error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to add tags to subscription",
		})
		return
	}

	logger.Info("Tags added to subscription successfully",
		"subscription_id", subscriptionID,
		"tag_count", len(req.TagIDs))

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
	})
}

// RemoveTagFromSubscription 从订阅移除标签
// DELETE /api/v1/subscriptions/:id/tags/:tag_id
func (h *TagHandler) RemoveTagFromSubscription(c *gin.Context) {
	subscriptionID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		logger.Warn("Invalid subscription ID in remove tag request",
			"id_param", c.Param("id"),
			"error", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid subscription ID",
		})
		return
	}

	tagID, err := strconv.ParseUint(c.Param("tag_id"), 10, 32)
	if err != nil {
		logger.Warn("Invalid tag ID in remove tag request",
			"tag_id_param", c.Param("tag_id"),
			"error", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "Invalid tag ID",
		})
		return
	}

	// 检查订阅是否存在
	_, err = h.repo.GetByID(uint(subscriptionID))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "Subscription not found",
			})
			return
		}
		logger.Error("Failed to get subscription",
			"id", subscriptionID,
			"error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to get subscription",
		})
		return
	}

	logger.Info("Removing tag from subscription",
		"subscription_id", subscriptionID,
		"tag_id", tagID,
		"client_ip", c.ClientIP())

	if err := h.repo.RemoveTagsFromSubscription(uint(subscriptionID), []uint{uint(tagID)}); err != nil {
		logger.Error("Failed to remove tag from subscription",
			"subscription_id", subscriptionID,
			"tag_id", tagID,
			"error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "Failed to remove tag from subscription",
		})
		return
	}

	logger.Info("Tag removed from subscription successfully",
		"subscription_id", subscriptionID,
		"tag_id", tagID)

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Success",
	})
}
