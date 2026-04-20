"use client"

import AutoList from "@/components/shared/auto-list"

export default function WarehousesListPage() {
    return <AutoList entityName="Warehouse" entityType="catalog" routePrefix="warehouses" />
}
