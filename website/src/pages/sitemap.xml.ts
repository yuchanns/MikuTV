import type { APIRoute } from 'astro'

const ROUTES = ['/', '/plugins/', '/develop/', '/zh/', '/zh/plugins/', '/zh/develop/']

export const GET: APIRoute = () => {
  const urls = ROUTES.map(
    (path) =>
      `  <url>\n    <loc>https://mikutv.yuchanns.dev${path}</loc>\n    <changefreq>weekly</changefreq>\n  </url>`,
  ).join('\n')

  const xml = `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${urls}\n</urlset>\n`

  return new Response(xml, {
    headers: { 'content-type': 'application/xml; charset=utf-8' },
  })
}
