import { Type } from "@sinclair/typebox";
import type { AgentTool } from "@mariozechner/pi-agent-core";

import {
  DEFAULT_CACHE_TTL_MINUTES,
  DEFAULT_TIMEOUT_SECONDS,
  normalizeCacheKey,
  readCache,
  readResponseText,
  resolveCacheTtlMs,
  resolveTimeoutSeconds,
  withTimeout,
  writeCache,
} from "./cache.js";
import type { CacheEntry } from "./cache.js";
import { jsonResult, readNumberParam, readStringParam } from "./param-helpers.js";
import { credentialManager } from "../../credentials.js";

const SEARCH_PROVIDERS = ["brave", "perplexity"] as const;
type SearchProvider = (typeof SEARCH_PROVIDERS)[number];

const DEFAULT_SEARCH_COUNT = 5;
const MAX_SEARCH_COUNT = 10;

const BRAVE_SEARCH_ENDPOINT = "https://api.search.brave.com/res/v1/web/search";
const DEFAULT_PERPLEXITY_BASE_URL = "https://openrouter.ai/api/v1";
const PERPLEXITY_DIRECT_BASE_URL = "https://api.perplexity.ai";
const DEFAULT_PERPLEXITY_MODEL = "perplexity/sonar-pro";
const PERPLEXITY_KEY_PREFIXES = ["pplx-"];
const OPENROUTER_KEY_PREFIXES = ["sk-or-"];

const SEARCH_CACHE = new Map<string, CacheEntry<Record<string, unknown>>>();
const BRAVE_FRESHNESS_SHORTCUTS = new Set(["pd", "pw", "pm", "py"]);
const BRAVE_FRESHNESS_RANGE = /^(\d{4}-\d{2}-\d{2})to(\d{4}-\d{2}-\d{2})$/;

const WebSearchSchema = Type.Object({
  query: Type.String({ description: "Search query string." }),
  provider: Type.Optional(
    Type.String({
      description:
        'Search provider: "brave" (default, traditional search results) or "perplexity" (AI-synthesized answers).',
    }),
  ),
  count: Type.Optional(
    Type.Number({
      description: "Number of results to return (1-10). Default: 5. Brave only.",
      minimum: 1,
      maximum: MAX_SEARCH_COUNT,
    }),
  ),
  country: Type.Optional(
    Type.String({
      description:
        "2-letter country code for region-specific results (e.g., 'DE', 'US'). Default: 'US'.",
    }),
  ),
  freshness: Type.Optional(
    Type.String({
      description:
        "Filter results by time (Brave only): 'pd' (past day), 'pw' (past week), 'pm' (past month), 'py' (past year), or 'YYYY-MM-DDtoYYYY-MM-DD'.",
    }),
  ),
});

type WebSearchArgs = {
  query: string;
  provider?: string;
  count?: number;
  country?: string;
  freshness?: string;
};

type BraveSearchResult = {
  title?: string;
  url?: string;
  description?: string;
  age?: string;
};

type BraveSearchResponse = {
  web?: {
    results?: BraveSearchResult[];
  };
};

type PerplexitySearchResponse = {
  choices?: Array<{
    message?: {
      content?: string;
    };
  }>;
  citations?: string[];
};

export type WebSearchResult = {
  query: string;
  provider: SearchProvider;
  tookMs: number;
  cached?: boolean;
} & (
  | {
      // Brave result
      count: number;
      results: Array<{
        title: string;
        url: string;
        description: string;
        published?: string;
        siteName?: string;
      }>;
    }
  | {
      // Perplexity result
      model: string;
      content: string;
      citations: string[];
    }
);

function resolveSearchCount(value: unknown, fallback: number): number {
  const parsed = typeof value === "number" && Number.isFinite(value) ? value : fallback;
  const clamped = Math.max(1, Math.min(MAX_SEARCH_COUNT, Math.floor(parsed)));
  return clamped;
}

function normalizeFreshness(value: string | undefined): string | undefined {
  if (!value) return undefined;
  const trimmed = value.trim();
  if (!trimmed) return undefined;

  const lower = trimmed.toLowerCase();
  if (BRAVE_FRESHNESS_SHORTCUTS.has(lower)) return lower;

  const match = trimmed.match(BRAVE_FRESHNESS_RANGE);
  if (!match) return undefined;

  const start = match[1];
  const end = match[2];
  if (!start || !end) return undefined;
  if (!isValidIsoDate(start) || !isValidIsoDate(end)) return undefined;
  if (start > end) return undefined;

  return `${start}to${end}`;
}

function isValidIsoDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const parts = value.split("-").map((part) => Number.parseInt(part, 10));
  const year = parts[0];
  const month = parts[1];
  const day = parts[2];
  if (year === undefined || month === undefined || day === undefined) return false;
  if (!Number.isFinite(year) || !Number.isFinite(month) || !Number.isFinite(day)) return false;

  const date = new Date(Date.UTC(year, month - 1, day));
  return (
    date.getUTCFullYear() === year && date.getUTCMonth() === month - 1 && date.getUTCDate() === day
  );
}

function resolveSiteName(url: string | undefined): string | undefined {
  if (!url) return undefined;
  try {
    return new URL(url).hostname;
  } catch {
    return undefined;
  }
}

function inferPerplexityBaseUrl(apiKey: string): string {
  const normalized = apiKey.toLowerCase();
  if (PERPLEXITY_KEY_PREFIXES.some((prefix) => normalized.startsWith(prefix))) {
    return PERPLEXITY_DIRECT_BASE_URL;
  }
  if (OPENROUTER_KEY_PREFIXES.some((prefix) => normalized.startsWith(prefix))) {
    return DEFAULT_PERPLEXITY_BASE_URL;
  }
  return DEFAULT_PERPLEXITY_BASE_URL;
}

function resolvePerplexityApiKey(): { apiKey: string; source: string } | { apiKey: null; source: "none" } {
  const perplexityKey = (credentialManager.getToolConfig("perplexity")?.apiKey ?? "").trim();
  if (perplexityKey) {
    return { apiKey: perplexityKey, source: "perplexity" };
  }

  const openrouterKey = (credentialManager.getToolConfig("openrouter")?.apiKey ?? "").trim();
  if (openrouterKey) {
    return { apiKey: openrouterKey, source: "openrouter" };
  }

  return { apiKey: null, source: "none" };
}

function resolveBraveApiKey(): string | undefined {
  return (credentialManager.getToolConfig("brave")?.apiKey ?? "").trim() || undefined;
}

function resolveProvider(requested?: string): SearchProvider {
  if (requested === "perplexity") return "perplexity";
  if (requested === "brave") return "brave";

  // Auto-detect based on available API keys
  const braveKey = resolveBraveApiKey();
  if (braveKey) return "brave";

  const perplexityResult = resolvePerplexityApiKey();
  if (perplexityResult.apiKey) return "perplexity";

  // Default to brave
  return "brave";
}

