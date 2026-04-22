"use client"

import { useParams } from "next/navigation"
import { useReportPage } from "@/hooks/useReportPage"
import { ReportPage } from "@/components/shared/report-page"

export default function ReportPageRoute() {
    const params = useParams()
    const key = params.key as string

    const report = useReportPage(key)
    
    return (
        <div className="h-full bg-background flex flex-col">
            <ReportPage report={report} />
        </div>
    )
}
