// lib/automation-helpers.ts
// Shared helpers for automation UI — single source of truth for labels, icons, statuses.

import { Bot, Webhook, Send, MessageSquare, Hash, type LucideIcon } from "lucide-react"
import type { AccountType, AccountStatus, HistoryStatus } from "@/types/automation"

// ── Account Type Metadata ────────────────────────────────────────────────

export interface AccountTypeMeta {
  label: string
  icon: LucideIcon
}

export const ACCOUNT_TYPE_META: Record<string, AccountTypeMeta> = {
  telegram:   { label: "Telegram Bot", icon: Bot },
  webhook:    { label: "Webhook",      icon: Webhook },
  email:      { label: "Email SMTP",   icon: Send },
  rocketchat: { label: "Rocket.Chat",  icon: MessageSquare },
  slack:      { label: "Slack",        icon: Hash },
}

// ── Account Status Metadata ──────────────────────────────────────────────

export interface StatusMeta {
  label: string
  variant: "default" | "destructive" | "secondary" | "outline"
}

export const ACCOUNT_STATUS_MAP: Record<AccountStatus, StatusMeta> = {
  active:   { label: "Активен",   variant: "default" },
  error:    { label: "Ошибка",    variant: "destructive" },
  disabled: { label: "Отключён",  variant: "secondary" },
}

// ── History Status ───────────────────────────────────────────────────────

export const HISTORY_STATUS_MAP: Record<HistoryStatus, StatusMeta> = {
  success:         { label: "OK",       variant: "default" },
  error:           { label: "Ошибка",   variant: "destructive" },
  condition_false: { label: "Условие",  variant: "secondary" },
  skipped:         { label: "Пропуск",  variant: "outline" },
  pending:         { label: "Ожидание", variant: "outline" },
}

// ── Dynamic config fields per account type ───────────────────────────────

export interface ConfigField {
  key: string
  label: string
  placeholder: string
  type: "text" | "number" | "password" | "select"
  options?: { value: string; label: string }[]
  required?: boolean
}

/** Non-secret config fields shown in Account form */
export function getAccountConfigFields(accountType: AccountType): ConfigField[] {
  switch (accountType) {
    case "telegram":
      return [
        { key: "parse_mode", label: "Parse Mode", placeholder: "MarkdownV2", type: "select",
          options: [
            { value: "", label: "Не задан" },
            { value: "Markdown", label: "Markdown" },
            { value: "MarkdownV2", label: "MarkdownV2" },
            { value: "HTML", label: "HTML" },
          ] },
      ]
    case "email":
      return [
        { key: "smtp_host", label: "SMTP Host", placeholder: "smtp.gmail.com", type: "text", required: true },
        { key: "smtp_port", label: "SMTP Port", placeholder: "587", type: "text" },
        { key: "from", label: "From (email)", placeholder: "noreply@example.com", type: "text", required: true },
        { key: "content_type", label: "Content Type", placeholder: "text/plain", type: "select",
          options: [
            { value: "text/plain", label: "text/plain" },
            { value: "text/html", label: "text/html" },
          ] },
      ]
    case "webhook":
      return [
        { key: "auth_type", label: "Авторизация", placeholder: "", type: "select",
          options: [
            { value: "bearer", label: "Bearer Token" },
            { value: "header", label: "Custom Header" },
          ] },
        { key: "header_name", label: "Header Name", placeholder: "X-Webhook-Secret", type: "text" },
      ]
    default:
      return []
  }
}

/** Destination fields shown in Channel form (depends on account type) */
export function getChannelDestinationFields(accountType: AccountType): ConfigField[] {
  switch (accountType) {
    case "telegram":
      return [
        { key: "chat_id", label: "Chat ID", placeholder: "-1001234567890", type: "text", required: true },
      ]
    case "email":
      return [
        { key: "to", label: "Получатели (email)", placeholder: "user@example.com, user2@example.com", type: "text", required: true },
      ]
    case "webhook":
      return [
        { key: "url", label: "Webhook URL", placeholder: "https://api.example.com/webhook", type: "text", required: true },
        { key: "method", label: "HTTP Method", placeholder: "POST", type: "select",
          options: [
            { value: "POST", label: "POST" },
            { value: "PUT", label: "PUT" },
          ] },
      ]
    default:
      return []
  }
}

/** Credential label per account type */
export function getCredentialLabel(accountType: AccountType): string {
  switch (accountType) {
    case "telegram":  return "Bot Token"
    case "email":     return "SMTP Password"
    case "webhook":   return "API Key / Bearer Token"
    default:          return "Секретный ключ"
  }
}
