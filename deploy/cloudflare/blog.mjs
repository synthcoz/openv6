// Generates the blog and the SEO files from blog/posts/*.html (repo root):
//   site/blog/index.html, site/blog/<slug>/index.html, site/blog/feed.xml,
//   site/sitemap.xml, site/llms.txt, site/llms-full.txt
// and returns the "latest posts" fragment that build.mjs puts on the landing.
//
// A post is an HTML body preceded by a JSON header in an HTML comment:
//   <!-- { "title", "description", "date", "updated"?, "keywords", "tldr", "faq": [[q, a], …] } -->
import { readdirSync, readFileSync, writeFileSync, mkdirSync, rmSync, existsSync } from "node:fs";
import { join, dirname } from "node:path";
import { execSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "../..");
const site = join(root, "site");
const SITE = "https://thephuc.xyz/openv6/";
const ORG = { "@type": "Organization", name: "openv6", url: SITE, logo: SITE + "assets/logo-256.png" };

const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
const text = (html) => html.replace(/<[^>]+>/g, " ").replace(/&nbsp;/g, " ").replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&amp;/g, "&").replace(/\s+/g, " ").trim();
const fmtDate = (d) => new Date(d + "T00:00:00Z").toLocaleDateString("en-US", { year: "numeric", month: "long", day: "numeric", timeZone: "UTC" });
const jsonld = (o) => `<script type="application/ld+json">${JSON.stringify(o).replace(/</g, "\\u003c")}</script>`;

export function loadPosts() {
  const dir = join(root, "blog/posts");
  return readdirSync(dir).filter((f) => f.endsWith(".html")).map((f) => {
    const raw = readFileSync(join(dir, f), "utf8");
    const m = raw.match(/^<!--\s*([\s\S]*?)\s*-->\s*/);
    if (!m) throw new Error(f + ": missing JSON header");
    const meta = JSON.parse(m[1]);
    const body = raw.slice(m[0].length).trim();
    const slug = f.replace(/\.html$/, "");
    const words = text(body).split(" ").length;
    const toc = [...body.matchAll(/<h2 id="([^"]+)">([\s\S]*?)<\/h2>/g)].map((x) => ({ id: x[1], title: text(x[2]) }));
    return { ...meta, slug, body, words, minutes: Math.max(1, Math.round(words / 220)), toc, updated: meta.updated || meta.date };
  }).sort((a, b) => b.date.localeCompare(a.date) || (a.order ?? 99) - (b.order ?? 99) || a.title.localeCompare(b.title));
}

const THEME_SCRIPT = `<script>try { const t = localStorage.getItem('ov6_theme'); if (t === 'light' || t === 'dark') document.documentElement.dataset.theme = t; } catch (e) { /* storage blocked */ }</script>`;
const FONTS = `<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@500&display=swap" rel="stylesheet">`;
const MOON = `<svg class="moon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/></svg><svg class="sun" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>`;

