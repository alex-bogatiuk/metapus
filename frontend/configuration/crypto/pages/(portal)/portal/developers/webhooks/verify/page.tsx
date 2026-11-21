"use client"

import { useState } from "react"
import Link from "next/link"
import { ArrowLeft, BookOpen, Check, Copy } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  return (
    <Button
      variant="ghost"
      size="icon"
      className="absolute right-2 top-2 size-7 opacity-0 transition-opacity group-hover:opacity-100"
      aria-label="Скопировать пример"
      onClick={() => {
        void navigator.clipboard?.writeText(text)
        setCopied(true)
        window.setTimeout(() => setCopied(false), 2000)
      }}
    >
      {copied ? <Check className="size-3 text-green-500" /> : <Copy className="size-3" />}
    </Button>
  )
}

function CodeBlock({ code }: { code: string }) {
  return (
    <div className="group relative">
      <pre className="overflow-x-auto rounded-lg border bg-muted/50 p-4 font-mono text-sm leading-relaxed">
        <code>{code}</code>
      </pre>
      <CopyButton text={code} />
    </div>
  )
}

const CODE_NODE = `import express from "express";
import { verifyWebhook, MetapusClient } from "@metapus/merchant-sdk";

const app = express();
const metapus = new MetapusClient(process.env.METAPUS_URL!, process.env.METAPUS_API_KEY!);

// Register this route before express.json(): the verifier needs the raw bytes.
app.post("/webhooks/metapus", express.raw({ type: "application/json" }), async (req, res) => {
  try {
    const event = verifyWebhook(
      req.body,
      String(req.header("Metapus-Signature") ?? ""),
      process.env.METAPUS_WEBHOOK_SECRET!
    );

    // Insert event.id and enqueue the business action in one DB transaction.
    const accepted = await inbox.insertOnceAndEnqueue(event.id, event);
    if (accepted && event.type === "invoice.completed") {
      const invoice = await metapus.getInvoice(String(event.data.object.id));
      if (invoice.status !== "completed") throw new Error("invoice is not canonical-completed");
    }
    res.sendStatus(200);
  } catch {
    res.sendStatus(401);
  }
});`

const CODE_PYTHON = `from flask import Flask, request, abort
from metapus import Client, verify_webhook

app = Flask(__name__)
client = Client(METAPUS_URL, METAPUS_API_KEY)

@app.post("/webhooks/metapus")
def metapus_webhook():
    raw_body = request.get_data(cache=False)
    try:
        event = verify_webhook(
            raw_body,
            request.headers.get("Metapus-Signature", ""),
            METAPUS_WEBHOOK_SECRET,
        )
    except ValueError:
        abort(401)

    # UNIQUE(event_id) and the queue insert belong to the same transaction.
    accepted = inbox.insert_once_and_enqueue(event["id"], event)
    if accepted and event["type"] == "invoice.completed":
        invoice = client.get_invoice(event["data"]["object"]["id"])
        if invoice["status"] != "completed":
            abort(409)
    return "", 200`

const CODE_GO = `package webhooks

import (
    "io"
    "net/http"
    "time"

    metapus "github.com/metapus/merchant-sdk-go/metapus"
)

func Handler(w http.ResponseWriter, r *http.Request) {
    rawBody, err := io.ReadAll(r.Body)
    if err != nil {
        http.Error(w, "bad body", http.StatusBadRequest)
        return
    }
    event, err := metapus.VerifyWebhook[map[string]any](
        rawBody,
        r.Header.Get("Metapus-Signature"),
        webhookSecret,
        time.Now(),
        5*time.Minute,
    )
    if err != nil {
        http.Error(w, "invalid signature", http.StatusUnauthorized)
        return
    }

    // UNIQUE(event.ID) and the business outbox insert are one transaction.
    if err := inbox.InsertOnceAndEnqueue(r.Context(), event.ID, event); err != nil {
        http.Error(w, "retry", http.StatusServiceUnavailable)
        return
    }
    w.WriteHeader(http.StatusOK)
}`

