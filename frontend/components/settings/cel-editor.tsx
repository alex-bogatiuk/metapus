"use client"

import { useCallback, useRef, useEffect } from "react"
import Editor, { loader, type OnMount, type Monaco } from "@monaco-editor/react"
import type { editor, languages, IRange } from "monaco-editor"
import { Loader2 } from "lucide-react"

/**
 * Preload Monaco editor from CDN.
 * Call this early (e.g. on profile page mount) so Monaco is ready
 * when the user opens the CEL tab — avoids multi-second loading spinner.
 */
export function preloadMonaco() {
  loader.init().catch(() => {})
}

// ── Types ────────────────────────────────────────────────────────────

interface CelField {
  name: string
  label?: string
  type: string
}

interface CelEditorProps {
  value: string
  onChange: (value: string) => void
  /** Available document fields for autocomplete */
  fields?: CelField[]
  placeholder?: string
  /** Height in pixels (default 80) */
  height?: number
  readOnly?: boolean
  className?: string
}

// ── CEL language tokens ─────────────────────────────────────────────

const CEL_KEYWORDS = [
  "true", "false", "null", "in", "has", "size", "type",
  "matches", "startsWith", "endsWith", "contains",
  "int", "uint", "double", "string", "bool", "bytes",
  "list", "map", "timestamp", "duration",
]

const CEL_OPERATORS = ["&&", "||", "!", "==", "!=", "<", ">", "<=", ">=", "+", "-", "*", "/", "%", "?", ":"]

// ── Component ────────────────────────────────────────────────────────

