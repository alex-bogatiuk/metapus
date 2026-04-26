package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/auth"
	"metapus/internal/domain/notifications"
	ws "metapus/internal/infrastructure/websocket"
)

type NotificationHandler struct {
	BaseHandler   *BaseHandler
	repo          notifications.Repository
	wsTicketStore *auth.WSTicketStore
}

func NewNotificationHandler(repo notifications.Repository, ticketStore *auth.WSTicketStore) *NotificationHandler {
	return &NotificationHandler{
		BaseHandler:   NewBaseHandler(),
		repo:          repo,
		wsTicketStore: ticketStore,
	}
}

// ServeWS upgrades to WebSocket using ticket-based auth (F-05).
// Flow: client obtains ticket via POST /auth/ws-ticket, then connects with ?ticket=<ticket>.
func (h *NotificationHandler) ServeWS(c *gin.Context) {
	ticket := c.Query("ticket")
	if ticket == "" {
		_ = c.Error(apperror.NewUnauthorized("missing ticket parameter — obtain via POST /auth/ws-ticket"))
		c.Abort()
		return
	}

	userID, tenantID, ok := h.wsTicketStore.ValidateTicket(ticket)
	if !ok {
		_ = c.Error(apperror.NewUnauthorized("invalid or expired ticket"))
		c.Abort()
		return
	}

	ws.ServeWS(c.Writer, c.Request, tenantID, userID)
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

func (h *NotificationHandler) MarkAsUnread(c *gin.Context) {
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

	notifID, err := id.Parse(c.Param("id"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid notification ID").WithDetail("error", err.Error()))
		c.Abort()
		return
	}

	err = h.repo.MarkAsUnread(c.Request.Context(), notifID, userID)
	if err != nil {
		_ = c.Error(apperror.NewInternal(err))
		c.Abort()
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *NotificationHandler) Delete(c *gin.Context) {
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

	notifID, err := id.Parse(c.Param("id"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid notification ID").WithDetail("error", err.Error()))
		c.Abort()
		return
	}

	err = h.repo.Delete(c.Request.Context(), notifID, userID)
	if err != nil {
		_ = c.Error(apperror.NewInternal(err))
		c.Abort()
		return
	}

	c.Status(http.StatusNoContent)
}

