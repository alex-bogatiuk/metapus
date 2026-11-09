package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"metapus/pkg/logger"
)

func TestLoggerDoesNotRecordQueryValues(t *testing.T) {
	gin.SetMode(gin.TestMode)

	core, observed := observer.New(zap.InfoLevel)
	log := &logger.Logger{SugaredLogger: zap.New(core).Sugar()}
	router := gin.New()
	router.Use(Logger(log, nil))
	router.GET("/api/v1/ws", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	const secretTicket = "secret-ws-ticket-value"
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/ws?ticket="+secretTicket+"&tenant=tenant-a",
		nil,
	)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	entries := observed.TakeAll()
	require.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	require.Equal(t, "tenant,ticket", fields["query_keys"])
	require.NotContains(t, fields, "query")
	require.NotContains(t, entries[0].Message+entries[0].ContextMap()["query_keys"].(string), secretTicket)
}

func TestSafeQueryKeysIsStableAndOmitsValues(t *testing.T) {
	t.Parallel()

	require.Equal(t, "a,z", safeQueryKeys(map[string][]string{
		"z": {"private"},
		"a": {"also-private"},
	}))
	require.Empty(t, safeQueryKeys(nil))
}