function page({ up, title, description, canonical, image, type = "website", extraHead = "", main, ld }) {
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${esc(title)}</title>
<meta name="description" content="${esc(description)}">
<link rel="canonical" href="${canonical}">
<meta name="robots" content="index, follow, max-image-preview:large">
<meta name="theme-color" content="#ffffff">
<meta property="og:site_name" content="openv6">
<meta property="og:type" content="${type}">
<meta property="og:title" content="${esc(title)}">
<meta property="og:description" content="${esc(description)}">
<meta property="og:url" content="${canonical}">
<meta property="og:image" content="${image}">
<meta property="og:image:width" content="1200">
<meta property="og:image:height" content="630">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="${esc(title)}">
<meta name="twitter:description" content="${esc(description)}">
<meta name="twitter:image" content="${image}">
${extraHead}<link rel="alternate" type="application/rss+xml" title="openv6 blog" href="${SITE}blog/feed.xml">
<link rel="icon" href="${up}assets/logo.svg" type="image/svg+xml">
<link rel="apple-touch-icon" href="${up}assets/logo-256.png">
${FONTS}
${THEME_SCRIPT}
<link rel="stylesheet" href="${up}assets/site.css">
${ld.map(jsonld).join("\n")}
</head>
<body>
<header id="hdr"><div class="wrap"><div class="nav">
  <a class="brand" href="${up}"><img src="${up}assets/logo.svg" alt="" width="30" height="30">openv6</a>
  <nav class="links"><a href="${up}#how">How it works</a><a href="${up}#pricing">Pricing</a><a href="${up}blog/">Blog</a><a href="${up}#faq">FAQ</a></nav>
  <div class="sp"></div>
  <button class="icon-btn" id="theme" type="button" title="Switch light / dark" aria-label="Switch light / dark">${MOON}</button>
  <a class="btn sm primary dl" href="${up}#download">Download</a>
</div></div></header>
${main}
<footer><div class="wrap">
  <div class="fl"><a class="brand" href="${up}" style="font-size:15px"><img src="${up}assets/logo.svg" alt="" width="22" height="22" style="width:22px;height:22px">openv6</a>
  <span>© ${new Date().getUTCFullYear()} openv6</span></div>
  <nav class="fr"><a href="${up}blog/">Blog</a><a href="https://labs.thephuc.xyz/">Labs</a><a href="https://github.com/thephux/openv6-releases">GitHub</a><a href="${up}#download">Downloads</a></nav>
</div></footer>
<script>
document.getElementById('theme').onclick = () => {
  const t = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
  document.documentElement.dataset.theme = t; try { localStorage.setItem('ov6_theme', t); } catch (e) { /* ignore */ }
};
const hdr = document.getElementById('hdr');
addEventListener('scroll', () => hdr.classList.toggle('scrolled', scrollY > 8), { passive: true });
</script>
</body>
</html>
`;
}

function card(p, up) {
  return `<a class="pcard" href="${up}blog/${p.slug}/"><h3>${esc(p.title)}</h3><p>${esc(p.description)}</p><span class="pmeta">${fmtDate(p.date)} · ${p.minutes} min read</span></a>`;
}

function postPage(p, all) {
  const url = `${SITE}blog/${p.slug}/`;
  const image = `${SITE}assets/og/${p.slug}.jpg`;
  const body = p.body.replace(/<table>/g, '<div class="tbl"><table>').replace(/<\/table>/g, "</table></div>");
  const related = all.filter((x) => x.slug !== p.slug && body.includes(`../${x.slug}/`)).concat(all.filter((x) => x.slug !== p.slug && !body.includes(`../${x.slug}/`))).slice(0, 3);
  const faq = p.faq || [];
  const ld = [
    { "@context": "https://schema.org", "@type": "BlogPosting", headline: p.title, description: p.description, image, datePublished: p.date, dateModified: p.updated,
      author: ORG, publisher: ORG, mainEntityOfPage: url, keywords: (p.keywords || []).join(", "), wordCount: p.words, inLanguage: "en",
      about: { "@type": "SoftwareApplication", name: "openv6", url: SITE } },
    { "@context": "https://schema.org", "@type": "BreadcrumbList", itemListElement: [
      { "@type": "ListItem", position: 1, name: "openv6", item: SITE },
      { "@type": "ListItem", position: 2, name: "Blog", item: SITE + "blog/" },
      { "@type": "ListItem", position: 3, name: p.title, item: url }] },
  ];
  if (faq.length) ld.push({ "@context": "https://schema.org", "@type": "FAQPage", mainEntity: faq.map(([q, a]) => ({ "@type": "Question", name: q, acceptedAnswer: { "@type": "Answer", text: a } })) });
  const main = `<main class="wrap post">
  <nav class="crumbs" aria-label="Breadcrumb"><a href="../../">openv6</a><span>›</span><a href="../">Blog</a></nav>
  <article>
    <h1>${esc(p.title)}</h1>
    <p class="pmeta"><time datetime="${p.date}">${fmtDate(p.date)}</time>${p.updated !== p.date ? ` · Updated <time datetime="${p.updated}">${fmtDate(p.updated)}</time>` : ""} · ${p.minutes} min read</p>
    <div class="tldr"><b>In short</b><p>${esc(p.tldr)}</p></div>
    ${p.toc.length > 2 ? `<nav class="toc" aria-label="Contents"><b>Contents</b><ol>${p.toc.map((t) => `<li><a href="#${t.id}">${esc(t.title)}</a></li>`).join("")}</ol></nav>` : ""}
    <div class="prose">
${body}
    </div>
    ${faq.length ? `<section class="faqs" id="faq"><h2>Frequently asked questions</h2>${faq.map(([q, a]) => `<details><summary>${esc(q)}</summary><p>${esc(a)}</p></details>`).join("")}</section>` : ""}
  </article>
  <aside class="cta-card">
    <div><h2>Run your own static IPv6 proxies</h2><p>openv6 turns your home ISP or VPS IPv6 into fixed HTTP + SOCKS5 proxies for anti-detect browsers. Windows, macOS and Linux. Free for 3 proxies.</p></div>
    <div class="cta"><a class="btn primary" href="../../#download">Download free</a><a class="btn" href="../../#pricing">See Pro</a></div>
  </aside>
  <section class="related"><h2>Keep reading</h2><div class="pgrid">${related.map((r) => card(r, "../../")).join("")}</div></section>
</main>`;
  return page({ up: "../../", title: `${p.title} | openv6`, description: p.description, canonical: url, image, type: "article",
    extraHead: `<meta property="article:published_time" content="${p.date}">\n<meta property="article:modified_time" content="${p.updated}">\n<meta name="keywords" content="${esc((p.keywords || []).join(", "))}">\n`, main, ld });
}

function indexPage(posts) {
  const url = SITE + "blog/";
  const ld = [
    { "@context": "https://schema.org", "@type": "Blog", name: "openv6 blog", url, description: "Guides on IPv6 proxies, anti-detect browsers and running your own static proxies.", publisher: ORG,
      blogPost: posts.map((p) => ({ "@type": "BlogPosting", headline: p.title, url: `${url}${p.slug}/`, datePublished: p.date })) },
    { "@context": "https://schema.org", "@type": "BreadcrumbList", itemListElement: [
      { "@type": "ListItem", position: 1, name: "openv6", item: SITE }, { "@type": "ListItem", position: 2, name: "Blog", item: url }] },
  ];
  const main = `<main class="wrap post blogidx">
  <nav class="crumbs" aria-label="Breadcrumb"><a href="../">openv6</a><span>›</span><span>Blog</span></nav>
  <h1>IPv6 proxies, explained</h1>
  <p class="lead">Guides on IPv6 proxies, anti-detect browsers and running your own static proxies at home or on a VPS.</p>
  <div class="pgrid">${posts.map((p) => card(p, "../")).join("")}</div>
</main>`;
  return page({ up: "../", title: "openv6 Blog: IPv6 Proxy Guides", description: "Guides on IPv6 proxies, anti-detect browsers and running your own static HTTP/SOCKS5 proxies at home or on a VPS.", canonical: url, image: SITE + "assets/og.jpg", main, ld });
}

function feed(posts) {
  return `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">
<channel>
<title>openv6 blog</title>
<link>${SITE}blog/</link>
<atom:link href="${SITE}blog/feed.xml" rel="self" type="application/rss+xml"/>
<description>Guides on IPv6 proxies, anti-detect browsers and self-hosted static proxies.</description>
<language>en</language>
${posts.map((p) => `<item><title>${esc(p.title)}</title><link>${SITE}blog/${p.slug}/</link><guid>${SITE}blog/${p.slug}/</guid><pubDate>${new Date(p.date + "T00:00:00Z").toUTCString()}</pubDate><description>${esc(p.description)}</description></item>`).join("\n")}
</channel>
</rss>
`;
}

function sitemap(posts, today) {
  const u = (loc, lastmod, pr) => `<url><loc>${loc}</loc><lastmod>${lastmod}</lastmod><priority>${pr}</priority></url>`;
  return `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${[u(SITE, today, "1.0"), u(SITE + "blog/", posts[0]?.updated || today, "0.8"), ...posts.map((p) => u(`${SITE}blog/${p.slug}/`, p.updated, "0.7"))].join("\n")}
</urlset>
`;
}

// HTML body → Markdown-ish plain text for llms-full.txt
function md(html) {
  return html
    .replace(/<pre><code>([\s\S]*?)<\/code><\/pre>/g, (_, c) => "\n```\n" + c.replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&amp;/g, "&") + "\n```\n")
    .replace(/<h2[^>]*>([\s\S]*?)<\/h2>/g, (_, t) => `\n## ${text(t)}\n`)
    .replace(/<h3[^>]*>([\s\S]*?)<\/h3>/g, (_, t) => `\n### ${text(t)}\n`)
    .replace(/<tr>([\s\S]*?)<\/tr>/g, (_, r) => "| " + [...r.matchAll(/<t[hd]>([\s\S]*?)<\/t[hd]>/g)].map((c) => text(c[1])).join(" | ") + " |\n")
    .replace(/<li>([\s\S]*?)<\/li>/g, (_, t) => `- ${text(t)}\n`)
    .replace(/<p>([\s\S]*?)<\/p>/g, (_, t) => `\n${text(t)}\n`)
    .replace(/<\/?(table|thead|tbody|ul|ol|div)[^>]*>/g, "")
    .replace(/\n{3,}/g, "\n\n").trim();
}