export default function WebhookVerifyPage() {
  return (
    <div className="max-w-4xl space-y-6">
      <div className="flex items-center gap-3">
        <Button variant="ghost" size="icon" asChild>
          <Link href="/portal/developers/webhooks" aria-label="Вернуться к вебхукам">
            <ArrowLeft data-icon="inline-start" />
          </Link>
        </Button>
        <div>
          <h1 className="flex items-center gap-2 text-2xl font-bold tracking-tight">
            <BookOpen className="size-5 text-muted-foreground" />
            Руководство по вебхукам
          </h1>
          <p className="text-sm text-muted-foreground">Проверка подписи, примеры SDK и правила обработки событий.</p>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Контракт подписи</CardTitle>
          <CardDescription>Не разбирайте и не сериализуйте JSON до проверки подписи.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3 text-sm">
          <div className="rounded-lg border bg-muted/30 p-4 font-mono text-xs leading-relaxed">
            <div>signedPayload = unixTimestamp + &quot;.&quot; + rawBody</div>
            <div>v1 = HMAC-SHA256(signedPayload, endpointSecret)</div>
          </div>
          <p className="text-muted-foreground">
            Заголовок имеет вид <code>t=&lt;unix&gt;,v1=&lt;hex&gt;</code>. SDK отклоняет timestamp старше пяти минут и
            сравнивает HMAC constant-time. Во время 24-часовой ротации заголовок содержит две подписи <code>v1</code>.
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Заголовки доставки</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow><TableHead>Заголовок</TableHead><TableHead>Назначение</TableHead></TableRow>
            </TableHeader>
            <TableBody>
              <TableRow>
                <TableCell><code>Metapus-Signature</code></TableCell>
                <TableCell>Timestamp и одна или две HMAC-подписи.</TableCell>
              </TableRow>
              <TableRow>
                <TableCell><code>Metapus-Event-Id</code></TableCell>
                <TableCell>Стабильный ID события. Сохраняйте его в UNIQUE-колонке для дедупликации.</TableCell>
              </TableRow>
              <TableRow>
                <TableCell><code>Metapus-Delivery-Id</code></TableCell>
                <TableCell>Новый ID каждой HTTP-попытки. Подходит для трассировки, но не для дедупликации.</TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Официальные SDK</CardTitle>
          <CardDescription>Verifier принимает неизменённые bytes и весь заголовок Metapus-Signature.</CardDescription>
        </CardHeader>
        <CardContent>
          <Tabs defaultValue="node">
            <TabsList>
              <TabsTrigger value="node">TypeScript</TabsTrigger>
              <TabsTrigger value="python">Python</TabsTrigger>
              <TabsTrigger value="go">Go</TabsTrigger>
            </TabsList>
            <TabsContent value="node" className="mt-4"><CodeBlock code={CODE_NODE} /></TabsContent>
            <TabsContent value="python" className="mt-4"><CodeBlock code={CODE_PYTHON} /></TabsContent>
            <TabsContent value="go" className="mt-4"><CodeBlock code={CODE_GO} /></TabsContent>
          </Tabs>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle className="text-base">Правила обработчика</CardTitle></CardHeader>
        <CardContent className="space-y-3 text-sm text-muted-foreground">
          <p><Badge variant="outline">1</Badge> Проверьте подпись по raw body и freshness timestamp.</p>
          <p><Badge variant="outline">2</Badge> В одной транзакции вставьте <code>event.id</code> и поставьте бизнес-действие в свою очередь.</p>
          <p><Badge variant="outline">3</Badge> Быстро ответьте любым <code>2xx</code>. Non-2xx, timeout и network error повторяются до 72 часов.</p>
          <p><Badge variant="outline">4</Badge> События могут повторяться и приходить не по порядку. Перед выдачей товара получите канонический invoice через API.</p>
          <p><Badge variant="outline">5</Badge> Выдавайте товар только по <code>invoice.completed</code>, никогда по <code>paid</code>, <code>confirmed</code> или return URL.</p>
        </CardContent>
      </Card>
    </div>
  )
}
