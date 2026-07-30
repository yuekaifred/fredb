package website

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed all:static
var staticFS embed.FS

type Provisioner interface {
	RequestProvision() (string, error)
}

func NewHandler(p Provisioner) http.Handler {
	r := gin.Default()

	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	r.StaticFS("/static", http.FS(staticSub))

	r.GET("/", func(c *gin.Context) {
		websiteIndex().Render(c.Request.Context(), c.Writer)
	})

	r.GET("/robot", func(c *gin.Context) {
		c.FileFromFS("robot.txt", http.FS(staticSub))
	})

	r.POST("/ui/provision", func(c *gin.Context) {
		apiKey, err := p.RequestProvision()
		if err != nil {
			c.Status(http.StatusInternalServerError)
			websiteError(err.Error()).Render(c.Request.Context(), c.Writer)
			return
		}
		websiteKey(apiKey).Render(c.Request.Context(), c.Writer)
	})

	r.GET("/docs", func(c *gin.Context) {
		docsPage().Render(c.Request.Context(), c.Writer)
	})

	return r
}
