package website

import (
	"net/http"
	"path/filepath"
	"runtime"

	"github.com/gin-gonic/gin"
)

var staticDir = func() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "static")
}()

type Provisioner interface {
	Provision() (string, error)
}

func NewHandler(p Provisioner) http.Handler {
	r := gin.Default()

	r.Static("/static", staticDir)

	r.GET("/", func(c *gin.Context) {
		websiteIndex().Render(c.Request.Context(), c.Writer)
	})

	r.GET("/loadtest", func(c *gin.Context) {
		loadTestPage().Render(c.Request.Context(), c.Writer)
	})

	r.POST("/provision", func(c *gin.Context) {
		apiKey, err := p.Provision()
		if err != nil {
			c.Status(http.StatusInternalServerError)
			websiteError(err.Error()).Render(c.Request.Context(), c.Writer)
			return
		}
		websiteKey(apiKey).Render(c.Request.Context(), c.Writer)
	})

	return r
}
