"use client"

import type { FormState } from "@/components/settings/security-profile/model"
import { EMPTY_FORM, tabStateKeys } from "@/components/settings/security-profile/model"
import { useTabDirty } from "@/hooks/useTabDirty"
import { useHasTabCache, useTabState } from "@/hooks/useTabState"
import { useTabTitle } from "@/hooks/useTabTitle"
import { api, ApiError } from "@/lib/api"
import { useProfilePresetStore } from "@/stores/useProfilePresetStore"
import { useParams, useRouter } from "next/navigation"
import type { SetStateAction } from "react"
import { useCallback, useEffect, useRef, useState } from "react"
import { toast } from "sonner"

export function useSecurityProfile() {
  const params = useParams()
  const router = useRouter()
  const profileId = params.id as string
  const isNew = profileId === "new"
  const [profile, setProfile] = useTabState(tabStateKeys.profile, null)
  const [form, setFormState] = useTabState(tabStateKeys.form, EMPTY_FORM)
  const { markDirty, markClean } = useTabDirty()
  const formRevision = useRef(0)
  const setForm = useCallback((value: SetStateAction<FormState>) => {
    formRevision.current++
    setFormState(value)
    markDirty()
  }, [setFormState, markDirty])

  const [rules, setRules] = useTabState(tabStateKeys.rules, [])
  const hasCachedForm = useHasTabCache(tabStateKeys.form)
  const [loading, setLoading] = useState(!isNew && !hasCachedForm)
  const [saving, setSaving] = useState(false)
  const [activeTab, setActiveTab] = useTabState(tabStateKeys.activeTab, "general")

  useTabTitle(
    profile ? profile.name : isNew ? "Новый профиль" : undefined,
    "Профиль безопасности"
  )

  useEffect(() => {
    if (!isNew) return
    const preset = useProfilePresetStore.getState().preset
    if (!preset) return
    setForm({
      code: preset.code,
      name: preset.name,
      description: preset.description,
      dimensions: preset.dimensions,
      entityDimensions: preset.entityDimensions ?? {},
      fieldPolicies: preset.fieldPolicies,
    })
    useProfilePresetStore.getState().clear()
  }, [isNew, setForm])

  useEffect(() => {
    if (isNew || hasCachedForm) return
    let cancelled = false
    api.security.profiles.get(profileId).then((data) => {
      if (cancelled) return
      setProfile(data)
      setFormState({
        code: data.code,
        name: data.name,
        description: data.description ?? "",
        dimensions: data.dimensions ?? {},
        entityDimensions: data.entityDimensions ?? {},
        fieldPolicies: data.fieldPolicies ?? [],
      })
      setRules(data.policyRules ?? [])
      setLoading(false)
    }).catch(() => {
      if (!cancelled) {
        toast.error("Не удалось загрузить профиль")
        setLoading(false)
      }
    })
    return () => { cancelled = true }
  }, [profileId, isNew, hasCachedForm, setProfile, setFormState, setRules])

  const handleSave = useCallback(async () => {
    if (!form.code || !form.name) {
      toast.error("Код и название обязательны")
      return
    }
    const savedRevision = formRevision.current
    setSaving(true)
    try {
      if (isNew) {
        const created = await api.security.profiles.create({
          code: form.code,
          name: form.name,
          description: form.description || undefined,
          dimensions: form.dimensions,
          entityDimensions: Object.keys(form.entityDimensions).length > 0 ? form.entityDimensions : undefined,
          fieldPolicies: form.fieldPolicies,
        })
        toast.success("Профиль создан")
        router.replace(`/admin/security-profiles/${created.id}`)
      } else {
        const updated = await api.security.profiles.update(profileId, {
          code: form.code,
          name: form.name,
          description: form.description || undefined,
          dimensions: form.dimensions,
          entityDimensions: Object.keys(form.entityDimensions).length > 0 ? form.entityDimensions : undefined,
          fieldPolicies: form.fieldPolicies,
        })
        setProfile(updated)
        toast.success("Профиль сохранён")
      }
      if (formRevision.current === savedRevision) markClean()
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "Не удалось сохранить профиль")
    } finally {
      setSaving(false)
    }
  }, [form, isNew, profileId, router, setProfile, markClean])
  return { router, profileId, isNew, profile, form, setForm, rules, setRules, loading, saving, activeTab, setActiveTab, handleSave }
}
