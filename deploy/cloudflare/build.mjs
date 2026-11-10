// Bundles site/ into one Worker script (dist/worker.js) served under /openv6/.
// A plain script (no static-assets upload) keeps deploys simple and fast.
import { readdirSync, readFileSync, statSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { join, relative, extname, dirname } from "node:path";
import { createHash } from "node:crypto";
import { fileURLToPath } from "node:url";
import { generate } from "./blog.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const site = join(here, "../../site");
const out = join(here, "dist");
// blog pages, sitemap, feed and llms.txt are generated from blog/posts first
const blog = generate();
const types = {
  ".html": "text/html; charset=utf-8", ".svg": "image/svg+xml", ".png": "image/png",
  ".webp": "image/webp", ".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8",
  ".ico": "image/x-icon", ".txt": "text/plain; charset=utf-8", ".json": "application/json",
  ".xml": "application/xml; charset=utf-8", ".jpg": "image/jpeg",
};
const files = {};
(function walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) { walk(p); continue; }
    const type = types[extname(name).toLowerCase()];
    if (!type) continue;
    const body = readFileSync(p);
    files["/" + relative(site, p).split("\\").join("/")] = {
      type, b64: body.toString("base64"),
      etag: '"' + createHash("sha256").update(body).digest("hex").slice(0, 16) + '"',
    };
  }
})(site);
if (!files["/index.html"]) throw new Error("site/index.html missing");

// Pre-render: pages fill their text from a T = { vi, en } table in JavaScript.
// Crawlers and AI assistants often do not run JavaScript, so the English text,
// links and prices are written into the HTML here; the page script still
// switches language in the browser.
const SITE = "https://thephuc.xyz/openv6/";
const escHtml = (s) => String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
const grab = (html, name) => { const m = html.match(new RegExp("\\nconst " + name + " = (\\{[\\s\\S]*?\\n?\\});")); return m ? Function("return " + m[1])() : null; };
function prerender(html, key) {
  const T = grab(html, "T"); if (!T) return html;
  const en = T.en, LINKS = grab(html, "LINKS") || {}, PRICES = grab(html, "PRICES") || {};
  html = html.replace(/<(\w+)([^>]*?\sdata-i="(\w+)"[^>]*)><\/\1>/g, (m, tag, attrs, k) => typeof en[k] === "string" ? `<${tag}${attrs}>${escHtml(en[k])}</${tag}>` : m);
  html = html.replace(/<(\w+)([^>]*?\sdata-h="(\w+)"[^>]*)><\/\1>/g, (m, tag, attrs, k) => typeof en[k] === "string" ? `<${tag}${attrs}>${en[k]}</${tag}>` : m);
  html = html.replace(/<(\w+)([^>]*?\sdata-price="(\w+)"[^>]*)><\/\1>/g, (m, tag, attrs, k) => PRICES[k] ? `<${tag}${attrs}>${escHtml(PRICES[k])}</${tag}>` : m);
  html = html.replace(/<a((?:(?!\shref=)[^>])*?)\sdata-href="(\w+)"((?:(?!\shref=)[^>])*)>/g, (m, a, k, b) => LINKS[k] ? `<a${a} data-href="${k}" href="${LINKS[k]}"${b}>` : m);
  html = html.replace(/<a((?:(?!\shref=)[^>])*?)\sdata-dl="(\w+)"((?:(?!\shref=)[^>])*)>/g, (m, a, k, b) => `<a${a} data-dl="${k}" href="#download"${b}>`);
  if (key === "/index.html") {
    html = html.replace("<!--BLOG_LATEST-->", blog.latest);
    const version = (readFileSync(join(here, "../../internal/proto/proto.go"), "utf8").match(/"(\d+\.\d+\.\d+)"/) || [])[1];
    const faq = Object.keys(en).filter((k) => /^q\d+$/.test(k)).map((k) => ({ "@type": "Question", name: en[k], acceptedAnswer: { "@type": "Answer", text: en["a" + k.slice(1)] } }));
    const price = (v) => String(v).replace(/[^\d.]/g, "");
    const ld = { "@context": "https://schema.org", "@graph": [
      { "@type": "Organization", "@id": SITE + "#org", name: "openv6", url: SITE, logo: SITE + "assets/logo-256.png", sameAs: ["https://github.com/thephux/openv6-releases"] },
      { "@type": "WebSite", "@id": SITE + "#website", name: "openv6", url: SITE, inLanguage: ["en", "vi"], publisher: { "@id": SITE + "#org" } },
      { "@type": "SoftwareApplication", "@id": SITE + "#app", name: "openv6", url: SITE, description: en.lead,
        applicationCategory: "UtilitiesApplication", applicationSubCategory: "IPv6 proxy server", operatingSystem: "Windows 10, Windows 11, macOS, Linux",
        softwareVersion: version, downloadUrl: SITE + "#download", screenshot: SITE + "assets/shot-proxies.webp", publisher: { "@id": SITE + "#org" },
        featureList: [en.f1_h, en.f2_h, en.f3_h, en.f4_h, en.f5_h, en.f6_h].filter(Boolean),
        offers: [
          { "@type": "Offer", name: "Free", price: "0", priceCurrency: "USD", description: [en.pf1, en.pf2].join(", ") },
          { "@type": "Offer", name: en.p1_h, price: price(PRICES.pro1), priceCurrency: "USD", url: LINKS.buy1 },
          { "@type": "Offer", name: en.p3_h, price: price(PRICES.pro3), priceCurrency: "USD", url: LINKS.buy3 }] },
      { "@type": "FAQPage", "@id": SITE + "#faq", mainEntity: faq },
    ] };
    html = html.replace("</head>", `<script type="application/ld+json">${JSON.stringify(ld).replace(/</g, "\\u003c")}</script>\n</head>`);
  }
  return html;
}
for (const k of Object.keys(files).filter((k) => k.endsWith(".html"))) {
  const html = prerender(Buffer.from(files[k].b64, "base64").toString("utf8"), k);
  files[k].b64 = Buffer.from(html).toString("base64");
}