const FACTS = `openv6 turns the IPv6 prefix that a home internet connection or a VPS already has into static HTTP + SOCKS5 proxies, one fixed IPv6 address per proxy, for anti-detect browsers such as Multilogin, GoLogin, Dolphin Anty, ixBrowser, Linken Sphere, AdsPower and MoreLogin.

- Each proxy has its own port, username, password and outgoing IPv6, kept until the user changes it. The same port speaks HTTP and SOCKS5.
- Works with home ISPs (a /64 on the LAN; when the ISP changes the prefix, openv6 follows and keeps ports and passwords) and VPS or homelab servers with a /64 or a routed /56 or /48 (optionally one /64 per proxy).
- Clients connect over IPv4 (the machine's LAN IP or the VPS public IP); traffic leaves over IPv6. IPv4 fallback is optional and off by default for account work.
- Runs on Windows 10/11, macOS (Intel and Apple silicon) and Linux (Ubuntu, Debian, Zorin; amd64, arm64, armhf) as a background service with a tray icon. Several machines (nodes) are managed from one web dashboard.
- Regular IP checks (latency, country, ISP), plain-language error causes with one-click fixes, Telegram alerts, works with a VPN on, sharing outside the LAN through a relay on a VPS, UPnP or Tailscale, JSON API, customer accounts for reselling.
- Exports proxy lists in the formats of Multilogin, GoLogin, Dolphin Anty, ixBrowser, Linken Sphere 2 and AdsPower, plus host:port:user:pass, URLs, CSV and JSON.
- Pricing: Free = 3 proxies on 1 machine. Pro = one-time payment, $6 for 1 dashboard machine or $12 for 3, unlimited proxies and nodes, no subscription. Licenses are sold through Polar.
- Download: ${SITE}#download (Windows .exe, macOS .pkg, Linux install command). Releases: https://github.com/thephux/openv6-releases/releases/latest`;

