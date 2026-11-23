package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// maxAPIRequestBytes bounds every /api request body. The largest legitimate
// body is a pull request URL plus a revision SHA and a few numbers, well
// under 1 KiB; handlers that need less apply their own, tighter limit.
const maxAPIRequestBytes = 16 << 10

// limitRequestBody refuses a body that declares a size above max before any
// handler reads it, and caps a body of undeclared size (chunked upload) so a
// handler's JSON decoding fails once it passes max instead of allocating it.
func limitRequestBody(max int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > max {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "The request is too large."})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, max)
		c.Next()
	}
}
