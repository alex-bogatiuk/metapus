"use client"

import { useState } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Badge } from "@/components/ui/badge"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Button } from "@/components/ui/button"
import { Copy, Check, ShieldCheck, ArrowLeft } from "lucide-react"
import Link from "next/link"

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <Button
      variant="ghost"
      size="icon"
      className="absolute top-2 right-2 size-7 opacity-0 group-hover:opacity-100 transition-opacity"
      onClick={() => {
        navigator.clipboard?.writeText(text)
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
      }}
    >
      {copied ? <Check className="size-3 text-green-500" /> : <Copy className="size-3" />}
    </Button>
  )
}

function CodeBlock({ code, lang }: { code: string; lang: string }) {
  return (
    <div className="relative group">
      <pre className="rounded-lg border bg-muted/50 p-4 overflow-x-auto text-sm font-mono leading-relaxed">
        <code>{code}</code>
      </pre>
      <CopyButton text={code} />
    </div>
  )
}

const CODE_NODE = `import crypto from 'crypto';

function verifyWebhookSignature(
  payload: string,      // raw request body
  signature: string,    // X-Metapus-Signature header
  timestamp: string,    // X-Metapus-Timestamp header
  secret: string        // your webhook signing secret
): boolean {
  // 1. Build signed content
  const signedContent = \`\${timestamp}.\${payload}\`;

  // 2. Compute expected signature
  const expected = crypto
    .createHmac('sha256', secret)
    .update(signedContent, 'utf8')
    .digest('hex');

  // 3. Compare using timing-safe equality
  try {
    return crypto.timingSafeEqual(
      Buffer.from(expected, 'hex'),
      Buffer.from(signature, 'hex')
    );
  } catch {
    return false;
  }
}

// Usage in Express handler:
app.post('/webhook', express.raw({ type: '*/*' }), (req, res) => {
  const sig = req.headers['x-metapus-signature'] as string;
  const ts  = req.headers['x-metapus-timestamp'] as string;

  if (!verifyWebhookSignature(req.body.toString(), sig, ts, WEBHOOK_SECRET)) {
    return res.status(401).send('Invalid signature');
  }

  // ✅ Signature valid — process event
  const event = JSON.parse(req.body.toString());
  console.log('Event:', event.event, event.data);
  res.status(200).send('OK');
});`

const CODE_PYTHON = `import hmac
import hashlib
import time
from flask import Flask, request, abort

WEBHOOK_SECRET = "whsec_..."  # your signing secret

def verify_webhook(payload: bytes, signature: str, timestamp: str) -> bool:
    """Verify Metapus webhook signature."""
    # 1. Build signed content
    signed_content = f"{timestamp}.{payload.decode('utf-8')}"

    # 2. Compute expected signature
    expected = hmac.new(
        WEBHOOK_SECRET.encode('utf-8'),
        signed_content.encode('utf-8'),
        hashlib.sha256
    ).hexdigest()

    # 3. Timing-safe comparison
    return hmac.compare_digest(expected, signature)


app = Flask(__name__)

@app.route("/webhook", methods=["POST"])
def handle_webhook():
    sig = request.headers.get("X-Metapus-Signature", "")
    ts  = request.headers.get("X-Metapus-Timestamp", "")

    # Replay protection: reject events older than 5 minutes
    if abs(time.time() - int(ts)) > 300:
        abort(401, "Timestamp too old")

    if not verify_webhook(request.data, sig, ts):
        abort(401, "Invalid signature")

    event = request.get_json()
    print(f"Event: {event['event']}")
    return "OK", 200`

const CODE_GO = `package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"
)

const webhookSecret = "whsec_..." // your signing secret

func verifyWebhookSignature(body []byte, signature, timestamp string) bool {
	// 1. Build signed content
	signedContent := timestamp + "." + string(body)

	// 2. Compute expected signature
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write([]byte(signedContent))
	expected := hex.EncodeToString(mac.Sum(nil))

	// 3. Timing-safe comparison
	return hmac.Equal([]byte(expected), []byte(signature))
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	sig := r.Header.Get("X-Metapus-Signature")
	ts  := r.Header.Get("X-Metapus-Timestamp")

	// Replay protection
	tsInt, _ := strconv.ParseInt(ts, 10, 64)
	if math.Abs(float64(time.Now().Unix()-tsInt)) > 300 {
		http.Error(w, "Timestamp too old", http.StatusUnauthorized)
		return
	}

	if !verifyWebhookSignature(body, sig, ts) {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	fmt.Println("✅ Webhook verified, body:", string(body))
	w.WriteHeader(http.StatusOK)
}`

const CODE_PHP = `<?php
$webhookSecret = 'whsec_...'; // your signing secret

$payload   = file_get_contents('php://input');
$signature = $_SERVER['HTTP_X_METAPUS_SIGNATURE'] ?? '';
$timestamp = $_SERVER['HTTP_X_METAPUS_TIMESTAMP'] ?? '';

// 1. Replay protection: reject if older than 5 minutes
if (abs(time() - (int) $timestamp) > 300) {
    http_response_code(401);
    die('Timestamp too old');
}

// 2. Compute expected signature
$signedContent = $timestamp . '.' . $payload;
$expected = hash_hmac('sha256', $signedContent, $webhookSecret);

// 3. Timing-safe comparison
if (!hash_equals($expected, $signature)) {
    http_response_code(401);
    die('Invalid signature');
}

// ✅ Signature valid
$event = json_decode($payload, true);
error_log('Webhook event: ' . $event['event']);
http_response_code(200);
echo 'OK';`