function llms(posts) {
  return `# openv6

> Self-hosted static IPv6 proxies (HTTP + SOCKS5) from your own home internet or VPS, one fixed IPv6 per account, for anti-detect browsers. Windows, macOS, Linux. Free for 3 proxies; Pro is a one-time $6 / $12.

${FACTS}

## Pages

- [openv6 home](${SITE}): features, how it works, pricing, FAQ
- [Download](${SITE}#download): installers for Windows, macOS and Linux
- [Pricing](${SITE}#pricing): Free and Pro plans

## Guides

${posts.map((p) => `- [${p.title}](${SITE}blog/${p.slug}/): ${p.description}`).join("\n")}

## Optional

- [Full text of all guides](${SITE}llms-full.txt)
- [Release notes and downloads](https://github.com/thephux/openv6-releases/releases)
`;
}

function llmsFull(posts) {
  return `# openv6: full reference

${FACTS}

${posts.map((p) => `---

# ${p.title}

URL: ${SITE}blog/${p.slug}/
Published: ${p.date}

In short: ${p.tldr}

${md(p.body)}
${(p.faq || []).length ? "\n## FAQ\n\n" + p.faq.map(([q, a]) => `**${q}**\n${a}\n`).join("\n") : ""}`).join("\n")}
`;
}

export function generate() {
  const posts = loadPosts();
  // landing lastmod = last commit that touched it (fresh clones have no useful mtimes)
  let today = new Date().toISOString().slice(0, 10);
  try { today = execSync("git log -1 --format=%cs -- site/index.html site/assets/site.css", { cwd: root }).toString().trim() || today; } catch { /* not a git checkout */ }
  const out = join(site, "blog");
  if (existsSync(out)) rmSync(out, { recursive: true });
  mkdirSync(out, { recursive: true });
  writeFileSync(join(out, "index.html"), indexPage(posts));
  for (const p of posts) {
    mkdirSync(join(out, p.slug), { recursive: true });
    writeFileSync(join(out, p.slug, "index.html"), postPage(p, posts));
  }
  writeFileSync(join(out, "feed.xml"), feed(posts));
  writeFileSync(join(site, "sitemap.xml"), sitemap(posts, today));
  writeFileSync(join(site, "llms.txt"), llms(posts));
  writeFileSync(join(site, "llms-full.txt"), llmsFull(posts));
  const latest = `<div class="pgrid">${posts.slice(0, 3).map((p) => card(p, "")).join("")}</div>`;
  return { posts, latest };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const { posts } = generate();
  console.log(`blog: ${posts.length} posts`);
}
