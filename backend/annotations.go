package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	annotationPageSize = 100
)

var safeAnnotationPath = regexp.MustCompile(`^[^\\\x00]+$`)

type checkAnnotation struct {
	Path      string `json:"path"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Level     string `json:"level"`
	Title     string `json:"title,omitempty"`
	Message   string `json:"message"`
	Details   string `json:"details,omitempty"`
	SourceURL string `json:"sourceUrl,omitempty"`
}

type checkAnnotationPage struct {
	RevisionSHA string            `json:"revisionSha"`
	CheckRunID  int64             `json:"checkRunId"`
	CheckRunURL string            `json:"checkRunUrl,omitempty"`
	Page        int               `json:"page"`
	PageSize    int               `json:"pageSize"`
	HasMore     bool              `json:"hasMore"`
	Items       []checkAnnotation `json:"items"`
}

type githubCheckRun struct {
	ID      int64  `json:"id"`
	HeadSHA string `json:"head_sha"`
	HTMLURL string `json:"html_url"`
}

type githubCheckAnnotation struct {
	Path            string `json:"path"`
	StartLine       int    `json:"start_line"`
	EndLine         int    `json:"end_line"`
	AnnotationLevel string `json:"annotation_level"`
	Title           string `json:"title"`
	Message         string `json:"message"`
	RawDetails      string `json:"raw_details"`
}

func loadCheckAnnotations(ctx context.Context, gh *githubClient, owner, repo string, sha string, runID int64, page int) (checkAnnotationPage, error) {
	result := checkAnnotationPage{RevisionSHA: sha, CheckRunID: runID, Page: page, PageSize: annotationPageSize, Items: []checkAnnotation{}}
	if err := ensurePublicRepository(ctx, gh, owner, repo); err != nil {
		return result, err
	}
	runPath := fmt.Sprintf("/repos/%s/%s/check-runs/%d", url.PathEscape(owner), url.PathEscape(repo), runID)
	var run githubCheckRun
	if err := gh.get(ctx, runPath, &run); err != nil {
		return result, err
	}
	if run.ID != runID || run.HeadSHA != sha {
		return result, errRevisionMismatch
	}
	result.CheckRunURL = safeURL(run.HTMLURL)
	annotationsPath := fmt.Sprintf("%s/annotations?per_page=%d&page=%d", runPath, annotationPageSize, page)
	var raw []githubCheckAnnotation
	hasMore, err := gh.getWithNext(ctx, annotationsPath, &raw)
	if err != nil {
		return result, err
	}
	result.HasMore = hasMore
	for _, item := range raw {
		annotation := checkAnnotation{
			Path: item.Path, StartLine: item.StartLine, EndLine: item.EndLine,
			Level: item.AnnotationLevel, Title: item.Title, Message: item.Message,
			Details: item.RawDetails,
		}
		annotation.SourceURL = annotationSourceURL(owner, repo, sha, item.Path, item.StartLine, item.EndLine)
		result.Items = append(result.Items, annotation)
	}
	return result, nil
}

func annotationSourceURL(owner, repo, sha, filePath string, startLine, endLine int) string {
	if !fullSHA.MatchString(sha) || !safeAnnotationPath.MatchString(filePath) || path.IsAbs(filePath) || path.Clean(filePath) != filePath || filePath == "." || startLine < 1 || endLine < startLine {
		return ""
	}
	for _, segment := range strings.Split(filePath, "/") {
		if segment == "." || segment == ".." || segment == "" {
			return ""
		}
	}
	lineFragment := "L" + strconv.Itoa(startLine)
	if endLine != startLine {
		lineFragment += "-L" + strconv.Itoa(endLine)
	}
	link := url.URL{Scheme: "https", Host: "github.com", Path: fmt.Sprintf("/%s/%s/blob/%s/%s", owner, repo, sha, filePath), Fragment: lineFragment}
	return link.String()
}

func (a api) loadCheckAnnotations(c *gin.Context) {
	var body struct {
		URL        string `json:"url"`
		SHA        string `json:"sha"`
		CheckRunID int64  `json:"checkRunId"`
		Page       int    `json:"page"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, map[string]string{"error": "Provide a GitHub pull request URL, full revision SHA, and check run ID."})
		return
	}
	owner, repo, _, err := parsePRURL(body.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !fullSHA.MatchString(body.SHA) || body.CheckRunID < 1 || body.Page < 1 {
		c.JSON(http.StatusBadRequest, map[string]string{"error": "The revision, check run ID, or annotation page is invalid."})
		return
	}
	result, err := loadCheckAnnotations(c.Request.Context(), a.gh, owner, repo, body.SHA, body.CheckRunID, body.Page)
	if err == nil {
		c.JSON(http.StatusOK, result)
		return
	}
	var statusErr statusError
	switch {
	case err == errNotFound || errors.Is(err, errPrivateRepository):
		c.JSON(http.StatusNotFound, map[string]string{"error": "GitHub did not return annotations for that check run."})
	case errors.Is(err, errRepositoryVisibilityUnknown):
		c.JSON(http.StatusBadGateway, map[string]string{"error": "GitHub did not confirm that this repository is public, so annotations were not loaded."})
	case err == errRateLimit:
		c.JSON(http.StatusTooManyRequests, map[string]string{"error": "GitHub’s public API rate limit has been reached. Try again later."})
	case err == errRevisionMismatch:
		c.JSON(http.StatusConflict, map[string]string{"error": "That check run does not belong to the selected revision. Re-import the pull request before viewing its annotations."})
	case errors.As(err, &statusErr) && statusErr.code == http.StatusForbidden:
		c.JSON(http.StatusForbidden, map[string]string{"error": "GitHub denied access to this check run's annotations."})
	default:
		c.JSON(http.StatusBadGateway, map[string]string{"error": "Could not load annotations from GitHub. Try again shortly."})
	}
}
