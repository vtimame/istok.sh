import { useEffect, useState } from "react"

import { cn } from "@/lib/utils"

type CodeBlockProps = {
  code: string
  language?: string
  // First line number, so snippets show their position in the file.
  startLine?: number
  className?: string
}

// CodeBlock renders a snippet with line numbers and, once the grammar has
// loaded, syntax highlighting. Plain text shows immediately, so a slow or
// unknown grammar never blocks the content.
export function CodeBlock({
  code,
  language,
  startLine = 1,
  className,
}: CodeBlockProps) {
  const [html, setHtml] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    // The highlighter core loads on first use, keeping shiki out of the
    // initial bundle.
    import("@/lib/highlight")
      .then(({ highlight }) => highlight(code, language))
      .then((value) => {
        if (!cancelled) {
          setHtml(value)
        }
      })
      .catch(() => {
        // Highlighting is decoration; the plain fallback stays on failure.
      })

    return () => {
      cancelled = true
    }
  }, [code, language])

  const style = { counterReset: `line ${startLine - 1}` }
  const classes = cn(
    "code-block max-h-96 overflow-auto rounded bg-muted py-3 font-mono text-xs leading-relaxed",
    className
  )

  if (html) {
    // Shiki escapes the code it renders, so the markup is safe to inject.
    return (
      <div
        className={classes}
        style={style}
        dangerouslySetInnerHTML={{ __html: html }}
      />
    )
  }

  return (
    <div className={classes} style={style}>
      <pre>
        <code>
          {code.split("\n").map((line, index) => (
            <span key={index} className="line">
              {line}
              {"\n"}
            </span>
          ))}
        </code>
      </pre>
    </div>
  )
}
