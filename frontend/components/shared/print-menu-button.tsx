"use client"

import { useCallback, useState } from "react"
import { Printer, FileDown, FileText, ChevronDown, Loader2 } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { useAuthStore } from "@/stores/useAuthStore"
import { usePrintForms } from "@/hooks/usePrintForms"
import type { PrintFormSummary } from "@/types/print"

// ── Print execution ────────────────────────────────────────────────────

type OutputFormat = "html" | "pdf" | "docx"

async function fetchPrint(
  documentType: string,
  documentId: string,
  formName: string,
  output: OutputFormat,
): Promise<Response> {
  const tokens = useAuthStore.getState().tokens
  const tenantId = process.env.NEXT_PUBLIC_TENANT_ID ?? ""
  const base = process.env.NEXT_PUBLIC_API_URL ?? "/api/v1"
  const url = `${base}/document/${documentType}/${documentId}/print?format=${encodeURIComponent(formName)}&output=${output}`
  return fetch(url, {
    headers: {
      ...(tokens?.accessToken
        ? { Authorization: `Bearer ${tokens.accessToken}` }
        : {}),
      ...(tenantId ? { "X-Tenant-ID": tenantId } : {}),
    },
  })
}

async function executePrint(
  documentType: string,
  documentId: string,
  formName: string,
  output: OutputFormat,
): Promise<void> {
  const res = await fetchPrint(documentType, documentId, formName, output)
  if (!res.ok) {
    toast.error("Не удалось сформировать печатную форму")
    return
  }

  if (output === "html") {
    const html = await res.text()
    const w = window.open("", "_blank")
    if (w) {
      w.document.write(html)
      w.document.close()
    }
  } else {
    const blob = await res.blob()
    const disposition = res.headers.get("Content-Disposition") ?? ""
    // Prefer RFC 5987 filename* (UTF-8 encoded), fall back to plain filename
    const rfc5987 = disposition.match(/filename\*=UTF-8''(.+?)(?:;|$)/i)
    const plain = disposition.match(/filename="?(.+?)"?(?:;|$)/)
    const filename = rfc5987?.[1]
      ? decodeURIComponent(rfc5987[1])
      : plain?.[1] ?? `document.${output}`

    const a = document.createElement("a")
    a.href = URL.createObjectURL(blob)
    a.download = filename
    a.click()
    URL.revokeObjectURL(a.href)
  }
}

// ── Format button (tiny icon) ──────────────────────────────────────────

function FormatButton({
  icon,
  title,
  loading,
  onClick,
}: {
  icon: React.ReactNode
  title: string
  loading: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      className="inline-flex h-6 w-6 items-center justify-center rounded-sm text-muted-foreground hover:bg-accent hover:text-foreground transition-colors disabled:opacity-50"
      title={title}
      disabled={loading}
      onClick={(e) => {
        e.stopPropagation()
        onClick()
      }}
    >
      {loading ? <Loader2 className="h-3 w-3 animate-spin" /> : icon}
    </button>
  )
}

// ── Form row (label + 3 format buttons) ────────────────────────────────

function PrintFormRow({
  form,
  loadingKey,
  onPrint,
}: {
  form: PrintFormSummary
  loadingKey: string | null
  onPrint: (name: string, output: OutputFormat) => void
}) {
  return (
    <div className="flex items-center justify-between px-2 py-1.5 hover:bg-accent rounded-sm">
      <span className="text-sm flex-1 truncate mr-2" title={form.label}>
        {form.label}
      </span>
      <div className="flex items-center gap-0.5 shrink-0">
        <FormatButton
          icon={<Printer className="h-3 w-3" />}
          title="Открыть для печати"
          loading={loadingKey === `${form.name}:html`}
          onClick={() => onPrint(form.name, "html")}
        />
        <FormatButton
          icon={<FileDown className="h-3 w-3" />}
          title="Скачать PDF"
          loading={loadingKey === `${form.name}:pdf`}
          onClick={() => onPrint(form.name, "pdf")}
        />
        <FormatButton
          icon={<FileText className="h-3 w-3" />}
          title="Скачать Word"
          loading={loadingKey === `${form.name}:docx`}
          onClick={() => onPrint(form.name, "docx")}
        />
      </div>
    </div>
  )
}

// ── Main component ─────────────────────────────────────────────────────

export function PrintMenuButton({
  documentType,
  documentId,
}: {
  documentType: string
  documentId: string
}) {
  const { standard, custom, loading: formsLoading } = usePrintForms(documentType)
  const [loadingKey, setLoadingKey] = useState<string | null>(null)

  const handlePrint = useCallback(
    async (formName: string, output: OutputFormat) => {
      const key = `${formName}:${output}`
      setLoadingKey(key)
      try {
        await executePrint(documentType, documentId, formName, output)
      } catch {
        toast.error("Ошибка при формировании печатной формы")
      } finally {
        setLoadingKey(null)
      }
    },
    [documentType, documentId],
  )

  const hasForms = standard.length > 0 || custom.length > 0

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          className="px-2.5"
          disabled={formsLoading}
          title="Печать"
        >
          {formsLoading ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
          ) : (
            <Printer className="h-3.5 w-3.5" />
          )}
          <ChevronDown className="ml-1.5 h-3.5 w-3.5 opacity-70" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-72">
        {!hasForms && (
          <div className="px-2 py-3 text-center text-xs text-muted-foreground">
            Нет доступных печатных форм
          </div>
        )}

        {standard.length > 0 && (
          <>
            <DropdownMenuLabel className="text-[10px] uppercase tracking-wider text-muted-foreground">
              Регламентированные
            </DropdownMenuLabel>
            {standard.map((form) => (
              <PrintFormRow
                key={form.name}
                form={form}
                loadingKey={loadingKey}
                onPrint={handlePrint}
              />
            ))}
          </>
        )}

        {custom.length > 0 && (
          <>
            {standard.length > 0 && <DropdownMenuSeparator />}
            <DropdownMenuLabel className="text-[10px] uppercase tracking-wider text-muted-foreground">
              Кастомные
            </DropdownMenuLabel>
            {custom.map((form) => (
              <PrintFormRow
                key={form.name}
                form={form}
                loadingKey={loadingKey}
                onPrint={handlePrint}
              />
            ))}
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
