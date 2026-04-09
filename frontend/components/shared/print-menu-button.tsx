"use client"

import { useCallback, useState } from "react"
import { Printer, FileDown, FileText, ChevronDown, Loader2 } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { useAuthStore } from "@/stores/useAuthStore"

export interface PrintMenuConfig {
  /** Print form display name, e.g. "Goods Receipt" */
  formLabel: string
  /** Document ID (UUID) */
  documentId: string
  /** Backend document type slug, e.g. "goods-receipt" */
  documentType: string
}

async function fetchPrint(
  documentType: string,
  documentId: string,
  output: "html" | "pdf" | "docx",
): Promise<Response> {
  const tokens = useAuthStore.getState().tokens
  const tenantId = process.env.NEXT_PUBLIC_TENANT_ID ?? ""
  const base = process.env.NEXT_PUBLIC_API_URL ?? "/api/v1"
  const url = `${base}/document/${documentType}/${documentId}/print?output=${output}`
  return fetch(url, {
    headers: {
      ...(tokens?.accessToken
        ? { Authorization: `Bearer ${tokens.accessToken}` }
        : {}),
      ...(tenantId ? { "X-Tenant-ID": tenantId } : {}),
    },
  })
}

export function PrintMenuButton({ config }: { config: PrintMenuConfig }) {
  const [loading, setLoading] = useState(false)

  const handleHtml = useCallback(async () => {
    setLoading(true)
    try {
      const res = await fetchPrint(config.documentType, config.documentId, "html")
      if (!res.ok) {
        toast.error("Не удалось сформировать печатную форму")
        return
      }
      const html = await res.text()
      const w = window.open("", "_blank")
      if (w) {
        w.document.write(html)
        w.document.close()
      }
    } catch {
      toast.error("Ошибка при формировании печатной формы")
    } finally {
      setLoading(false)
    }
  }, [config.documentType, config.documentId])

  const handleDownload = useCallback(
    async (output: "pdf" | "docx") => {
      setLoading(true)
      try {
        const res = await fetchPrint(config.documentType, config.documentId, output)
        if (!res.ok) {
          toast.error("Не удалось сформировать файл")
          return
        }
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
      } catch {
        toast.error("Ошибка при скачивании файла")
      } finally {
        setLoading(false)
      }
    },
    [config.documentType, config.documentId],
  )

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" className="px-2.5" disabled={loading} title="Печать">
          {loading ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
          ) : (
            <Printer className="h-3.5 w-3.5" />
          )}
          <ChevronDown className="ml-1.5 h-3.5 w-3.5 opacity-70" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">
          {config.formLabel}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={handleHtml} className="gap-2">
          <Printer className="h-3.5 w-3.5" />
          Открыть для печати
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => handleDownload("pdf")} className="gap-2">
          <FileDown className="h-3.5 w-3.5" />
          Скачать PDF
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => handleDownload("docx")} className="gap-2">
          <FileText className="h-3.5 w-3.5" />
          Скачать Word (.docx)
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
