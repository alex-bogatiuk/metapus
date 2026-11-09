"use client";
import { useParams } from "next/navigation";
import Link from "next/link";
import { AlertCircle, RefreshCw } from "lucide-react";
import { InvoiceOutcomePage } from "@/components/portal/invoice-outcome";
import { usePortalInvoiceDetail } from "@/hooks/use-portal-invoice-detail";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";

export default function InvoiceDetailPage() {
  const { id } = useParams<{ id: string }>();
  const result = usePortalInvoiceDetail(id);
  if (!result.data && result.error)
    return (
      <div className="flex flex-col gap-4 p-6">
        <Alert variant="destructive">
          <AlertCircle />
          <AlertTitle>Не удалось загрузить инвойс</AlertTitle>
          <AlertDescription>
            Проверьте доступ к выбранному мерчанту и попробуйте ещё раз.
          </AlertDescription>
        </Alert>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            disabled={result.isValidating}
            onClick={() => void result.refresh()}
          >
            <RefreshCw data-icon="inline-start" />
            Повторить
          </Button>
          <Button variant="outline" asChild>
            <Link href="/portal/invoices">К инвойсам</Link>
          </Button>
        </div>
      </div>
    );
  if (result.loading || !result.data)
    return (
      <div aria-label="Загрузка инвойса" className="flex flex-col gap-5 p-6">
        <Skeleton className="h-12 w-2/3" />
        <Skeleton className="h-72 w-full" />
        <div className="grid gap-5 lg:grid-cols-3">
          <Skeleton className="h-96 lg:col-span-2" />
          <Skeleton className="h-80" />
        </div>
      </div>
    );
  return (
    <InvoiceOutcomePage
      key={result.contextKey}
      invoice={result.data}
      refreshing={result.isValidating}
      refresh={() => void result.refresh()}
      refreshError={!!result.error}
    />
  );
}
