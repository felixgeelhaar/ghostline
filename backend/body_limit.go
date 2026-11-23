package main

import (
	"bytes"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// maxAPIRequestBytes bounds every /api request body. The largest legitimate
// body is a pull request URL plus a revision SHA and a few numbers, well
// under 1 KiB; handlers that need less apply their own, tighter limit.
const maxAPIRequestBytes = 16 << 10

var errRequestTooLarge = gin.H{"error": "The request is too large."}

// refuseDeclaredOversizedBody is the cap for routes that never read a body:
// it refuses a declared size above max and leaves any body unread, so a slow
// chunked upload cannot hold the handler.
func refuseDeclaredOversizedBody(max int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > max {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, errRequestTooLarge)
			return
		}
		c.Next()
	}
}

// limitRequestBody refuses a body larger than max with 413 before any handler
// sees it. A declared size is checked without reading; a body of undeclared
// size (chunked upload) is read up to max+1 bytes, which is enough to tell,
// and an allowed body is handed on from memory unchanged.
func limitRequestBody(max int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > max {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, errRequestTooLarge)
			return
		}
		if c.Request.Body == nil || c.Request.Body == http.NoBody {
			c.Next()
			return
		}
		raw, err := io.ReadAll(io.LimitReader(c.Request.Body, max+1))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "The request body could not be read."})
			return
		}
		if int64(len(raw)) > max {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, errRequestTooLarge)
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		c.Next()
	}
}
