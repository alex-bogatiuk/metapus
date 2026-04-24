import { DataTableSkeleton } from "@/components/shared/data-table-skeleton"

export default function GlobalLoading() {
    return <DataTableSkeleton rows={10} columns={5} showCheckbox showPrefix />
}
