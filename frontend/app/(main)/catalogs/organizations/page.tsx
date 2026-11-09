"use client"

import AutoList from "@/components/shared/auto-list"

export default function OrganizationsListPage() {
    return <AutoList entityName="Organization" entityType="catalog" routePrefix="organizations" />
}
