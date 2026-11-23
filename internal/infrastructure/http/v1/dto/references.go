package dto

import "github.com/alex-bogatiuk/metapus/internal/domain"

type ResolveRefsRequest struct {
	Refs []domain.RefResolveRequest `json:"refs" binding:"required,min=1,max=100"`
}
