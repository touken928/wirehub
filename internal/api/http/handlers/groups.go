package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/touken928/wirehub/internal/api/http/dto"
	"github.com/touken928/wirehub/internal/api/http/httputil"
	"github.com/touken928/wirehub/internal/service"
)

type createGroupRequest struct {
	Name string  `json:"name" binding:"required"`
	PosX float64 `json:"pos_x"`
	PosY float64 `json:"pos_y"`
}

type updateGroupRequest struct {
	Name            *string  `json:"name"`
	PosX            *float64 `json:"pos_x"`
	PosY            *float64 `json:"pos_y"`
	AllowIntraGroup *bool    `json:"allow_intra_group"`
}

type groupLinkRequest struct {
	FromGroupID   uint  `json:"from_group_id" binding:"required"`
	ToGroupID     uint  `json:"to_group_id" binding:"required"`
	Bidirectional *bool `json:"bidirectional"`
}

type layoutItem struct {
	ID   uint    `json:"id" binding:"required"`
	PosX float64 `json:"pos_x"`
	PosY float64 `json:"pos_y"`
}

type layoutRequest struct {
	Groups []layoutItem `json:"groups" binding:"required"`
}

func ListGroups(s *Server, c *gin.Context) {
	groups, err := s.App.ListGroups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]dto.GroupResponse, 0, len(groups))
	for _, g := range groups {
		out = append(out, dto.ToGroupResponse(g))
	}
	c.JSON(http.StatusOK, out)
}

func CreateGroup(s *Server, c *gin.Context) {
	var req createGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g, err := s.App.CreateGroup(service.CreateGroupInput{Name: req.Name, PosX: req.PosX, PosY: req.PosY})
	if err != nil {
		if service.IsRuntimeFailure(err) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	v, _ := s.App.ListGroups()
	for _, item := range v {
		if item.ID == g.ID {
			c.JSON(http.StatusCreated, dto.ToGroupResponse(item))
			return
		}
	}
	c.JSON(http.StatusCreated, gin.H{"id": g.ID, "name": g.Name})
}

func UpdateGroup(s *Server, c *gin.Context) {
	id, err := httputil.ParseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req updateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g, err := s.App.UpdateGroup(id, service.UpdateGroupInput{Name: req.Name, PosX: req.PosX, PosY: req.PosY, AllowIntraGroup: req.AllowIntraGroup})
	if err != nil {
		if service.IsRuntimeFailure(err) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}
	v, err := s.App.ListGroups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for _, item := range v {
		if item.ID == g.ID {
			c.JSON(http.StatusOK, dto.ToGroupResponse(item))
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"id": g.ID, "name": g.Name})
}

func DeleteGroup(s *Server, c *gin.Context) {
	id, err := httputil.ParseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := s.App.DeleteGroup(id); err != nil {
		if service.IsRuntimeFailure(err) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func GroupGraph(s *Server, c *gin.Context) {
	data, err := s.App.GroupGraph()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, dto.ToGroupGraphResponse(data))
}

func CreateGroupLink(s *Server, c *gin.Context) {
	var req groupLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	bidir := true
	if req.Bidirectional != nil {
		bidir = *req.Bidirectional
	}
	if err := s.App.CreateGroupLink(service.GroupLinkInput{FromGroupID: req.FromGroupID, ToGroupID: req.ToGroupID, Bidirectional: bidir}); err != nil {
		if service.IsRuntimeFailure(err) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, service.ErrSelfLink) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"ok": true})
}

func DeleteGroupLink(s *Server, c *gin.Context) {
	var req groupLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.App.DeleteGroupLink(service.GroupLinkInput{FromGroupID: req.FromGroupID, ToGroupID: req.ToGroupID, Bidirectional: req.Bidirectional != nil && *req.Bidirectional}); err != nil {
		if service.IsRuntimeFailure(err) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func UpdateGroupLayout(s *Server, c *gin.Context) {
	var req layoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	items := make([]service.GroupLayoutItem, len(req.Groups))
	for i, item := range req.Groups {
		items[i] = service.GroupLayoutItem{ID: item.ID, PosX: item.PosX, PosY: item.PosY}
	}
	_ = s.App.UpdateGroupLayout(items)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
