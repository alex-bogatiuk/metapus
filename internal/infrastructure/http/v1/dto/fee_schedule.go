package dto

import "metapus/internal/domain/crypto"

// ── Fee Schedule DTOs ────────────────────────────────────────────────────

// FeeScheduleRequest is the request body for upserting a fee schedule entry.
type FeeScheduleRequest struct {
	TokenID   string `json:"tokenId" binding:"required"`
	Direction string `json:"direction" binding:"required"`
	FixedFee  int64  `json:"fixedFee"`
	PercentBP int    `json:"percentBp"`
	MinFee    int64  `json:"minFee"`
	MaxFee    int64  `json:"maxFee"`
}

// FeeScheduleResponse is the response body for a fee schedule entry.
type FeeScheduleResponse struct {
	MerchantID *string `json:"merchantId"` // nil = global default
	TokenID    string  `json:"tokenId"`
	Direction  string  `json:"direction"`
	FixedFee   string  `json:"fixedFee"`
	PercentBP  int     `json:"percentBp"`
	MinFee     string  `json:"minFee"`
	MaxFee     string  `json:"maxFee"`
	UpdatedAt  string  `json:"updatedAt"`
}

// FeeScheduleDeleteRequest is the request body for deleting a fee schedule entry.
type FeeScheduleDeleteRequest struct {
	TokenID   string `json:"tokenId" binding:"required"`
	Direction string `json:"direction" binding:"required"`
}

// FromFeeSchedule creates response DTO from domain entity.
func FromFeeSchedule(fs *crypto.FeeSchedule) FeeScheduleResponse {
	resp := FeeScheduleResponse{
		TokenID:   fs.TokenID.String(),
		Direction: string(fs.Direction),
		FixedFee:  fs.FixedFee.String(),
		PercentBP: fs.PercentBP,
		MinFee:    fs.MinFee.String(),
		MaxFee:    fs.MaxFee.String(),
		UpdatedAt: fs.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if fs.MerchantID != nil {
		s := fs.MerchantID.String()
		resp.MerchantID = &s
	}
	return resp
}

// FromFeeScheduleList converts a slice of domain entities to response DTOs.
func FromFeeScheduleList(schedules []crypto.FeeSchedule) []FeeScheduleResponse {
	result := make([]FeeScheduleResponse, len(schedules))
	for i := range schedules {
		result[i] = FromFeeSchedule(&schedules[i])
	}
	return result
}
