package server_handler

import (
	"strings"

	"github.com/gin-gonic/gin"
)

type ServerHandler interface {
	ServerOk(ctx *gin.Context)
}

type serverHandler struct {
	publicBaseURL string
}

// ServerOk implements ServerHandler.
func (s *serverHandler) ServerOk(ctx *gin.Context) {
	response := gin.H{
		"status": "ok",
	}

	if s.publicBaseURL != "" {
		base := s.publicBaseURL
		response["public_base_url"] = base
		response["swagger_url"] = strings.TrimRight(base, "/") + "/swagger/index.html"
		response["manager_url"] = strings.TrimRight(base, "/") + "/manager"
	}

	ctx.JSON(200, response)
}

func NewServerHandler(publicBaseURL string) ServerHandler {
	return &serverHandler{
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
	}
}
