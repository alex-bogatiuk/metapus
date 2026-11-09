package tree_probe

import (
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

// Fields is the explicit writable projection. Never embed the domain model here.
type TreeProbeFields struct {
	Code     string `json:"code"`
	Name     string `json:"name" binding:"required"`
	ParentID *id.ID `json:"parentId"`
	IsFolder bool   `json:"isFolder"`

	Attributes entity.Attributes `json:"attributes"`
}

type CreateTreeProbeRequest struct {
	TreeProbeFields
}

type UpdateTreeProbeRequest struct {
	TreeProbeFields
	Version int `json:"version" binding:"required,min=1"`
}

func (r TreeProbeFields) apply(e *TreeProbe) {
	e.Code = r.Code
	e.Name = r.Name
	e.ParentID = r.ParentID
	e.IsFolder = r.IsFolder

	if r.Attributes != nil {
		e.Attributes = r.Attributes
	}
}
func (r CreateTreeProbeRequest) ToEntity() *TreeProbe {
	e := NewTreeProbe(r.Code, r.Name)
	r.TreeProbeFields.apply(e)
	return e
}
func (r UpdateTreeProbeRequest) ApplyTo(e *TreeProbe) {
	r.TreeProbeFields.apply(e)
	e.Version = r.Version
}

type TreeProbeResponse struct {
	TreeProbeFields
	ID           string `json:"id"`
	Version      int    `json:"version"`
	DeletionMark bool   `json:"deletionMark"`
}

func FromTreeProbe(e *TreeProbe) *TreeProbeResponse {
	return &TreeProbeResponse{
		TreeProbeFields: TreeProbeFields{
			Code: e.Code, Name: e.Name,
			ParentID: e.ParentID, IsFolder: e.IsFolder,
			Attributes: e.Attributes,
		}, ID: e.ID.String(), Version: e.Version, DeletionMark: e.DeletionMark,
	}
}
