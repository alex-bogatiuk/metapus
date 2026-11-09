"use client"

import { useId } from "react"
import { Input } from "@/components/ui/input"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Textarea } from "@/components/ui/textarea"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ScrollArea } from "@/components/ui/scroll-area"
import { useEntityFiltersMeta } from "@/hooks/useEntityFiltersMeta"
import { counterpartyTypeSchema, legalFormSchema } from "@/lib/schemas/catalog-values"
import type { CounterpartyFormState } from "@/lib/counterparty-form"

interface CounterpartyFormProps {
  value: CounterpartyFormState
  onChange: (patch: Partial<CounterpartyFormState>) => void
}

export function CounterpartyForm({ value: f, onChange: update }: CounterpartyFormProps) {
  const fieldId = useId()
  const { fieldsMeta, loading: filtersLoading } = useEntityFiltersMeta("Counterparty")
  const typeFieldMeta = fieldsMeta.find(field => field.key === "type")
  const legalFormFieldMeta = fieldsMeta.find(field => field.key === "legalForm")

  return (
      <ScrollArea className="flex-1">
        <div className="p-6">
        <FieldGroup className="max-w-3xl gap-6">
          <div className="grid grid-cols-1 gap-x-6 gap-y-4 md:grid-cols-2">
            <Field>
              <FieldLabel htmlFor={`${fieldId}-field-1`} className="text-xs text-muted-foreground">Наименование *</FieldLabel>
              <Input id={`${fieldId}-field-1`} className="mt-1" value={f.name} onChange={(e) => { update({ name: e.target.value }) }} />
            </Field>
            <Field>
              <FieldLabel htmlFor={`${fieldId}-field-2`} className="text-xs text-muted-foreground">Код</FieldLabel>
              <Input id={`${fieldId}-field-2`} className="mt-1" value={f.code} onChange={(e) => { update({ code: e.target.value }) }} placeholder="Авто" />
            </Field>
            <Field>
              <FieldLabel htmlFor={`${fieldId}-field-3`} className="text-xs text-muted-foreground">Тип *</FieldLabel>
              <Select value={f.type} onValueChange={(v) => { update({ type: counterpartyTypeSchema.parse(v) }) }}>
                <SelectTrigger id={`${fieldId}-field-3`} className="mt-1"><SelectValue /></SelectTrigger>
                <SelectContent><SelectGroup>
                  {filtersLoading ? (
                    <SelectItem value={f.type} disabled>Загрузка...</SelectItem>
                  ) : (
                    typeFieldMeta?.enumValues?.map(ev => (
                      <SelectItem key={ev.value} value={ev.value}>{ev.label}</SelectItem>
                    ))
                  )}
                </SelectGroup></SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor={`${fieldId}-field-4`} className="text-xs text-muted-foreground">Правовая форма *</FieldLabel>
              <Select value={f.legalForm} onValueChange={(v) => { update({ legalForm: legalFormSchema.parse(v) }) }}>
                <SelectTrigger id={`${fieldId}-field-4`} className="mt-1"><SelectValue /></SelectTrigger>
                <SelectContent><SelectGroup>
                  {filtersLoading ? (
                    <SelectItem value={f.legalForm} disabled>Загрузка...</SelectItem>
                  ) : (
                    legalFormFieldMeta?.enumValues?.map(ev => (
                      <SelectItem key={ev.value} value={ev.value}>{ev.label}</SelectItem>
                    ))
                  )}
                </SelectGroup></SelectContent>
              </Select>
            </Field>
            <Field className="md:col-span-2">
              <FieldLabel htmlFor={`${fieldId}-field-5`} className="text-xs text-muted-foreground">Полное наименование</FieldLabel>
              <Input id={`${fieldId}-field-5`} className="mt-1" value={f.fullName} onChange={(e) => { update({ fullName: e.target.value }) }} />
            </Field>
          </div>

          <div className="grid grid-cols-1 gap-x-6 gap-y-4 md:grid-cols-3">
            <Field>
              <FieldLabel htmlFor={`${fieldId}-field-6`} className="text-xs text-muted-foreground">ИНН</FieldLabel>
              <Input id={`${fieldId}-field-6`} className="mt-1" value={f.inn} onChange={(e) => { update({ inn: e.target.value }) }} />
            </Field>
            <Field>
              <FieldLabel htmlFor={`${fieldId}-field-7`} className="text-xs text-muted-foreground">КПП</FieldLabel>
              <Input id={`${fieldId}-field-7`} className="mt-1" value={f.kpp} onChange={(e) => { update({ kpp: e.target.value }) }} />
            </Field>
            <Field>
              <FieldLabel htmlFor={`${fieldId}-field-8`} className="text-xs text-muted-foreground">ОГРН</FieldLabel>
              <Input id={`${fieldId}-field-8`} className="mt-1" value={f.ogrn} onChange={(e) => { update({ ogrn: e.target.value }) }} />
            </Field>
          </div>

          <div className="grid grid-cols-1 gap-x-6 gap-y-4 md:grid-cols-2">
            <Field>
              <FieldLabel htmlFor={`${fieldId}-field-9`} className="text-xs text-muted-foreground">Юридический адрес</FieldLabel>
              <Input id={`${fieldId}-field-9`} className="mt-1" value={f.legalAddress} onChange={(e) => { update({ legalAddress: e.target.value }) }} />
            </Field>
            <Field>
              <FieldLabel htmlFor={`${fieldId}-field-10`} className="text-xs text-muted-foreground">Фактический адрес</FieldLabel>
              <Input id={`${fieldId}-field-10`} className="mt-1" value={f.actualAddress} onChange={(e) => { update({ actualAddress: e.target.value }) }} />
            </Field>
            <Field>
              <FieldLabel htmlFor={`${fieldId}-field-11`} className="text-xs text-muted-foreground">Телефон</FieldLabel>
              <Input id={`${fieldId}-field-11`} className="mt-1" value={f.phone} onChange={(e) => { update({ phone: e.target.value }) }} />
            </Field>
            <Field>
              <FieldLabel htmlFor={`${fieldId}-field-12`} className="text-xs text-muted-foreground">Email</FieldLabel>
              <Input id={`${fieldId}-field-12`} className="mt-1" type="email" value={f.email} onChange={(e) => { update({ email: e.target.value }) }} />
            </Field>
            <Field>
              <FieldLabel htmlFor={`${fieldId}-field-13`} className="text-xs text-muted-foreground">Контактное лицо</FieldLabel>
              <Input id={`${fieldId}-field-13`} className="mt-1" value={f.contactPerson} onChange={(e) => { update({ contactPerson: e.target.value }) }} />
            </Field>
          </div>

          <Field>
            <FieldLabel htmlFor={`${fieldId}-field-14`} className="text-xs text-muted-foreground">Комментарий</FieldLabel>
            <Textarea id={`${fieldId}-field-14`} rows={3} className="mt-1" value={f.comment} onChange={(e) => { update({ comment: e.target.value }) }} />
          </Field>
        </FieldGroup>
        </div>
      </ScrollArea>
  )
}
