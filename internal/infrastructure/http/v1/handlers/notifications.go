package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/notifications"
	ws "metapus/internal/infrastructure/websocket"
)

type NotificationHandler struct {
	BaseHandler *BaseHandler
	repo        notifications.Repository
}

func NewNotificationHandler(repo notifications.Repository) *NotificationHandler {
	return &NotificationHandler{
		BaseHandler: NewBaseHandler(),
		repo:        repo,
	}
}

func (h *NotificationHandler) ServeWS(c *gin.Context) {
	userIDStr := h.BaseHandler.GetUserID(c)
	tenantStr := h.BaseHandler.GetTenantID(c)

	if userIDStr == "" || tenantStr == "" {
		_ = c.Error(apperror.NewUnauthorized("unauthorized or missing tenant"))
		c.Abort()
		return
	}

	ws.ServeWS(c.Writer, c.Request, tenantStr, userIDStr)
}

func (h *NotificationHandler) List(c *gin.Context) {
	userIDStr := h.BaseHandler.GetUserID(c)
	if userIDStr == "" {
		_ = c.Error(apperror.NewUnauthorized("authentication required"))
		c.Abort()
		return
	}

	userID, err := id.Parse(userIDStr)
	if err != nil {
		_ = c.Error(apperror.NewUnauthorized("invalid user identity"))
		c.Abort()
		return
	}

	filter := &notifications.NotificationFilter{
		UserID: userID,
	}

	if unreadStr := c.Query("unreadOnly"); unreadStr == "true" {
		filter.UnreadOnly = true
	}
	if limitStr := c.Query("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			filter.Limit = limit
		}
	}
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil {
			filter.Offset = offset
		}
	}

	notifs, err := h.repo.List(c.Request.Context(), filter)
	if err != nil {
		_ = c.Error(apperror.NewInternal(err))
		c.Abort()
		return
	}

	totalUnread, err := h.repo.CountUnread(c.Request.Context(), userID)
	if err != nil {
		_ = c.Error(apperror.NewInternal(err))
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items":       notifs,
		"unreadCount": totalUnread,
	})
}

func (h *NotificationHandler) MarkAsRead(c *gin.Context) {
	userIDStr := h.BaseHandler.GetUserID(c)
	if userIDStr == "" {
		_ = c.Error(apperror.NewUnauthorized("authentication required"))
		c.Abort()
		return
	}

	userID, err := id.Parse(userIDStr)
	if err != nil {
		_ = c.Error(apperror.NewUnauthorized("invalid user identity"))
		c.Abort()
		return
	}

	notifIDStr := c.Param("id")
	notifID, err := id.Parse(notifIDStr)
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid notification ID").WithDetail("error", err.Error()))
		c.Abort()
		return
	}

	err = h.repo.MarkAsRead(c.Request.Context(), notifID, userID)
	if err != nil {
		_ = c.Error(apperror.NewInternal(err))
		c.Abort()
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *NotificationHandler) MarkAllAsRead(c *gin.Context) {
	userIDStr := h.BaseHandler.GetUserID(c)
	if userIDStr == "" {
		_ = c.Error(apperror.NewUnauthorized("authentication required"))
		c.Abort()
		return
	}

	userID, err := id.Parse(userIDStr)
	if err != nil {
		_ = c.Error(apperror.NewUnauthorized("invalid user identity"))
		c.Abort()
		return
	}

	err = h.repo.MarkAllAsRead(c.Request.Context(), userID)
	if err != nil {
		_ = c.Error(apperror.NewInternal(err))
		c.Abort()
		return
	}

	c.Status(http.StatusNoContent)
}
