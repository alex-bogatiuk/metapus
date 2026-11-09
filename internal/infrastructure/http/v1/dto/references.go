package dto

import "metapus/internal/domain"

type ResolveRefsRequest struct {
	Refs []domain.RefResolveRequest `json:"refs" binding:"required,min=1,max=100"`
}
