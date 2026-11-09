package doc_probe

import (
	"time"

	"metapus/internal/core/entity"
)

// Fields is the explicit writable projection. Never embed the domain model here.
type DocProbeFields struct {
	Number      string    `json:"number"`
	Date        time.Time `json:"date" binding:"required"`
	Description string    `json:"description"`

	Attributes entity.Attributes `json:"attributes"`
}

type CreateDocProbeRequest struct {
	DocProbeFields
	PostImmediately bool `json:"postImmediately"`
}

type UpdateDocProbeRequest struct {
	DocProbeFields
	Version int `json:"version" binding:"required,min=1"`
}

func (r DocProbeFields) apply(e *DocProbe) {
	e.Number = r.Number
	e.Date = r.Date
	e.Description = r.Description

	if r.Attributes != nil {
		e.Attributes = r.Attributes
	}
}
func (r CreateDocProbeRequest) ToEntity() *DocProbe {
	e := NewDocProbe()
	r.DocProbeFields.apply(e)
	return e
}
func (r UpdateDocProbeRequest) ApplyTo(e *DocProbe) {
	r.DocProbeFields.apply(e)
	e.Version = r.Version
}

type DocProbeResponse struct {
	DocProbeFields
	ID            string `json:"id"`
	Version       int    `json:"version"`
	DeletionMark  bool   `json:"deletionMark"`
	Posted        bool   `json:"posted"`
	PostedVersion int    `json:"postedVersion"`
}

func FromDocProbe(e *DocProbe) *DocProbeResponse {
	return &DocProbeResponse{
		DocProbeFields: DocProbeFields{
			Number: e.Number, Date: e.Date, Description: e.Description,
			Attributes: e.Attributes,
		}, ID: e.ID.String(), Version: e.Version, DeletionMark: e.DeletionMark,
		Posted: e.Posted, PostedVersion: e.PostedVersion,
	}
}
