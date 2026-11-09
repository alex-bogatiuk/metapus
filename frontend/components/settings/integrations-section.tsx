"use client"

import { useCallback, useEffect, useState } from "react"
import { Send, Radio, KeyRound, Clock } from "lucide-react"

import { api } from "@/lib/api"
import type { AutomationAccount, AutomationChannel, AutomationRule } from "@/types/automation"

import { Badge } from "@/components/ui/badge"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { TooltipProvider } from "@/components/ui/tooltip"

import { AccountsTab } from "./integrations/accounts-tab"
import { ChannelsTab } from "./integrations/channels-tab"
import { RulesTab } from "./integrations/rules-tab"
import { HistoryTab } from "./integrations/history-tab"

export function IntegrationsSection() {
  const [accounts, setAccounts] = useState<AutomationAccount[]>([])
  const [channels, setChannels] = useState<AutomationChannel[]>([])
  const [rules, setRules] = useState<AutomationRule[]>([])
  const [loading, setLoading] = useState(true)

  const fetchAccounts = useCallback(async () => {
    try {
      const data = await api.automation.accounts.list()
      setAccounts(data)
    } catch (e) {
      console.error(e)
    }
  }, [])

  const fetchChannels = useCallback(async () => {
    try {
      const data = await api.automation.channels.list()
      setChannels(data)
    } catch (e) {
      console.error(e)
    }
  }, [])

  const fetchRules = useCallback(async () => {
    try {
      const data = await api.automation.rules.list()
      setRules(Array.isArray(data) ? data : [])
    } catch (e) {
      console.error(e)
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    const loadInitial = async () => {
      try {
        const [accs, chans, rls] = await Promise.all([
          api.automation.accounts.list(),
          api.automation.channels.list(),
          api.automation.rules.list(),
        ])
        if (!cancelled) {
          setAccounts(accs)
          setChannels(chans)
          setRules(Array.isArray(rls) ? rls : [])
        }
      } catch (e) {
        console.error(e)
      } finally {
        if (!cancelled) {
          setLoading(false)
        }
      }
    }
    void loadInitial()
    return () => {
      cancelled = true
    }
  }, [])

  if (loading) {
    return <div className="text-sm text-muted-foreground py-4">Загрузка...</div>
  }

  return (
    <TooltipProvider>
      <Tabs defaultValue="rules" className="space-y-4">
        <div className="flex items-center justify-between">
          <TabsList>
            <TabsTrigger value="rules" className="gap-1.5">
              <Send className="h-3.5 w-3.5" />
              Правила
              {rules.length > 0 && (
                <Badge variant="secondary" className="h-5 min-w-5 px-1.5 text-[10px] rounded-full">
                  {rules.length}
                </Badge>
              )}
            </TabsTrigger>
            <TabsTrigger value="channels" className="gap-1.5">
              <Radio className="h-3.5 w-3.5" />
              Каналы
              {channels.length > 0 && (
                <Badge variant="secondary" className="h-5 min-w-5 px-1.5 text-[10px] rounded-full">
                  {channels.length}
                </Badge>
              )}
            </TabsTrigger>
            <TabsTrigger value="accounts" className="gap-1.5">
              <KeyRound className="h-3.5 w-3.5" />
              Аккаунты
              {accounts.length > 0 && (
                <Badge variant="secondary" className="h-5 min-w-5 px-1.5 text-[10px] rounded-full">
                  {accounts.length}
                </Badge>
              )}
            </TabsTrigger>
            <TabsTrigger value="history" className="gap-1.5">
              <Clock className="h-3.5 w-3.5" />
              Журнал
            </TabsTrigger>
          </TabsList>
        </div>

        <TabsContent value="rules" className="space-y-4">
          <RulesTab rules={rules} onRefresh={fetchRules} />
        </TabsContent>

        <TabsContent value="channels" className="space-y-4">
          <ChannelsTab channels={channels} accounts={accounts} onRefresh={fetchChannels} />
        </TabsContent>

        <TabsContent value="accounts" className="space-y-4">
          <AccountsTab accounts={accounts} onRefresh={fetchAccounts} />
        </TabsContent>

        <TabsContent value="history" className="flex flex-col gap-3 min-h-0">
          <HistoryTab rules={rules} channels={channels} />
        </TabsContent>
      </Tabs>
    </TooltipProvider>
  )
}