async function runPerplexitySearch(params: {
  query: string;
  apiKey: string;
  baseUrl: string;
  model: string;
  timeoutSeconds: number;
}): Promise<{ content: string; citations: string[] }> {
  const endpoint = `${params.baseUrl.replace(/\/$/, "")}/chat/completions`;

  const res = await fetch(endpoint, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${params.apiKey}`,
      "HTTP-Referer": "https://multica.ai",
      "X-Title": "Multica Web Search",
    },
    body: JSON.stringify({
      model: params.model,
      messages: [
        {
          role: "user",
          content: params.query,
        },
      ],
    }),
    signal: withTimeout(undefined, params.timeoutSeconds * 1000),
  });

  if (!res.ok) {
    const detail = await readResponseText(res);
    throw new Error(`Perplexity API error (${res.status}): ${detail || res.statusText}`);
  }

  const data = (await res.json()) as PerplexitySearchResponse;
  const content = data.choices?.[0]?.message?.content ?? "No response";
  const citations = data.citations ?? [];

  return { content, citations };
}

async function runBraveSearch(params: {
  query: string;
  count: number;
  apiKey: string;
  timeoutSeconds: number;
  country: string | undefined;
  freshness: string | undefined;
}): Promise<{
  results: Array<{
    title: string;
    url: string;
    description: string;
    published?: string;
    siteName?: string;
  }>;
}> {
  const url = new URL(BRAVE_SEARCH_ENDPOINT);
  url.searchParams.set("q", params.query);
  url.searchParams.set("count", String(params.count));
  if (params.country) {
    url.searchParams.set("country", params.country);
  }
  if (params.freshness) {
    url.searchParams.set("freshness", params.freshness);
  }

  const res = await fetch(url.toString(), {
    method: "GET",
    headers: {
      Accept: "application/json",
      "X-Subscription-Token": params.apiKey,
    },
    signal: withTimeout(undefined, params.timeoutSeconds * 1000),
  });

  if (!res.ok) {
    const detail = await readResponseText(res);
    throw new Error(`Brave Search API error (${res.status}): ${detail || res.statusText}`);
  }

  const data = (await res.json()) as BraveSearchResponse;
  const rawResults = Array.isArray(data.web?.results) ? (data.web?.results ?? []) : [];
  const results = rawResults.map((entry) => {
    const result: {
      title: string;
      url: string;
      description: string;
      published?: string;
      siteName?: string;
    } = {
      title: entry.title ?? "",
      url: entry.url ?? "",
      description: entry.description ?? "",
    };
    if (entry.age) {
      result.published = entry.age;
    }
    const siteName = resolveSiteName(entry.url);
    if (siteName) {
      result.siteName = siteName;
    }
    return result;
  });
  return { results };
}

async function runWebSearch(params: {
  query: string;
  provider: SearchProvider;
  count: number;
  timeoutSeconds: number;
  cacheTtlMs: number;
  country: string | undefined;
  freshness: string | undefined;
}): Promise<Record<string, unknown>> {
  const cacheKey = normalizeCacheKey(
    `${params.provider}:${params.query}:${params.count}:${params.country || "default"}:${params.freshness || "default"}`,
  );
  const cached = readCache(SEARCH_CACHE, cacheKey);
  if (cached) return { ...cached.value, cached: true };

  const start = Date.now();

  if (params.provider === "perplexity") {
    const perplexityResult = resolvePerplexityApiKey();
    if (!perplexityResult.apiKey) {
      return {
        error: "missing_api_key",
        message:
          "Perplexity search requires tools.perplexity.apiKey (or tools.openrouter.apiKey) in credentials.json5.",
      };
    }

    const apiKey = perplexityResult.apiKey;
    const perplexityConfig = credentialManager.getToolConfig("perplexity");
    const baseUrl = (perplexityConfig?.baseUrl ?? "").trim() || inferPerplexityBaseUrl(apiKey);
    const model = (perplexityConfig?.model ?? "").trim() || DEFAULT_PERPLEXITY_MODEL;
    const { content, citations } = await runPerplexitySearch({
      query: params.query,
      apiKey,
      baseUrl,
      model,
      timeoutSeconds: params.timeoutSeconds,
    });

    const payload = {
      query: params.query,
      provider: params.provider,
      model,
      tookMs: Date.now() - start,
      content,
      citations,
    };
    writeCache(SEARCH_CACHE, cacheKey, payload, params.cacheTtlMs);
    return payload;
  }

  // Brave search
  const apiKey = resolveBraveApiKey();
  if (!apiKey) {
    return {
      error: "missing_api_key",
      message: "Brave search requires tools.brave.apiKey in credentials.json5.",
    };
  }

  const { results } = await runBraveSearch({
    query: params.query,
    count: params.count,
    apiKey,
    timeoutSeconds: params.timeoutSeconds,
    country: params.country,
    freshness: params.freshness,
  });

  const payload = {
    query: params.query,
    provider: params.provider,
    count: results.length,
    tookMs: Date.now() - start,
    results,
  };
  writeCache(SEARCH_CACHE, cacheKey, payload, params.cacheTtlMs);
  return payload;
}

export function createWebSearchTool(): AgentTool<typeof WebSearchSchema, unknown> {
  return {
    name: "web_search",
    label: "Web Search",
    description:
      'Search the web. Supports "brave" (traditional results with titles/URLs/snippets) and "perplexity" (AI-synthesized answers with citations). Provider auto-detected from available API keys if not specified.',
    parameters: WebSearchSchema,
    execute: async (_toolCallId, args) => {
      const params = args as WebSearchArgs;
      const query = readStringParam(params as Record<string, unknown>, "query", { required: true });
      const providerRaw = readStringParam(params as Record<string, unknown>, "provider");
      const provider = resolveProvider(providerRaw);
      const count =
        readNumberParam(params as Record<string, unknown>, "count", { integer: true }) ??
        DEFAULT_SEARCH_COUNT;
      const country = readStringParam(params as Record<string, unknown>, "country");
      const rawFreshness = readStringParam(params as Record<string, unknown>, "freshness");

      if (rawFreshness && provider !== "brave") {
        return jsonResult({
          error: "unsupported_parameter",
          message: "freshness parameter is only supported by the Brave search provider.",
        });
      }

      const freshness = rawFreshness ? normalizeFreshness(rawFreshness) : undefined;
      if (rawFreshness && !freshness) {
        return jsonResult({
          error: "invalid_freshness",
          message:
            "freshness must be one of: pd (past day), pw (past week), pm (past month), py (past year), or YYYY-MM-DDtoYYYY-MM-DD.",
        });
      }

      try {
        const result = await runWebSearch({
          query,
          provider,
          count: resolveSearchCount(count, DEFAULT_SEARCH_COUNT),
          timeoutSeconds: resolveTimeoutSeconds(DEFAULT_TIMEOUT_SECONDS, DEFAULT_TIMEOUT_SECONDS),
          cacheTtlMs: resolveCacheTtlMs(DEFAULT_CACHE_TTL_MINUTES, DEFAULT_CACHE_TTL_MINUTES),
          country,
          freshness,
        });
        return jsonResult(result);
      } catch (error) {
        return jsonResult({
          error: "search_failed",
          message: error instanceof Error ? error.message : String(error),
        });
      }
    },
  };
}
