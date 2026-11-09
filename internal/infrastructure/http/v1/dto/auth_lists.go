package dto

type AuthMessageResponse struct {
	Message string `json:"message"`
}

type AdminUserListResponse struct {
	Items []*UserResponse `json:"items"`
	Total int             `json:"total"`
}

type RoleListResponse struct {
	Items []*RoleResponse `json:"items"`
}

type PermissionListResponse struct {
	Items []*PermissionResponse `json:"items"`
}

type DeleteRoleResponse struct {
	Message       string `json:"message"`
	AffectedUsers int    `json:"affectedUsers"`
}

type PermissionCodesResponse struct {
	Items []string `json:"items"`
}

type WebSocketTicketResponse struct {
	Ticket string `json:"ticket"`
}
