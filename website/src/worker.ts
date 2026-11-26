import { THEME_SWATCH_MAP, type PluginEntry } from './data/catalog'

interface GitHubRepoItem {
  full_name: string
  name: string
  description: string | null
  topics?: string[]
  stargazers_count: number
  updated_at: string
  pushed_at?: string
  archived?: boolean
  disabled?: boolean
  html_url: string
  owner?: {
    login?: string
    avatar_url?: string
  }
  license?: {
    spdx_id?: string
  } | null
}

interface GitHubSearchPayload {
  total_count: number
  items: GitHubRepoItem[]
}

const GITHUB_SEARCH_URL =
  'https://api.github.com/search/repositories?q=topic:mikutv-plugin&sort=updated&order=desc&per_page=100'

function inferKind(topics: string[]): 'source' | 'theme' | 'locale' {
  if (topics.includes('mikutv-theme')) return 'theme'
  if (topics.includes('mikutv-locale')) return 'locale'
  return 'source'
}

function prettifyName(item: GitHubRepoItem): string {
  const key = item.full_name.toLowerCase()
  const knownTheme = THEME_SWATCH_MAP[key]
  if (knownTheme) return knownTheme.displayName
  if (key === 'yuchanns/mikutv-plugin-jellyfin') return 'Jellyfin'
  if (key === 'yuchanns/mikutv-locale-ja') return 'Japanese (日本語)'
  return item.name
    .replace(/^mikutv-(plugin|theme|locale)-/i, '')
    .split(/[-_]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}

function mapGitHubRepo(item: GitHubRepoItem): PluginEntry {
  const topics = Array.isArray(item.topics) ? item.topics : []
  const kind = inferKind(topics)
  const owner = item.owner?.login || item.full_name.split('/')[0] || ''
  const themeMeta = THEME_SWATCH_MAP[item.full_name.toLowerCase()]
  return {
    fullName: item.full_name,
    name: item.name,
    displayName: prettifyName(item),
    owner,
    avatarUrl: item.owner?.avatar_url || '',
    description: item.description?.trim() || '',
    kind,
    topics,
    stars: item.stargazers_count || 0,
    updatedAt: item.pushed_at || item.updated_at || '',
    htmlUrl: item.html_url,
    releasesUrl: `${item.html_url}/releases`,
    license: item.license?.spdx_id && item.license.spdx_id !== 'NOASSERTION' ? item.license.spdx_id : '',
    official: owner.toLowerCase() === 'yuchanns',
    ...(themeMeta ? { swatches: themeMeta.swatches, appearance: themeMeta.appearance } : {}),
  }
}

function jsonResponse(body: unknown, headers: Record<string, string> = {}, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      'content-type': 'application/json; charset=utf-8',
      'access-control-allow-origin': '*',
      'access-control-allow-methods': 'GET, OPTIONS',
      ...headers,
    },
  })
}

async function handlePluginsApi(
  request: Request,
  env: Env,
  ctx: ExecutionContext,
): Promise<Response> {
  if (request.method === 'OPTIONS') {
    return new Response(null, {
      status: 204,
      headers: {
        'access-control-allow-origin': '*',
        'access-control-allow-methods': 'GET, OPTIONS',
      },
    })
  }
  if (request.method !== 'GET') {
    return jsonResponse({ error: 'Method not allowed' }, {}, 405)
  }

  const url = new URL(request.url)
  const forceRefresh = url.searchParams.get('refresh') === '1'
  const cacheKey = new Request(`${url.origin}/api/plugins`, { method: 'GET' })
  const edgeCache = (caches as CacheStorage & { default: Cache }).default

  if (!forceRefresh) {
    const cached = await edgeCache.match(cacheKey)
    if (cached) {
      const hit = new Response(cached.body, cached)
      hit.headers.set('x-mikutv-cache', 'HIT')
      return hit
    }
  }

  const ghHeaders: Record<string, string> = {
    accept: 'application/vnd.github+json',
    'user-agent': 'MikuTV-Website/1.0 (+https://mikutv.yuchanns.dev)',
    'x-github-api-version': '2022-11-28',
  }
  const maybeToken = (env as Env & { GITHUB_TOKEN?: string }).GITHUB_TOKEN
  if (maybeToken) {
    ghHeaders.authorization = `Bearer ${maybeToken}`
  }

  try {
    const upstream = await fetch(GITHUB_SEARCH_URL, { headers: ghHeaders })
    if (!upstream.ok) {
      throw new Error(`GitHub Search API returned HTTP ${upstream.status}`)
    }
    const payload = (await upstream.json()) as GitHubSearchPayload
    const items = (payload.items || [])
      .filter((repo) => !repo.archived && !repo.disabled)
      .map(mapGitHubRepo)

    const response = jsonResponse(
      {
        source: 'github-live',
        topic: 'mikutv-plugin',
        fetchedAt: new Date().toISOString(),
        total: items.length,
        items,
      },
      {
        'cache-control': 'public, max-age=120, s-maxage=300, stale-while-revalidate=900',
        'x-mikutv-cache': 'MISS',
      },
    )
    ctx.waitUntil(edgeCache.put(cacheKey, response.clone()))
    return response
  } catch (err) {
    console.error(
      JSON.stringify({
        event: 'plugins_api_error',
        message: err instanceof Error ? err.message : String(err),
      }),
    )
    return jsonResponse(
      {
        error: 'GitHub Search API unavailable',
        topic: 'mikutv-plugin',
      },
      {
        'cache-control': 'no-store',
        'x-mikutv-cache': 'ERROR',
      },
      502,
    )
  }
}

export default {
  async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
    const { pathname } = new URL(request.url)
    if (pathname === '/api/plugins') {
      return handlePluginsApi(request, env, ctx)
    }
    return env.ASSETS.fetch(request)
  },
} satisfies ExportedHandler<Env>