export default function WebhookVerifyPage() {
  return (
    <div className="space-y-6 max-w-3xl">
      {/* Header */}
      <div className="flex items-center gap-3">
        <Button variant="ghost" size="icon" asChild>
          <Link href="/portal/settings">
            <ArrowLeft className="size-4" />
          </Link>
        </Button>
        <div>
          <h1 className="text-2xl font-bold tracking-tight flex items-center gap-2">
            <ShieldCheck className="size-5 text-muted-foreground" />
            Верификация вебхуков
          </h1>
          <p className="text-sm text-muted-foreground">
            Как проверить подлинность входящих вебхуков от Metapus
          </p>
        </div>
      </div>

      {/* How it works */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Как это работает</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3 text-sm">
          <p>
            Каждый вебхук подписывается вашим <strong>signing secret</strong> через HMAC-SHA256.
            При получении вебхука вы должны пересчитать подпись и сравнить с заголовком — это гарантирует,
            что запрос пришёл именно от Metapus.
          </p>
          <div className="rounded-lg border bg-muted/30 p-4 font-mono text-xs leading-relaxed">
            <div className="text-muted-foreground mb-1">// Алгоритм подписи:</div>
            <div>signedContent = timestamp + &quot;.&quot; + rawBody</div>
            <div>signature = HMAC-SHA256(signedContent, webhookSecret)</div>
          </div>
        </CardContent>
      </Card>

      {/* Headers */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Заголовки вебхука</CardTitle>
          <CardDescription>
            Metapus отправляет следующие заголовки с каждым вебхуком
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Заголовок</TableHead>
                <TableHead>Описание</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow>
                <TableCell>
                  <code className="text-xs bg-muted px-1.5 py-0.5 rounded">X-Metapus-Signature</code>
                </TableCell>
                <TableCell className="text-sm">
                  HMAC-SHA256 подпись в hex-формате
                </TableCell>
              </TableRow>
              <TableRow>
                <TableCell>
                  <code className="text-xs bg-muted px-1.5 py-0.5 rounded">X-Metapus-Timestamp</code>
                </TableCell>
                <TableCell className="text-sm">
                  Unix-время отправки (секунды). Используйте для replay protection (±5 мин)
                </TableCell>
              </TableRow>
              <TableRow>
                <TableCell>
                  <code className="text-xs bg-muted px-1.5 py-0.5 rounded">X-Metapus-Delivery-ID</code>
                </TableCell>
                <TableCell className="text-sm">
                  Уникальный UUID доставки. Используйте для <strong>идемпотентности</strong> — дедупликации
                  повторных доставок
                </TableCell>
              </TableRow>
              <TableRow>
                <TableCell>
                  <code className="text-xs bg-muted px-1.5 py-0.5 rounded">X-Metapus-Event</code>
                </TableCell>
                <TableCell className="text-sm">
                  Тип события: <Badge variant="outline" className="text-[10px] ml-1">invoice.paid</Badge>{" "}
                  <Badge variant="outline" className="text-[10px]">invoice.confirmed</Badge>{" "}
                  <Badge variant="outline" className="text-[10px]">invoice.expired</Badge>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {/* Code Samples */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Примеры кода</CardTitle>
          <CardDescription>
            Готовые примеры верификации для популярных языков
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Tabs defaultValue="node" className="w-full">
            <TabsList>
              <TabsTrigger value="node">Node.js / TS</TabsTrigger>
              <TabsTrigger value="python">Python</TabsTrigger>
              <TabsTrigger value="go">Go</TabsTrigger>
              <TabsTrigger value="php">PHP</TabsTrigger>
            </TabsList>
            <TabsContent value="node" className="mt-4">
              <CodeBlock code={CODE_NODE} lang="typescript" />
            </TabsContent>
            <TabsContent value="python" className="mt-4">
              <CodeBlock code={CODE_PYTHON} lang="python" />
            </TabsContent>
            <TabsContent value="go" className="mt-4">
              <CodeBlock code={CODE_GO} lang="go" />
            </TabsContent>
            <TabsContent value="php" className="mt-4">
              <CodeBlock code={CODE_PHP} lang="php" />
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>

      {/* Best Practices */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Рекомендации</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4 text-sm">
          <div>
            <h3 className="font-medium mb-1">🕐 Replay Protection</h3>
            <p className="text-muted-foreground">
              Проверяйте <code className="text-xs bg-muted px-1 py-0.5 rounded">X-Metapus-Timestamp</code>.
              Отклоняйте события старше 5 минут. Это защищает от replay-атак, если подпись была перехвачена.
            </p>
          </div>
          <div>
            <h3 className="font-medium mb-1">🔁 Идемпотентность</h3>
            <p className="text-muted-foreground">
              Сохраняйте <code className="text-xs bg-muted px-1 py-0.5 rounded">X-Metapus-Delivery-ID</code>{" "}
              и проверяйте его перед обработкой. Metapus использует exponential backoff (5 попыток: 1мин → 5мин → 30мин → 2ч → 12ч),
              поэтому один и тот же вебхук может прийти повторно.
            </p>
          </div>
          <div>
            <h3 className="font-medium mb-1">⚡ Быстрый ответ</h3>
            <p className="text-muted-foreground">
              Отвечайте <code className="text-xs bg-muted px-1 py-0.5 rounded">200 OK</code> как можно быстрее.
              Тяжёлую обработку выполняйте асинхронно. Если ваш сервер не ответит за 10 секунд, вебхук
              будет помечен как неуспешный.
            </p>
          </div>
          <div>
            <h3 className="font-medium mb-1">🔐 Ротация секрета</h3>
            <p className="text-muted-foreground">
              Регулярно ротируйте signing secret в{" "}
              <Link href="/portal/settings" className="text-primary hover:underline">
                настройках
              </Link>.
              После ротации старый секрет немедленно перестаёт работать — обновите его на вашей стороне
              сразу.
            </p>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