export function CelEditor({
  value,
  onChange,
  fields = [],
  placeholder = 'doc.amount > 10000 && doc.posted == false',
  height = 80,
  readOnly = false,
  className,
}: CelEditorProps) {
  const editorRef = useRef<editor.IStandaloneCodeEditor | null>(null)
  const monacoRef = useRef<Monaco | null>(null)
  const disposablesRef = useRef<{ dispose(): void }[]>([])

  const registerCelLanguage = useCallback((monaco: Monaco) => {
    // Register language only once
    if (monaco.languages.getLanguages().some((l: { id: string }) => l.id === "cel")) return

    monaco.languages.register({ id: "cel" })

    // Syntax highlighting (monarch tokenizer)
    monaco.languages.setMonarchTokensProvider("cel", {
      keywords: CEL_KEYWORDS,
      operators: CEL_OPERATORS,
      tokenizer: {
        root: [
          // Identifiers & keywords
          [/[a-zA-Z_]\w*/, {
            cases: {
              "@keywords": "keyword",
              "@default": "identifier",
            },
          }],
          // Numbers
          [/\d+(\.\d+)?([eE][+-]?\d+)?/, "number"],
          // Strings
          [/"([^"\\]|\\.)*"/, "string"],
          [/'([^'\\]|\\.)*'/, "string"],
          // Operators
          [/[<>!=]=?/, "operator"],
          [/[&|]{2}/, "operator"],
          [/[+\-*/%]/, "operator"],
          // Delimiters
          [/[()[\]{}.,?:]/, "delimiter"],
          // Whitespace
          [/\s+/, "white"],
          // Comments
          [/\/\/.*$/, "comment"],
        ],
      },
    })

    // Language configuration (brackets, auto-closing)
    monaco.languages.setLanguageConfiguration("cel", {
      brackets: [
        ["(", ")"],
        ["[", "]"],
        ["{", "}"],
      ],
      autoClosingPairs: [
        { open: "(", close: ")" },
        { open: "[", close: "]" },
        { open: "{", close: "}" },
        { open: '"', close: '"' },
        { open: "'", close: "'" },
      ],
      surroundingPairs: [
        { open: "(", close: ")" },
        { open: "[", close: "]" },
        { open: '"', close: '"' },
      ],
    })
  }, [])

  // Register/update autocomplete when fields change
  useEffect(() => {
    const monaco = monacoRef.current
    if (!monaco) return

    // Dispose previous completions
    disposablesRef.current.forEach((d) => d.dispose())
    disposablesRef.current = []

    const provider = monaco.languages.registerCompletionItemProvider("cel", {
      triggerCharacters: [".", " "],
      provideCompletionItems: (model: editor.ITextModel, position: { lineNumber: number; column: number }) => {
        const word = model.getWordUntilPosition(position)
        const range = {
          startLineNumber: position.lineNumber,
          endLineNumber: position.lineNumber,
          startColumn: word.startColumn,
          endColumn: word.endColumn,
        }

        // Check if typing after "doc."
        const lineContent = model.getLineContent(position.lineNumber)
        const textBefore = lineContent.substring(0, position.column - 1)
        const afterDoc = textBefore.endsWith("doc.")

        const suggestions: languages.CompletionItem[] = []

        if (afterDoc) {
          // Suggest document fields
          for (const field of fields) {
            suggestions.push({
              label: field.name,
              kind: monaco.languages.CompletionItemKind.Field,
              detail: `${field.type}${field.label ? ` — ${field.label}` : ""}`,
              insertText: field.name,
              range: range as IRange,
            } as languages.CompletionItem)
          }
        } else {
          // Suggest "doc" prefix
          suggestions.push({
            label: "doc",
            kind: monaco.languages.CompletionItemKind.Variable,
            detail: "Документ",
            insertText: "doc",
            range: range as IRange,
          } as languages.CompletionItem)

          // Suggest "action" variable
          suggestions.push({
            label: "action",
            kind: monaco.languages.CompletionItemKind.Variable,
            detail: "Действие (create, read, update, delete, post, unpost)",
            insertText: "action",
            range: range as IRange,
          } as languages.CompletionItem)

          // Suggest keywords
          for (const kw of CEL_KEYWORDS) {
            suggestions.push({
              label: kw,
              kind: monaco.languages.CompletionItemKind.Keyword,
              insertText: kw,
              range: range as IRange,
            } as languages.CompletionItem)
          }

          // Suggest doc.field snippets
          for (const field of fields) {
            suggestions.push({
              label: `doc.${field.name}`,
              kind: monaco.languages.CompletionItemKind.Snippet,
              detail: `${field.type}${field.label ? ` — ${field.label}` : ""}`,
              insertText: `doc.${field.name}`,
              range: range as IRange,
            } as languages.CompletionItem)
          }
        }

        return { suggestions }
      },
    })

    disposablesRef.current.push(provider)

    return () => {
      disposablesRef.current.forEach((d) => d.dispose())
      disposablesRef.current = []
    }
  }, [fields])

  const handleMount: OnMount = (editor, monaco) => {
    editorRef.current = editor
    monacoRef.current = monaco

    registerCelLanguage(monaco)

    // Configure editor-level options
    editor.updateOptions({
      minimap: { enabled: false },
      scrollBeyondLastLine: false,
      lineNumbers: "off",
      glyphMargin: false,
      folding: false,
      lineDecorationsWidth: 8,
      lineNumbersMinChars: 0,
      renderLineHighlight: "none",
      overviewRulerBorder: false,
      overviewRulerLanes: 0,
      hideCursorInOverviewRuler: true,
      scrollbar: {
        vertical: "hidden",
        horizontal: "auto",
        verticalScrollbarSize: 0,
      },
      wordWrap: "on",
      fontSize: 12,
      fontFamily: "var(--font-mono, 'JetBrains Mono', 'Fira Code', monospace)",
      tabSize: 2,
      padding: { top: 8, bottom: 8 },
      readOnly,
      placeholder,
    })
  }

  return (
    <div className={className}>
      <Editor
        height={height}
        language="cel"
        value={value}
        onChange={(v) => onChange(v ?? "")}
        onMount={handleMount}
        theme="vs"
        loading={
          <div className="flex items-center gap-2 py-3 text-xs text-muted-foreground">
            <Loader2 className="h-3 w-3 animate-spin" />
            Загрузка редактора...
          </div>
        }
        options={{
          minimap: { enabled: false },
          scrollBeyondLastLine: false,
          lineNumbers: "off",
          wordWrap: "on",
          fontSize: 12,
          readOnly,
        }}
      />
    </div>
  )
}
