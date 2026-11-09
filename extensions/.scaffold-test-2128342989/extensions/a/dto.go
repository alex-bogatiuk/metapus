package a

import (
	"metapus/internal/core/entity"
)

// Fields is the explicit writable projection. Never embed the domain model here.
type AFields struct {
	Code string `json:"code"`
	Name string `json:"name" binding:"required"`

	Attributes entity.Attributes `json:"attributes"`
}

type CreateARequest struct {
	AFields
}

type UpdateARequest struct {
	AFields
	Version int `json:"version" binding:"required,min=1"`
}

func (r AFields) apply(e *A) {
	e.Code = r.Code
	e.Name = r.Name

	if r.Attributes != nil {
		e.Attributes = r.Attributes
	}
}
func (r CreateARequest) ToEntity() *A {
	e := NewA(r.Code, r.Name)
	r.AFields.apply(e)
	return e
}
func (r UpdateARequest) ApplyTo(e *A) {
	r.AFields.apply(e)
	e.Version = r.Version
}

type AResponse struct {
	AFields
	ID           string `json:"id"`
	Version      int    `json:"version"`
	DeletionMark bool   `json:"deletionMark"`
}

func FromA(e *A) *AResponse {
	return &AResponse{
		AFields: AFields{
			Code: e.Code, Name: e.Name,

			Attributes: e.Attributes,
		}, ID: e.ID.String(), Version: e.Version, DeletionMark: e.DeletionMark,
	}
}
