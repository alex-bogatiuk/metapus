import { CryptoNetworksConsole } from "@/configuration/crypto/components/admin/crypto-networks-console"

export default function CryptoNetworksPage() {
  return (
    <main className="mx-auto flex min-h-0 w-full max-w-[1400px] flex-1 flex-col gap-4 overflow-y-auto p-4">
      <header>
        <h1 className="text-2xl font-bold">Криптовалютные сети</h1>
        <p className="text-sm text-muted-foreground">
          Настройки определяют, может ли воркер выполнять операции в сети и сколько нативного токена он может расходовать на комиссии и JIT-пополнения.
        </p>
      </header>
      <CryptoNetworksConsole />
    </main>
  )
}
