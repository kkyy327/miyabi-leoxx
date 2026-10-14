package api

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/catalogue"
)

// MetaTubePostersManager exposes the MetaTube poster toggle. Kept separate
// from CatalogueManager so existing fakes of that interface keep compiling.
type MetaTubePostersManager interface {
	MetaTubePosters() catalogue.MetaTubePostersSetting
	UpdateMetaTubePosters(context.Context, catalogue.MetaTubePostersSetting) error
}

func metatubePostersGetHandler(service MetaTubePostersManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			respond(c, catalogue.MetaTubePostersSetting{}, nil)
			return
		}
		respond(c, service.MetaTubePosters(), nil)
	}
}

func metatubePostersUpdateHandler(service MetaTubePostersManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		setting, ok := bindJSON[catalogue.MetaTubePostersSetting](c)
		if !ok {
			return
		}
		if service == nil {
			respond(c, catalogue.MetaTubePostersSetting{}, nil)
			return
		}
		if err := service.UpdateMetaTubePosters(c.Request.Context(), setting); err != nil {
			c.Error(err)
			return
		}
		respond(c, service.MetaTubePosters(), nil)
	}
}
