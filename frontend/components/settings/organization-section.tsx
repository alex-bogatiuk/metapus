"use client"

import { useCallback } from "react"
import { Building2, Upload } from "lucide-react"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { useSettingsStore } from "@/stores/useSettingsStore"
import { useTabDirty } from "@/hooks/useTabDirty"

function FieldGroup({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-4">
      <h3 className="text-sm font-semibold text-foreground">{label}</h3>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">{children}</div>
    </div>
  )
}

interface FieldProps {
  label: string
  value: string
  onChange: (value: string) => void
  placeholder?: string
  colSpan?: boolean
  disabled?: boolean
}

function Field({
  label,
  value,
  onChange,
  placeholder,
  colSpan,
  disabled,
}: FieldProps) {
  return (
    <div className={colSpan ? "sm:col-span-2" : undefined}>
      <Label className="mb-1.5 text-xs text-muted-foreground">{label}</Label>
      <Input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        disabled={disabled}
        className="h-9 text-sm"
      />
    </div>
  )
}

export function OrganizationSection() {
  const { settings, updateOrganization, isSaving } = useSettingsStore()
  const { markDirty } = useTabDirty()
  const org = settings.organization

  const update = useCallback(
    (field: string, value: string) => {
      updateOrganization({ [field]: value })
      markDirty()
    },
    [updateOrganization, markDirty]
  )

  return (
    <div className="space-y-6" onChange={markDirty}>
      {/* Logo */}
      <div className="flex items-start gap-4">
        <div className="flex h-20 w-20 shrink-0 items-center justify-center rounded-lg border-2 border-dashed border-border bg-muted/40">
          {org.logoUrl ? (
            <>
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img
                src={org.logoUrl}
                alt="Логотип"
                className="h-full w-full rounded-lg object-contain"
              />
            </>
          ) : (
            <Building2 className="h-8 w-8 text-muted-foreground/50" />
          )}
        </div>
        <div className="space-y-1.5">
          <p className="text-sm font-medium text-foreground">
            Логотип компании
          </p>
          <p className="text-xs text-muted-foreground">
            PNG, JPG или SVG, не более 2 МБ
          </p>
          <Button variant="outline" size="sm" disabled={isSaving}>
            <Upload className="mr-1.5 h-3.5 w-3.5" />
            Загрузить
          </Button>
        </div>
      </div>

      <Separator />

      {/* Основные реквизиты */}
      <FieldGroup label="Основные реквизиты">
        <Field
          label="Полное наименование"
          value={org.companyName}
          onChange={(v) => update("companyName", v)}
          placeholder='ООО "Название"'
          colSpan
        />
        <Field
          label="Краткое наименование"
          value={org.shortName}
          onChange={(v) => update("shortName", v)}
          placeholder="Название"
        />
        <Field
          label="ИНН"
          value={org.inn}
          onChange={(v) => update("inn", v)}
          placeholder="1234567890"
        />
        <Field
          label="КПП"
          value={org.kpp}
          onChange={(v) => update("kpp", v)}
          placeholder="123456789"
        />
        <Field
          label="ОГРН"
          value={org.ogrn}
          onChange={(v) => update("ogrn", v)}
          placeholder="1234567890123"
        />
      </FieldGroup>

      <Separator />

      {/* Адреса */}
      <FieldGroup label="Адреса">
        <Field
          label="Юридический адрес"
          value={org.legalAddress}
          onChange={(v) => update("legalAddress", v)}
          placeholder="г. Москва, ул. Примерная, д. 1"
          colSpan
        />
        <Field
          label="Фактический адрес"
          value={org.actualAddress}
          onChange={(v) => update("actualAddress", v)}
          placeholder="г. Москва, ул. Примерная, д. 1"
          colSpan
        />
      </FieldGroup>

      <Separator />

      {/* Контакты */}
      <FieldGroup label="Контакты">
        <Field
          label="Телефон"
          value={org.phone}
          onChange={(v) => update("phone", v)}
          placeholder="+7 (999) 123-45-67"
        />
        <Field
          label="Email"
          value={org.email}
          onChange={(v) => update("email", v)}
          placeholder="info@company.ru"
        />
        <Field
          label="Веб-сайт"
          value={org.website}
          onChange={(v) => update("website", v)}
          placeholder="https://company.ru"
        />
      </FieldGroup>

      <Separator />

      {/* Ответственные лица */}
      <FieldGroup label="Ответственные лица">
        <Field
          label="Руководитель"
          value={org.director}
          onChange={(v) => update("director", v)}
          placeholder="Иванов Иван Иванович"
        />
        <Field
          label="Главный бухгалтер"
          value={org.accountant}
          onChange={(v) => update("accountant", v)}
          placeholder="Петрова Мария Сергеевна"
        />
      </FieldGroup>

      {/* Spacer so content doesn't hide behind sticky footer */}
      <div className="h-16" />

      {/* Sticky Save footer */}
      <div className="sticky bottom-0 -mx-6 border-t bg-background px-6 py-3 flex items-center gap-3">
        <Button disabled={isSaving}>
          {isSaving ? "Сохранение..." : "Сохранить"}
        </Button>
        <p className="text-xs text-muted-foreground">
          Изменения применятся ко всем пользователям системы
        </p>
      </div>
    </div>
  )
}
