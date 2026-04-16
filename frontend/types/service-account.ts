export type AccountType = "telegram" | "email" | "webhook" | "rocketchat" | "slack"

export type AccountStatus = "active" | "error" | "disabled"

export interface ServiceAccount {
  id: string
  name: string
  accountType: AccountType
  config: Record<string, any>
  organizationId?: string
  status: AccountStatus
  isDefault: boolean
  lastError?: string
  lastSuccessAt?: string
  createdAt: string
  updatedAt: string
}

export interface CreateServiceAccountRequest {
  name: string
  accountType: AccountType
  config: Record<string, any>
  organizationId?: string
  isDefault: boolean
  credentials?: string // Base64 or plain depending on what the backend expects; we'll treat as string for now
}

export interface UpdateServiceAccountRequest {
  name: string
  config: Record<string, any>
  organizationId?: string
  status: AccountStatus
  isDefault: boolean
}

export interface UpdateCredentialsRequest {
  credentials: string // base64 encoded bytes for Go backward comp if needed, or string if API is updated safely
}
