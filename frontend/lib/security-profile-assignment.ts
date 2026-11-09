export const NO_SECURITY_PROFILE_VALUE = "__none__"

export function securityProfileSelectValue(profileId: string | null): string {
  return profileId ?? NO_SECURITY_PROFILE_VALUE
}

export function parseSecurityProfileSelectValue(value: string): string | null {
  return value === NO_SECURITY_PROFILE_VALUE ? null : value
}

interface SyncSecurityProfileAssignmentParams {
  userId: string
  currentProfileId: string | null
  nextProfileId: string | null
  assignUser: (profileId: string, userId: string) => Promise<void>
  removeUser: (profileId: string, userId: string) => Promise<void>
}

export async function syncSecurityProfileAssignment({
  userId,
  currentProfileId,
  nextProfileId,
  assignUser,
  removeUser,
}: SyncSecurityProfileAssignmentParams): Promise<void> {
  if (nextProfileId === currentProfileId) return

  if (currentProfileId) {
    await removeUser(currentProfileId, userId)
  }

  if (nextProfileId) {
    await assignUser(nextProfileId, userId)
  }
}
