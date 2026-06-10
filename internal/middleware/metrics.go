package middleware

import (
	"strconv"
	"time"

	"github.com/afahey03/docklab/internal/services"
	"github.com/gin-gonic/gin"
)

// Metrics records request count and latency per route template and status code.
func Metrics(metrics *services.Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		metrics.ObserveHTTPRequest(c.Request.Method, route, strconv.Itoa(c.Writer.Status()), time.Since(started))
	}
}
