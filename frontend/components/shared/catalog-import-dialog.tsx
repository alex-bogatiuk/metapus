"use client"

/**
 * CatalogImportDialog loads catalog items from an XLSX or CSV file
 * (загрузка данных): the server suggests which field each column fills, the
 * user corrects the mapping, checks the file without storing it, and loads
 * it. A file with bad rows stores nothing; its rows are listed with the
 * reason.
 */

import { useCallback, useState } from "react"
import { toast } from "sonner"
import { FileUp } from "lucide-react"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { apiFetch, ApiError } from "@/lib/api"
import type * as Contract from "@/types/generated/http-contracts"

type Preview = Contract.ImportPreviewResponse
type Result = Contract.ImportCatalogResponse

const SKIP = "__skip"

interface CatalogImportDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    /** The catalog's API path, e.g. "/catalog/items". */
    basePath: string
    title: string
    /** Called after a load that stored items. */
    onImported: () => void
}

/** The file's content in base64, as the import routes take it. */
function readBase64(file: File): Promise<string> {
    return new Promise((resolve, reject) => {
        const reader = new FileReader()
        reader.onload = () => {
            const url = String(reader.result ?? "")
            resolve(url.slice(url.indexOf(",") + 1))
        }
        reader.onerror = () => reject(reader.error)
        reader.readAsDataURL(file)
    })
}

/** What the user reads of a failed request. */
function errorMessage(err: Error): string {
    if (err instanceof ApiError) return err.parsedBody?.message ?? `Ошибка ${err.status}`
    return err.message || "Не удалось обработать файл"
}