// Cache busting: asset URLs in every page get ?v=<hash of every asset>, so a
// new screenshot or stylesheet is never hidden behind a browser's day-long cache.
{
  const h = createHash("sha256");
  for (const k of Object.keys(files).sort()) if (!k.endsWith(".html")) h.update(k + files[k].etag);
  const v = h.digest("hex").slice(0, 10);
  for (const k of Object.keys(files).filter((k) => k.endsWith(".html"))) {
    let html = Buffer.from(files[k].b64, "base64").toString("utf8");
    html = html.replace(/(assets\/[\w.-]+\.(?:webp|png|svg|css))(?=["'])/g, "$1?v=" + v).replaceAll("__ASSET_V__", v);
    const body = Buffer.from(html);
    files[k] = { ...files[k], b64: body.toString("base64"),
      etag: '"' + createHash("sha256").update(body).digest("hex").slice(0, 16) + '"' };
  }
}

const worker = `// generated by deploy/cloudflare/build.mjs — do not edit
const BASE = "/openv6";
const REL = "https://github.com/thephux/openv6-releases/releases";
const DOWNLOADS = {
  "/download/windows": REL + "/latest/download/openv6-setup-windows-amd64.exe",
  "/download/macos": REL + "/latest/download/openv6-macos.pkg",
  "/download/linux": REL + "/latest",
  "/download": REL + "/latest",
};
const FILES = ${JSON.stringify(files)};
const cache = new Map();
function bytes(f) {
  let b = cache.get(f);
  if (!b) { b = Uint8Array.from(atob(f.b64), c => c.charCodeAt(0)); cache.set(f, b); }
  return b;
}
export default {
  fetch(req) {
    const url = new URL(req.url);
    if (req.method !== "GET" && req.method !== "HEAD") return new Response("Method not allowed", { status: 405, headers: { allow: "GET, HEAD" } });
    if (url.pathname === BASE) return Response.redirect(url.origin + BASE + "/" + url.search, 301);
    if (!url.pathname.startsWith(BASE + "/")) return new Response("Not found", { status: 404 });
    let path = url.pathname.slice(BASE.length);
    // One stable download URL per OS (always the latest release); shows up in Cloudflare analytics.
    const dl = DOWNLOADS[path.endsWith("/") ? path.slice(0, -1) : path];
    if (dl) return new Response(null, { status: 302, headers: { location: dl, "cache-control": "no-store" } });
    if (!path.endsWith("/") && FILES[path + "/index.html"]) return Response.redirect(url.origin + BASE + path + "/" + url.search, 301);
    if (path.endsWith("/")) path += "index.html";
    const f = FILES[path];
    if (!f) return new Response("Not found", { status: 404, headers: { "content-type": "text/plain; charset=utf-8" } });
    const headers = {
      "content-type": f.type, etag: f.etag,
      // pages, sitemap, feed, llms.txt: short cache; versioned assets: a day
      "cache-control": ["text/html", "text/plain", "application/xml"].some((t) => f.type.startsWith(t)) ? "public, max-age=300" : "public, max-age=86400",
      "x-content-type-options": "nosniff", "referrer-policy": "strict-origin-when-cross-origin",
    };
    if (req.headers.get("if-none-match") === f.etag) return new Response(null, { status: 304, headers });
    return new Response(req.method === "HEAD" ? null : bytes(f), { headers });
  },
};
`;
rmSync(out, { recursive: true, force: true });
mkdirSync(out, { recursive: true });
writeFileSync(join(out, "worker.js"), worker);
console.log(`bundled ${Object.keys(files).length} files, ${(worker.length / 1024).toFixed(0)} KiB`);
