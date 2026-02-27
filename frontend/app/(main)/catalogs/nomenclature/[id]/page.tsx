"use client"

import { useState, useEffect, useCallback } from "react"
import { useRouter, useParams } from "next/navigation"
import { Loader2 } from "lucide-react"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { Switch } from "@/components/ui/switch"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useTabDirty } from "@/hooks/useTabDirty"
import { useTabTitle } from "@/hooks/useTabTitle"
import { api } from "@/lib/api"
import type { NomenclatureType, NomenclatureResponse, UpdateNomenclatureRequest } from "@/types/catalog"
import { NOMENCLATURE_TYPE_LABELS } from "@/types/catalog"

export default function NomenclatureItemPage() {
  const router = useRouter()
  const params = useParams<{ id: string }>()
  const { markDirty, markClean } = useTabDirty()

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // ── Form state matching UpdateNomenclatureRequest ──────────────────────
  const [code, setCode] = useState("")
  const [name, setName] = useState("")
  const [type, setType] = useState<NomenclatureType>("goods")
  const [article, setArticle] = useState("")
  const [barcode, setBarcode] = useState("")
  const [description, setDescription] = useState("")
  const [weight, setWeight] = useState("")
  const [volume, setVolume] = useState("")
  const [isWeighed, setIsWeighed] = useState(false)
  const [trackSerial, setTrackSerial] = useState(false)
  const [trackBatch, setTrackBatch] = useState(false)
  const [version, setVersion] = useState(0)
  const [deletionMark, setDeletionMark] = useState(false)
  useTabTitle(name || undefined, "Номенклатура")

  const populateForm = useCallback((item: NomenclatureResponse) => {
    setCode(item.code)
    setName(item.name)
    setType(item.type)
    setArticle(item.article ?? "")
    setBarcode(item.barcode ?? "")
    setDescription(item.description ?? "")
    setWeight(item.weight)
    setVolume(item.volume)
    setIsWeighed(item.isWeighed)
    setTrackSerial(item.trackSerial)
    setTrackBatch(item.trackBatch)
    setVersion(item.version)
    setDeletionMark(item.deletionMark)
  }, [])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    api.nomenclature.get(params.id)
      .then((item) => {
        if (!cancelled) {
          populateForm(item)
          setLoading(false)
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Ошибка загрузки")
          setLoading(false)
        }
      })
    return () => { cancelled = true }
  }, [params.id, populateForm])

  const handleChange = () => markDirty()

  const buildPayload = (): UpdateNomenclatureRequest => ({
    code,
    name,
    type,
    article: article || null,
    barcode: barcode || null,
    description: description || null,
    weight: weight || "0",
    volume: volume || "0",
    isWeighed,
    trackSerial,
    trackBatch,
    version,
  })

  const handleSave = async (andClose: boolean) => {
    if (!name.trim()) {
      setError("Наименование обязательно")
      return
    }
    setSaving(true)
    setError(null)
    try {
      const updated = await api.nomenclature.update(params.id, buildPayload())
      setVersion(updated.version)
      markClean()
      if (andClose) {
        router.push("/catalogs/nomenclature")
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка сохранения")
    } finally {
      setSaving(false)
    }
  }

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center text-muted-foreground">
        <Loader2 className="mr-2 h-5 w-5 animate-spin" />
        Загрузка…
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col">
      <FormToolbar
        title={`${name || "Номенклатура"} (${code})`}
        status={deletionMark ? { label: "Помечено на удаление", variant: "destructive" } : undefined}
        primaryAction={{
          label: saving ? "Сохранение…" : "Записать и закрыть",
          variant: "success",
          onClick: () => handleSave(true),
        }}
        secondaryActions={[{ label: "Записать", onClick: () => handleSave(false) }]}
        backHref="/catalogs/nomenclature"
        onClose={() => router.push("/catalogs/nomenclature")}
      />

      {error && (
        <div className="bg-destructive/10 border-b border-destructive/20 px-4 py-2 text-sm text-destructive">
          {error}
        </div>
      )}

      <div className="flex flex-1 overflow-auto">
        <div className="flex-1 overflow-auto p-4">
          <Tabs defaultValue="main">
            <TabsList>
              <TabsTrigger value="main">Главное</TabsTrigger>
              <TabsTrigger value="dimensions">Габариты и учет</TabsTrigger>
              <TabsTrigger value="additional">Дополнительно</TabsTrigger>
            </TabsList>

            <TabsContent value="main">
              <div className="mt-4 grid grid-cols-1 gap-6 lg:grid-cols-3">
                <div className="lg:col-span-1">
                  <div className="flex aspect-square items-center justify-center rounded-lg border-2 border-dashed border-muted bg-muted/30">
                    <span className="text-sm text-muted-foreground">Добавить изображение</span>
                  </div>
                </div>

                <div className="lg:col-span-2">
                  <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                    <div className="md:col-span-2">
                      <Label className="text-xs text-muted-foreground">Наименование *</Label>
                      <Input
                        className="mt-1"
                        value={name}
                        onChange={(e) => { setName(e.target.value); handleChange() }}
                      />
                    </div>

                    <div>
                      <Label className="text-xs text-muted-foreground">Код</Label>
                      <Input value={code} className="mt-1 font-mono" readOnly />
                    </div>

                    <div>
                      <Label className="text-xs text-muted-foreground">Тип *</Label>
                      <Select value={type} onValueChange={(v) => { setType(v as NomenclatureType); handleChange() }}>
                        <SelectTrigger className="mt-1"><SelectValue /></SelectTrigger>
                        <SelectContent>
                          {Object.entries(NOMENCLATURE_TYPE_LABELS).map(([k, v]) => (
                            <SelectItem key={k} value={k}>{v}</SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>

                    <div>
                      <Label className="text-xs text-muted-foreground">Артикул</Label>
                      <Input className="mt-1" value={article} onChange={(e) => { setArticle(e.target.value); handleChange() }} />
                    </div>

                    <div>
                      <Label className="text-xs text-muted-foreground">Штрихкод</Label>
                      <Input className="mt-1" value={barcode} onChange={(e) => { setBarcode(e.target.value); handleChange() }} />
                    </div>

                    <div className="md:col-span-2">
                      <Label className="text-xs text-muted-foreground">Описание</Label>
                      <Textarea
                        rows={6}
                        className="mt-1"
                        placeholder="Описание номенклатуры..."
                        value={description}
                        onChange={(e) => { setDescription(e.target.value); handleChange() }}
                      />
                    </div>
                  </div>
                </div>
              </div>
            </TabsContent>

            <TabsContent value="dimensions">
              <div className="mt-4 grid grid-cols-2 gap-4 md:grid-cols-4">
                <div>
                  <Label className="text-xs text-muted-foreground">Вес, кг</Label>
                  <Input className="mt-1" type="number" step="0.001" value={weight} onChange={(e) => { setWeight(e.target.value); handleChange() }} />
                </div>
                <div>
                  <Label className="text-xs text-muted-foreground">Объем, м³</Label>
                  <Input className="mt-1" type="number" step="0.001" value={volume} onChange={(e) => { setVolume(e.target.value); handleChange() }} />
                </div>
              </div>
            </TabsContent>

            <TabsContent value="additional">
              <div className="mt-4 space-y-4">
                <div className="flex items-center gap-3">
                  <Switch checked={isWeighed} onCheckedChange={(v) => { setIsWeighed(v); handleChange() }} />
                  <Label>Весовой товар</Label>
                </div>
                <div className="flex items-center gap-3">
                  <Switch checked={trackSerial} onCheckedChange={(v) => { setTrackSerial(v); handleChange() }} />
                  <Label>Учет серийных номеров</Label>
                </div>
                <div className="flex items-center gap-3">
                  <Switch checked={trackBatch} onCheckedChange={(v) => { setTrackBatch(v); handleChange() }} />
                  <Label>Учет партий</Label>
                </div>
              </div>
            </TabsContent>
          </Tabs>
        </div>
      </div>
    </div>
  )
}
