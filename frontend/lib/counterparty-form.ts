import type { CounterpartyType, LegalForm, CounterpartyResponse, CreateCounterpartyRequest, UpdateCounterpartyRequest } from "@/types/catalog"
import { counterpartyTypeSchema, legalFormSchema } from "@/lib/schemas/catalog-values"

export interface CounterpartyFormState {
  name: string
  code: string
  type: CounterpartyType
  legalForm: LegalForm
  fullName: string
  inn: string
  kpp: string
  ogrn: string
  legalAddress: string
  actualAddress: string
  phone: string
  email: string
  contactPerson: string
  comment: string
  version: number
}

export const INITIAL_COUNTERPARTY_STATE: CounterpartyFormState = {
  name: "", code: "", type: "supplier", legalForm: "company", fullName: "",
  inn: "", kpp: "", ogrn: "", legalAddress: "", actualAddress: "", phone: "",
  email: "", contactPerson: "", comment: "", version: 0,
}

export function mapCounterpartyFromResponse(value: CounterpartyResponse): CounterpartyFormState {
  return {
    name: value.name, code: value.code, type: counterpartyTypeSchema.parse(value.type), legalForm: legalFormSchema.parse(value.legalForm),
    fullName: value.fullName ?? "", inn: value.inn ?? "", kpp: value.kpp ?? "", ogrn: value.ogrn ?? "",
    legalAddress: value.legalAddress ?? "", actualAddress: value.actualAddress ?? "",
    phone: value.phone ?? "", email: value.email ?? "", contactPerson: value.contactPerson ?? "",
    comment: value.comment ?? "", version: value.version,
  }
}

export function mapCounterpartyToCreate(value: CounterpartyFormState): CreateCounterpartyRequest {
  return {
    name: value.name, code: value.code || undefined, type: value.type, legalForm: value.legalForm,
    fullName: value.fullName || null, inn: value.inn || null, kpp: value.kpp || null, ogrn: value.ogrn || null,
    legalAddress: value.legalAddress || null, actualAddress: value.actualAddress || null,
    phone: value.phone || null, email: value.email || null, contactPerson: value.contactPerson || null,
    comment: value.comment || null,
  }
}

export function mapCounterpartyToUpdate(value: CounterpartyFormState): UpdateCounterpartyRequest {
  return { ...mapCounterpartyToCreate(value), code: value.code, version: value.version }
}

export const validateCounterparty = (value: CounterpartyFormState): string | null => value.name.trim() ? null : "Укажите наименование"