export function CatalogImportDialog({ open, onOpenChange, basePath, title, onImported }: CatalogImportDialogProps) {
    const [file, setFile] = useState<{ name: string; content: string } | null>(null)
    const [preview, setPreview] = useState<Preview | null>(null)
    const [mapping, setMapping] = useState<Record<string, string>>({})
    const [matchByCode, setMatchByCode] = useState(true)
    const [result, setResult] = useState<Result | null>(null)
    const [busy, setBusy] = useState(false)

    const reset = useCallback(() => {
        setFile(null)
        setPreview(null)
        setMapping({})
        setResult(null)
    }, [])

    const close = (next: boolean) => {
        if (!next) reset()
        onOpenChange(next)
    }

    const choose = async (chosen: File | undefined) => {
        if (!chosen) return
        setBusy(true)
        setResult(null)
        try {
            const content = await readBase64(chosen)
            const answer = await apiFetch<Preview>(`${basePath}/import/preview`, {
                method: "POST",
                body: JSON.stringify({ fileName: chosen.name, content }),
            })
            setFile({ name: chosen.name, content })
            setPreview(answer)
            setMapping(Object.fromEntries(answer.columns.map((c) => [c.header, c.field])))
        } catch (err) {
            toast.error(err instanceof Error ? errorMessage(err) : "Не удалось обработать файл")
        } finally {
            setBusy(false)
        }
    }

    const load = async (dryRun: boolean) => {
        if (!file) return
        setBusy(true)
        try {
            const chosen = Object.fromEntries(Object.entries(mapping).filter(([, field]) => field))
            const answer = await apiFetch<Result>(`${basePath}/import`, {
                method: "POST",
                body: JSON.stringify({ fileName: file.name, content: file.content, mapping: chosen, matchByCode, dryRun }),
            })
            setResult(answer)
            if (answer.applied) {
                toast.success(`Загружено: создано ${answer.created}, обновлено ${answer.updated}`)
                onImported()
                close(false)
            }
        } catch (err) {
            toast.error(err instanceof Error ? errorMessage(err) : "Не удалось обработать файл")
        } finally {
            setBusy(false)
        }
    }

    const fields = preview?.fields ?? []
    const taken = new Set(Object.values(mapping).filter(Boolean))
    const sample = (index: number) =>
        (preview?.rows ?? []).slice(0, 3).map((row) => row[index]).filter(Boolean).join(", ")

    return (
        <Dialog open={open} onOpenChange={close}>
            <DialogContent className="max-w-3xl">
                <DialogHeader>
                    <DialogTitle>Загрузка из файла: {title}</DialogTitle>
                    <DialogDescription>
                        XLSX или CSV, первая строка — заголовки колонок. Ссылки ищутся по коду или наименованию.
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-4">
                    <label className="flex cursor-pointer items-center gap-3 rounded-md border border-dashed p-3 text-sm hover:bg-muted/50">
                        <FileUp className="size-4 text-muted-foreground" />
                        <span className="flex-1">
                            {file ? `${file.name} — строк: ${preview?.total ?? 0}` : "Выберите файл"}
                        </span>
                        <input
                            type="file"
                            accept=".xlsx,.csv,.txt"
                            className="hidden"
                            disabled={busy}
                            onChange={(e) => { void choose(e.target.files?.[0]); e.target.value = "" }}
                        />
                    </label>

                    {preview && (
                        <ScrollArea className="max-h-72 rounded-md border">
                            <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_220px] gap-x-3 gap-y-1 p-3 text-sm">
                                <div className="text-xs font-medium text-muted-foreground">Колонка файла</div>
                                <div className="text-xs font-medium text-muted-foreground">Значения</div>
                                <div className="text-xs font-medium text-muted-foreground">Поле справочника</div>
                                {preview.columns.map((column, index) => (
                                    <div key={column.header} className="contents">
                                        <div className="truncate py-1.5">{column.header}</div>
                                        <div className="truncate py-1.5 text-muted-foreground">{sample(index)}</div>
                                        <Select
                                            value={mapping[column.header] || SKIP}
                                            onValueChange={(value) => setMapping((m) => ({ ...m, [column.header]: value === SKIP ? "" : value }))}
                                        >
                                            <SelectTrigger className="h-8 text-sm"><SelectValue /></SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value={SKIP}>Не загружать</SelectItem>
                                                {fields.map((field) => (
                                                    <SelectItem
                                                        key={field.name}
                                                        value={field.name}
                                                        disabled={taken.has(field.name) && mapping[column.header] !== field.name}
                                                    >
                                                        {field.label}{field.required ? " *" : ""}
                                                    </SelectItem>
                                                ))}
                                            </SelectContent>
                                        </Select>
                                    </div>
                                ))}
                            </div>
                        </ScrollArea>
                    )}

                    {preview && (
                        <div className="flex items-center gap-2">
                            <Checkbox id="import-match-code" checked={matchByCode} onCheckedChange={(v) => setMatchByCode(v === true)} />
                            <Label htmlFor="import-match-code" className="text-sm font-normal">
                                Обновлять элементы с тем же кодом
                            </Label>
                        </div>
                    )}

                    {result && (
                        <div className="space-y-2 rounded-md border p-3 text-sm">
                            <p>
                                {result.dryRun && result.errors.length === 0 ? "Проверка прошла: " : ""}
                                будет создано {result.created}, обновлено {result.updated}
                                {result.errors.length > 0 && `, строк с ошибками: ${result.errors.length} — файл не загружен`}
                            </p>
                            {result.errors.length > 0 && (
                                <ScrollArea className="max-h-40">
                                    <ul className="space-y-0.5 text-destructive">
                                        {result.errors.slice(0, 100).map((e, i) => (
                                            <li key={i}>
                                                Строка {e.line}{e.column ? `, «${e.column}»` : ""}: {e.message}
                                            </li>
                                        ))}
                                    </ul>
                                </ScrollArea>
                            )}
                        </div>
                    )}
                </div>

                <DialogFooter>
                    <Button variant="ghost" onClick={() => close(false)}>Отмена</Button>
                    <Button variant="outline" disabled={!preview || busy} onClick={() => void load(true)}>Проверить</Button>
                    <Button disabled={!preview || busy} onClick={() => void load(false)}>Загрузить</Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
