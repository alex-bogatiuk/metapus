package dto

type SecurityProfileListResponse struct {
	Items []SecurityProfileResponse `json:"items"`
}

type ProfileUserListResponse struct {
	Items []ProfileUserItem `json:"items"`
}

type SecurityAuditListResponse struct {
	Items []AuditEntryResponse `json:"items"`
}
