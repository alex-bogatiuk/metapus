package dto

// TypedCursorListResponse describes the concrete item projection selected by a factory.
type TypedCursorListResponse[T any] struct {
	Items       []T    `json:"items"`
	NextCursor  string `json:"nextCursor,omitempty"`
	PrevCursor  string `json:"prevCursor,omitempty"`
	HasMore     bool   `json:"hasMore"`
	HasPrev     bool   `json:"hasPrev"`
	TargetIndex *int   `json:"targetIndex,omitempty"`
	TotalCount  *int64 `json:"totalCount"`
}
