"use client"

import { useState, useEffect } from "react"
import { useRouter, useParams, usePathname } from "next/navigation"
import { Loader2 } from "lucide-react"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { ReferenceField } from "@/components/shared/reference-field"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { Switch } from "@/components/ui/switch"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useTabDirty } from "@/hooks/useTabDirty"
import { useTabTitle } from "@/hooks/useTabTitle"
import { useFormDraft } from "@/hooks/useFormDraft"
import { api } from "@/lib/api"
import type { NomenclatureType, UpdateNomenclatureRequest } from "@/types/catalog"
import { NOMENCLATURE_TYPE_LABELS } from "@/types/catalog"

interface NomenclatureEditState {
  code: string
  name: string
  type: NomenclatureType
  article: string
  barcode: string
  baseUnitId: string
  baseUnitName: string
  defaultVatRateId: string
  defaultVatRateName: string
  description: string
  weight: string
  volume: string
  isWeighed: boolean
  trackSerial: boolean
  trackBatch: boolean
  version: number
  deletionMark: boolean
}

const INITIAL_STATE: NomenclatureEditState = {
  code: "",
  name: "",
  type: "goods",
  article: "",
  barcode: "",
  baseUnitId: "",
  baseUnitName: "",
  defaultVatRateId: "",
  defaultVatRateName: "",
  description: "",
  weight: "",
  volume: "",
  isWeighed: false,
  trackSerial: false,
  trackBatch: false,
  version: 0,
  deletionMark: false,
}

export default function NomenclatureItemPage() {
  const router = useRouter()
  const params = useParams<{ id: string }>()
  const pathname = usePathname()
  const { markDirty, markClean } = useTabDirty()
  const { state: f, update, replace, clear, hasDraft } = useFormDraft<NomenclatureEditState>(pathname, INITIAL_STATE)

  const [loading, setLoading] = useState(!hasDraft)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  useTabTitle(f.name || undefined, "Номенклатура")

  useEffect(() => {
    if (!params.id || hasDraft) return
    let cancelled = false
    setLoading(true)
    setError(null)
    api.nomenclature.get(params.id)
      .then((item) => {
        if (!cancelled) {
          replace({
            code: item.code,
            name: item.name,
            type: item.type,
            article: item.article ?? "",
            barcode: item.barcode ?? "",
            baseUnitId: item.baseUnitId ?? "",
            baseUnitName: "",
            defaultVatRateId: item.defaultVatRateId ?? "",
            defaultVatRateName: "",
            description: item.description ?? "",
            weight: item.weight,
            volume: item.volume,
            isWeighed: item.isWeighed,
            trackSerial: item.trackSerial,
            trackBatch: item.trackBatch,
            version: item.version,
            deletionMark: item.deletionMark,
          })
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
  }, [params.id, hasDraft, replace])

  const handleChange = () => markDirty()

  const buildPayload = (): UpdateNomenclatureRequest => ({
    code: f.code,
    name: f.name,
    type: f.type,
    article: f.article || null,
    barcode: f.barcode || null,
    baseUnitId: f.baseUnitId || null,
    defaultVatRateId: f.defaultVatRateId || null,
    description: f.description || null,
    weight: f.weight || "0",
    volume: f.volume || "0",
    isWeighed: f.isWeighed,
    trackSerial: f.trackSerial,
    trackBatch: f.trackBatch,
    version: f.version,
  })

  const handleSave = async (andClose: boolean) => {
    if (!f.name.trim()) {
      setError("Наименование обязательно")
      return
    }
    setSaving(true)
    setError(null)
    try {
      const updated = await api.nomenclature.update(params.id, buildPayload())
      update({ version: updated.version })
      clear()
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
        title={`${f.name || "Номенклатура"} (${f.code})`}
        status={f.deletionMark ? { label: "Помечено на удаление", variant: "destructive" } : undefined}
        primaryAction={{
          label: saving ? "Сохранение…" : "Записать и закрыть",
          variant: "success",
          onClick: () => handleSave(true),
        }}
        secondaryActions={[{ label: "Записать", onClick: () => handleSave(false) }]}
        backHref="/catalogs/nomenclature"
        backTargetId={params.id}
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
                        value={f.name}
                        onChange={(e) => { update({ name: e.target.value }); handleChange() }}
                      />
                    </div>

                    <div>
                      <Label className="text-xs text-muted-foreground">Код</Label>
                      <Input value={f.code} className="mt-1 font-mono" readOnly />
                    </div>

                    <div>
                      <Label className="text-xs text-muted-foreground">Тип *</Label>
                      <Select value={f.type} onValueChange={(v) => { update({ type: v as NomenclatureType }); handleChange() }}>
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
                      <Input className="mt-1" value={f.article} onChange={(e) => { update({ article: e.target.value }); handleChange() }} />
                    </div>

                    <div>
                      <Label className="text-xs text-muted-foreground">Штрихкод</Label>
                      <Input className="mt-1" value={f.barcode} onChange={(e) => { update({ barcode: e.target.value }); handleChange() }} />
                    </div>

                    <div>
                      <Label className="text-xs text-muted-foreground">Базовая ед. изм.</Label>
                      <div className="mt-1">
                        <ReferenceField
                          value={f.baseUnitId}
                          displayName={f.baseUnitName}
                          apiEndpoint="/catalog/units"
                          placeholder="Выберите ед. изм."
                          onChange={(id, name) => { update({ baseUnitId: id, baseUnitName: name }); handleChange() }}
                        />
                      </div>
                    </div>

                    <div>
                      <Label className="text-xs text-muted-foreground">Ставка НДС</Label>
                      <div className="mt-1">
                        <ReferenceField
                          value={f.defaultVatRateId}
                          displayName={f.defaultVatRateName}
                          apiEndpoint="/catalog/vat-rates"
                          placeholder="Выберите ставку НДС"
                          onChange={(id, name) => { update({ defaultVatRateId: id, defaultVatRateName: name }); handleChange() }}
                        />
                      </div>
                    </div>

                    <div className="md:col-span-2">
                      <Label className="text-xs text-muted-foreground">Описание</Label>
                      <Textarea
                        rows={6}
                        className="mt-1"
                        placeholder="Описание номенклатуры..."
                        value={f.description}
                        onChange={(e) => { update({ description: e.target.value }); handleChange() }}
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
                  <Input className="mt-1" type="number" step="0.001" value={f.weight} onChange={(e) => { update({ weight: e.target.value }); handleChange() }} />
                </div>
                <div>
                  <Label className="text-xs text-muted-foreground">Объем, м³</Label>
                  <Input className="mt-1" type="number" step="0.001" value={f.volume} onChange={(e) => { update({ volume: e.target.value }); handleChange() }} />
                </div>
              </div>
            </TabsContent>

            <TabsContent value="additional">
              <div className="mt-4 space-y-4">
                <div className="flex items-center gap-3">
                  <Switch checked={f.isWeighed} onCheckedChange={(v) => { update({ isWeighed: v }); handleChange() }} />
                  <Label>Весовой товар</Label>
                </div>
                <div className="flex items-center gap-3">
                  <Switch checked={f.trackSerial} onCheckedChange={(v) => { update({ trackSerial: v }); handleChange() }} />
                  <Label>Учет серийных номеров</Label>
                </div>
                <div className="flex items-center gap-3">
                  <Switch checked={f.trackBatch} onCheckedChange={(v) => { update({ trackBatch: v }); handleChange() }} />
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
