import { expect, test } from "@playwright/test"
import {
  NO_SECURITY_PROFILE_VALUE,
  parseSecurityProfileSelectValue,
  securityProfileSelectValue,
  syncSecurityProfileAssignment,
} from "../lib/security-profile-assignment"

test.describe("security profile assignment", () => {
  test("removes current profile and does not assign when next profile is null", async () => {
    const calls: string[] = []

    await syncSecurityProfileAssignment({
      userId: "user-1",
      currentProfileId: "profile-current",
      nextProfileId: null,
      assignUser: async (profileId, userId) => {
        calls.push(`assign:${profileId}:${userId}`)
      },
      removeUser: async (profileId, userId) => {
        calls.push(`remove:${profileId}:${userId}`)
      },
    })

    expect(calls).toEqual(["remove:profile-current:user-1"])
  })

  test("does nothing when profile assignment is unchanged", async () => {
    const calls: string[] = []

    await syncSecurityProfileAssignment({
      userId: "user-1",
      currentProfileId: "profile-current",
      nextProfileId: "profile-current",
      assignUser: async (profileId, userId) => {
        calls.push(`assign:${profileId}:${userId}`)
      },
      removeUser: async (profileId, userId) => {
        calls.push(`remove:${profileId}:${userId}`)
      },
    })

    expect(calls).toEqual([])
  })

  test("keeps select sentinel out of profile state", () => {
    expect(securityProfileSelectValue(null)).toBe(NO_SECURITY_PROFILE_VALUE)
    expect(parseSecurityProfileSelectValue(NO_SECURITY_PROFILE_VALUE)).toBeNull()
    expect(parseSecurityProfileSelectValue("profile-next")).toBe("profile-next")
  })
})
