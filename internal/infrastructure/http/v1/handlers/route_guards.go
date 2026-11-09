package handlers

import "github.com/gin-gonic/gin"

func appendRouteGuards(guards []gin.HandlerFunc, handler gin.HandlerFunc) []gin.HandlerFunc {
	out := make([]gin.HandlerFunc, 0, len(guards)+1)
	out = append(out, guards...)
	return append(out, handler)
}
