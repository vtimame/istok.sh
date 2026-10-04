import { createBundledHighlighter, createSingletonShorthands } from "shiki/core"
import { createJavaScriptRegexEngine } from "shiki/engine/javascript"

// Fine-grained bundle: the UI is embedded into the istok binary, so only the
// languages the index labels are listed, and each grammar is a lazy chunk
// fetched the first time a snippet in that language is shown.
const languages = {
  go: () => import("@shikijs/langs/go"),
  typescript: () => import("@shikijs/langs/typescript"),
  tsx: () => import("@shikijs/langs/tsx"),
  javascript: () => import("@shikijs/langs/javascript"),
  jsx: () => import("@shikijs/langs/jsx"),
  python: () => import("@shikijs/langs/python"),
  java: () => import("@shikijs/langs/java"),
  kotlin: () => import("@shikijs/langs/kotlin"),
  c: () => import("@shikijs/langs/c"),
  cpp: () => import("@shikijs/langs/cpp"),
  rust: () => import("@shikijs/langs/rust"),
  markdown: () => import("@shikijs/langs/markdown"),
  yaml: () => import("@shikijs/langs/yaml"),
  json: () => import("@shikijs/langs/json"),
  toml: () => import("@shikijs/langs/toml"),
  html: () => import("@shikijs/langs/html"),
  css: () => import("@shikijs/langs/css"),
  sql: () => import("@shikijs/langs/sql"),
  shellscript: () => import("@shikijs/langs/shellscript"),
  svelte: () => import("@shikijs/langs/svelte"),
  make: () => import("@shikijs/langs/make"),
  docker: () => import("@shikijs/langs/docker"),
}

const themes = {
  "github-light": () => import("@shikijs/themes/github-light"),
  "github-dark": () => import("@shikijs/themes/github-dark"),
}

type Language = keyof typeof languages

const createHighlighter = createBundledHighlighter<
  Language,
  keyof typeof themes
>({
  langs: languages,
  themes,
  engine: () => createJavaScriptRegexEngine(),
})

const { codeToHtml } = createSingletonShorthands(createHighlighter)

// Labels produced by internal/indexing/discovery that differ from shiki ids.
const aliases: Record<string, Language> = {
  shell: "shellscript",
  makefile: "make",
  dockerfile: "docker",
}

function resolveLanguage(label?: string): Language | undefined {
  if (!label) {
    return undefined
  }
  if (label in aliases) {
    return aliases[label]
  }

  return label in languages ? (label as Language) : undefined
}

// highlight returns themed HTML, or null for languages without a grammar so
// callers fall back to plain text. Both themes are emitted as CSS variables;
// index.css picks one from the .dark class.
export async function highlight(
  code: string,
  label?: string
): Promise<string | null> {
  const lang = resolveLanguage(label)
  if (!lang) {
    return null
  }

  return codeToHtml(code, {
    lang,
    themes: { light: "github-light", dark: "github-dark" },
    defaultColor: false,
  })
}
