"use client"

/**
 * Panels and field editors the configuration adds to the platform's
 * metadata-driven detail pages, registered through its entity overlays.
 */
import type { AutoFormProps } from "@/components/shared/auto-form"
import { RPCKeyNameField } from "@/components/integrations/rpc-key-name-field"
import { PaymentAMLPanel, WalletAMLPanel } from "@/configuration/crypto/components/crypto/aml-risk"
import { CryptoPaymentAdjustments } from "@/configuration/crypto/components/documents/crypto-payment-adjustments"
import { CryptoPaymentReceipt } from "@/configuration/crypto/components/documents/crypto-payment-receipt"

export function WalletDetailPanels({ id }: { id: string }) {
    return <WalletAMLPanel walletId={id} />
}

export function CryptoPaymentDetailPanels({ id }: { id: string }) {
    return (
        <div className="space-y-4">
            <CryptoPaymentReceipt paymentId={id} />
            <PaymentAMLPanel paymentId={id} />
            <CryptoPaymentAdjustments paymentId={id} />
        </div>
    )
}

// RPC connections choose the account key from their pool, not as free text.
export const renderRPCEndpointField: NonNullable<AutoFormProps["renderField"]> = (field, context) =>
    field.name === "credentialSlot"
        ? <RPCKeyNameField value={context.value} values={context.values} onChange={context.onChange} readOnly={context.readOnly} />
        : undefined
